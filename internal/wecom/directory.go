package wecom

// 企业通讯录（部门 + 成员）的拉取。
//
// 两个刻意的设计取舍：
//
//  1. **返回扁平数据，树由前端组装。** 后端只做「搬运 + 翻译」：把企微的字段名
//     翻成 snake_case、把靠下标对齐的并行数组翻成对象数组、把 nil 归一成空数组，
//     但不构造树、不算层级、不排序成员。理由是树是**展示形态**而成员列表还需要
//     跨部门搜索 —— 前端无论怎样都要同时持有「扁平的成员集合」和「部门树」两份
//     视图，后端只给树的话，搜索反倒要再拍平一次。接口因此不会因为前端的展示
//     方式变化（换成折叠面板、换成图形化组织图）而改。
//
//  2. **不做本地落库。** 通讯录落库就要处理同步、离职、一致性三件事，成本远大于
//     收益。这里用「5 分钟内存缓存」换掉绝大部分调用量 —— 官方口径是 user/list
//     要逐部门取，31 个部门就是 31 次调用，不缓存的话每刷新一次页面就烧一次配额。

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	// directoryTTL 是通讯录快照的内存缓存时长。
	//
	// 取 5 分钟是「新鲜度」与「配额」的折中：组织架构不属于秒级变动的数据，
	// 而每抓一次要打 1 + N 次接口（N = 部门数）。感知到的延迟由前端主动刷新兜底。
	directoryTTL = 5 * time.Minute

	// directoryTimeout 是**单次抓取**的上限。
	//
	// 不能依赖调用方传来的 ctx：那是 HTTP 请求的 ctx，客户端一断开就取消。
	// 而抓取是 shared（singleflight 合并后的那一份）——某个调用方关掉页面就
	// 让所有等着的请求一起失败，还会留下一个「context canceled」的假故障。
	// 所以抓取挂在脱离请求生命周期的 ctx 上，并自带超时兜底。
	directoryTimeout = 20 * time.Second

	// directoryGroupKey 是通讯录抓取的 singleflight 合并键。
	// 必须与 token 的键不同：共用键会让「取 token」与「取通讯录」互相顶掉。
	directoryGroupKey = "directory"

	// directoryWorkers 是逐部门取成员时的并发上限。
	//
	// 通讯录读取的频率上限约 600 次/分钟，32 个部门并发 5 路远谈不上触限，
	// 但串行执行在部门多的公司会慢到几秒——这是纯粹的等待，没必要。
	//
	// 已知上限：user/list 一次返回一个部门的全部成员，单部门超过 10000 人会被截断，
	// 需改用 user/list_id 分页。本企业规模远小于该上限，暂不处理；
	// 将来要支持大企业，这里是第一个要改的地方。
	directoryWorkers = 5
)

// 成员状态取值，来自企业微信 user/list 的 status 字段。
const (
	// MemberStatusActivated 已激活：既激活了企业微信，也（或）关注了微信插件。
	MemberStatusActivated = 1
	// MemberStatusDisabled 已禁用。
	MemberStatusDisabled = 2
	// MemberStatusInactive 未激活：既未激活企业微信也未关注微信插件。
	// 批量导入的成员默认都是这个状态 —— 人在通讯录里，接口照常返回，只是登不进企业微信。
	MemberStatusInactive = 4
	// MemberStatusQuit 已退出企业。
	MemberStatusQuit = 5
)

// Department 是一个企业微信部门。
type Department struct {
	// ID 部门 id，同一企业内唯一。
	ID int64 `json:"id"`
	// Name 部门名称。
	Name string `json:"name"`
	// ParentID 上级部门 id，根部门为 0。
	//
	// ⚠️ 它可能指向一个**不在本列表里**的部门：自建应用只能读可见范围，
	// 父部门不在范围内时就成了孤儿。组装树的一方必须处理这种情况，别默认树是连通的。
	ParentID int64 `json:"parent_id"`
	// Order 在上级部门内的排序号，由企微分配（实测取值形如 99994000，不是 1/2/3）。
	Order int64 `json:"order"`
	// LeaderUserIDs 部门负责人的 userid 列表。
	LeaderUserIDs []string `json:"leader_user_ids"`
}

// MemberDepartment 描述成员在某个部门里的身份。
//
// 企微返回的是 department[] 与 is_leader_in_dept[] 两个**并行数组**（靠下标对齐），
// 这里翻成对象数组：下标对齐这种东西一旦漏到前端，错位会静默地张冠李戴。
type MemberDepartment struct {
	// ID 部门 id。
	ID int64 `json:"id"`
	// IsLeader 该成员是否为这个部门的负责人。
	IsLeader bool `json:"is_leader"`
}

// Member 是一个企业微信成员。
type Member struct {
	// UserID 成员帐号，企业内唯一。
	UserID string `json:"userid"`
	// Name 成员姓名。
	Name string `json:"name"`
	// Position 职务，可能为空串。
	Position string `json:"position"`
	// Status 成员状态，取值见 MemberStatusXxx。
	Status int `json:"status"`
	// MainDepartmentID 主部门 id。
	MainDepartmentID int64 `json:"main_department_id"`
	// Departments 所属部门（含是否负责人），至少一个。
	Departments []MemberDepartment `json:"departments"`
	// DirectLeaderIDs 直属上级的 userid 列表。
	//
	// ⚠️ 上级本人可能不在可见范围内，此时这个 userid 在 members 里查不到，
	// 前端要有回落（直接显示 userid 而不是空白）。
	DirectLeaderIDs []string `json:"direct_leader_ids"`
}

// DirectorySnapshot 是一次通讯录抓取的完整结果（扁平结构）。
//
// 它同时是接口响应体：字段名即对外契约。
// 约定：**一旦放进缓存就不再修改**（只读共享），因此并发地序列化它是安全的。
type DirectorySnapshot struct {
	// Departments 全量部门（已按 order、id 排序，顺序稳定）。
	Departments []Department `json:"departments"`
	// Members 去重后的成员（成员可属于多个部门，同一人只出现一次）。
	Members []Member `json:"members"`
	// FetchedAt 本次快照的抓取完成时间。
	FetchedAt time.Time `json:"fetched_at"`
	// FromCache 本次响应是否直接命中服务端缓存。
	// 前端据此说明「这份数据是刚抓的还是几分钟前的」，避免把缓存当成实时。
	FromCache bool `json:"from_cache"`
}

// 企微原始响应里的形状，只在包内使用。
//
// ⚠️ 实测：department/list 与 user/list **成功时返回的也是对象**
// （`{"errcode":0,"errmsg":"ok","department":[...]}`、`... "userlist":[...]}`），
// 不是裸数组。官方文档示例只截了数组那一段，照着它写会得到一个「数组还是对象」的判断分支，
// 而那条分支永远不会被真实响应走到。
type (
	// departmentListResponse 是 department/list 的响应。
	departmentListResponse struct {
		ErrCode     int             `json:"errcode"`
		ErrMsg      string          `json:"errmsg"`
		Departments []departmentDTO `json:"department"`
	}

	// memberListResponse 是 user/list 的响应。
	memberListResponse struct {
		ErrCode  int         `json:"errcode"`
		ErrMsg   string      `json:"errmsg"`
		UserList []memberDTO `json:"userlist"`
	}

	// departmentDTO 是 department/list 里的一项。
	departmentDTO struct {
		ID       int64    `json:"id"`
		Name     string   `json:"name"`
		ParentID int64    `json:"parentid"`
		Order    int64    `json:"order"`
		Leaders  []string `json:"department_leader"`
	}

	// memberDTO 是 user/list 里的一项。
	memberDTO struct {
		UserID         string   `json:"userid"`
		Name           string   `json:"name"`
		Position       string   `json:"position"`
		Status         int      `json:"status"`
		MainDepartment int64    `json:"main_department"`
		Department     []int64  `json:"department"`
		IsLeaderInDept []int    `json:"is_leader_in_dept"`
		DirectLeader   []string `json:"direct_leader"`
	}
)

// code 实现 codeCarrier。
func (r *departmentListResponse) code() int { return r.ErrCode }

// message 实现 codeCarrier。
func (r *departmentListResponse) message() string { return r.ErrMsg }

// code 实现 codeCarrier。
func (r *memberListResponse) code() int { return r.ErrCode }

// message 实现 codeCarrier。
func (r *memberListResponse) message() string { return r.ErrMsg }

// Directory 返回通讯录快照。
//
// force 为 true 时跳过缓存重新抓取（对应接口的 ?refresh=1）。
// 并发调用会被 singleflight 合并成一次真实抓取：多个管理员同时刷新不该打 N 遍企微。
func (c *Client) Directory(ctx context.Context, force bool) (*DirectorySnapshot, error) {
	if !force {
		if snapshot, ok := c.cachedDirectory(); ok {
			return snapshot, nil
		}
	}

	value, err, _ := c.group.Do(directoryGroupKey, func() (any, error) {
		// 二次检查：等待合并期间可能已有别的 goroutine 抓完并写了缓存。
		if !force {
			if snapshot, ok := c.cachedDirectory(); ok {
				return snapshot, nil
			}
		}

		// 脱离调用方 ctx 并自带超时：这次抓取可能同时服务多个调用方，
		// 不能被其中任何一个的断连拖下水（详见 directoryTimeout 的说明）。
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), directoryTimeout)
		defer cancel()

		snapshot, err := c.fetchDirectory(fetchCtx)
		if err != nil {
			// 只有抓成功了才写缓存：失败时保留旧快照，下次不带 refresh 的请求还能用，
			// 免得一次网络抖动之后连「几分钟前的组织架构」都看不到。
			return nil, err
		}

		c.storeDirectory(snapshot)
		return snapshot, nil
	})
	if err != nil {
		return nil, err
	}

	snapshot, ok := value.(*DirectorySnapshot)
	if !ok {
		return nil, ErrWecomUnavailable
	}
	return snapshot, nil
}

// cachedDirectory 返回仍在有效期内的快照。
//
// 返回的是一份**浅拷贝**并标上 FromCache：缓存里那份是同多个调用方共享的，
// 直接在它身上改标记会把「来自缓存」这个事实污染给所有人。
func (c *Client) cachedDirectory() (*DirectorySnapshot, bool) {
	c.dirMu.RLock()
	defer c.dirMu.RUnlock()

	if c.dir == nil || time.Since(c.dirTime) >= directoryTTL {
		return nil, false
	}

	hit := *c.dir
	hit.FromCache = true
	return &hit, true
}

// storeDirectory 写入快照缓存。
func (c *Client) storeDirectory(snapshot *DirectorySnapshot) {
	c.dirMu.Lock()
	defer c.dirMu.Unlock()

	c.dir = snapshot
	c.dirTime = time.Now()
}

// fetchDirectory 抓一次完整的通讯录。
//
// 任何一步失败都整体失败，不做「部分返回」：组织架构缺了几个人，看起来只是
// 数据少，实际会让人以为「这几个同事被移出企业了」——错得比报个错还难发现。
func (c *Client) fetchDirectory(ctx context.Context) (*DirectorySnapshot, error) {
	departments, err := c.fetchDepartments(ctx)
	if err != nil {
		return nil, err
	}

	members, err := c.fetchMembers(ctx, departments)
	if err != nil {
		return nil, err
	}

	sortDepartments(departments)
	sortMembers(members)

	return &DirectorySnapshot{
		Departments: departments,
		Members:     members,
		FetchedAt:   time.Now(),
	}, nil
}

// fetchDepartments 取全量部门。
func (c *Client) fetchDepartments(ctx context.Context) ([]Department, error) {
	var resp departmentListResponse
	if err := c.callWithTokenInto(ctx, departmentListPath, nil, &resp); err != nil {
		return nil, fmt.Errorf("获取部门列表失败: %w", err)
	}

	// 空切片而非 nil：nil 会被序列化成 null，前端就得多写一层判空。
	departments := make([]Department, 0, len(resp.Departments))
	for _, dto := range resp.Departments {
		departments = append(departments, Department{
			ID:            dto.ID,
			Name:          dto.Name,
			ParentID:      dto.ParentID,
			Order:         dto.Order,
			LeaderUserIDs: nonNilStrings(dto.Leaders),
		})
	}
	return departments, nil
}

// fetchMembers 逐部门取成员并按 userid 去重。
//
// 为什么逐部门取而不是只取根部门：user/list 默认只返回该部门的**直属**成员。
// 官方文档给的参数表里没有 fetch_child，社区里普遍在用的 `fetch_child=1` 属于
// 未公开参数（实测当前可用，一次就能拿到整棵子树），把它作为唯一入口等于把
// 整个功能押在一个官方没承诺的参数上 —— 哪天收紧就全线读不到人。
// 逐部门取走的是文档明确支持的路径，代价是 N 次调用，用缓存与并发摊掉。
func (c *Client) fetchMembers(ctx context.Context, departments []Department) ([]Member, error) {
	if len(departments) == 0 {
		return []Member{}, nil
	}

	var (
		mu   sync.Mutex
		seen = make(map[string]struct{}, len(departments))
		out  = make([]Member, 0, len(departments))
	)

	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(directoryWorkers)

	for _, department := range departments {
		group.Go(func() error {
			params := url.Values{"department_id": {strconv.FormatInt(department.ID, 10)}}

			var resp memberListResponse
			if err := c.callWithTokenInto(groupCtx, userListPath, params, &resp); err != nil {
				return fmt.Errorf("获取部门 %d 的成员失败: %w", department.ID, err)
			}

			// 各 goroutine 的结果在锁内合并：map 与切片都不允许并发写。
			// 合并本身极快（几十条数据），不会成为瓶颈。
			mu.Lock()
			defer mu.Unlock()
			for _, dto := range resp.UserList {
				// 一个成员属于多个部门时会在每个部门里各出现一次；
				// 每次返回的 department[] 都是**全量**部门列表，所以留先到的那份即可。
				if _, ok := seen[dto.UserID]; ok {
					continue
				}
				seen[dto.UserID] = struct{}{}
				out = append(out, dto.toMember())
			}
			return nil
		})
	}

	if err := group.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}

// toMember 把企微的原始成员翻成本包的 Member。
func (dto memberDTO) toMember() Member {
	departments := make([]MemberDepartment, 0, len(dto.Department))
	for index, id := range dto.Department {
		// 并行数组按**下标**对齐，所以这里必须同时判 index 是否越界：
		// 企微返回的两个数组长度理论上一致，但一旦不一致，盲取下标会 panic ——
		// 把一次接口抖动放大成整个请求 500。长度不足时按「非负责人」处理。
		isLeader := index < len(dto.IsLeaderInDept) && dto.IsLeaderInDept[index] != 0
		departments = append(departments, MemberDepartment{ID: id, IsLeader: isLeader})
	}

	return Member{
		UserID:           dto.UserID,
		Name:             dto.Name,
		Position:         dto.Position,
		Status:           dto.Status,
		MainDepartmentID: dto.MainDepartment,
		Departments:      departments,
		DirectLeaderIDs:  nonNilStrings(dto.DirectLeader),
	}
}

// sortDepartments 按企微给的 order 升序、同序时按 id 升序排列。
//
// 后端排一次，前端就不必理解企微的 order 语义（实测它是 99994000 这样的偏移量，
// 不是 1/2/3），树中同级部门的顺序因此是稳定且可预期的。
func sortDepartments(departments []Department) {
	sort.SliceStable(departments, func(i, j int) bool {
		if departments[i].Order != departments[j].Order {
			return departments[i].Order < departments[j].Order
		}
		return departments[i].ID < departments[j].ID
	})
}

// nonNilStrings 把 nil 切片归一成空切片，保证序列化出的是 [] 而不是 null。
func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// sortMembers 把成员按帐号（不区分大小写）排成确定顺序。
//
// 为什么必须排：成员是**并发**逐部门抓取、先返回先入的，到达顺序每次都不一样。
// 不排的话每次刷新整个列表的顺序都会变 —— 前端是分页表格，用户看到的是
// 「点一下刷新，人全换了个位置」，会以为数据被改了。
//
// 为什么按帐号而不是姓名：帐号通常是姓名拼音（ZengMaiKuan / haolv），
// 按它排出来接近拼音序；而按姓名排只能按 Unicode 码点，中文看起来是乱的
// （Go 标准库里没有拼音库，引入一个只为了排序不值当）。
// 大小写不敏感是为了让 ZengMaiKuan 与 zhangmin 这类混写排在一起；
// 完全同名时用原始帐号兜底，保证顺序全序、稳定。
func sortMembers(members []Member) {
	sort.SliceStable(members, func(i, j int) bool {
		left := strings.ToLower(members[i].UserID)
		right := strings.ToLower(members[j].UserID)
		if left != right {
			return left < right
		}
		return members[i].UserID < members[j].UserID
	})
}
