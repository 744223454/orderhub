package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// twoDepartmentsResponse 是两个部门的最小响应（外层是对象，与真实接口一致）。
const twoDepartmentsResponse = `{"errcode":0,"errmsg":"ok","department":[
  {"id":1,"name":"根部门","parentid":0,"order":100000000,"department_leader":["boss"]},
  {"id":2,"name":"研发部","parentid":1,"order":99999000,"department_leader":[]}
]}`

// departmentList 用给定的部门 JSON 片段包一层标准响应外壳。
func departmentList(departments string) string {
	return fmt.Sprintf(`{"errcode":0,"errmsg":"ok","department":[%s]}`, departments)
}

// memberBody 按部门 id 造一个 user/list 成功响应。
func memberBody(members string) string {
	return fmt.Sprintf(`{"errcode":0,"errmsg":"ok","userlist":[%s]}`, members)
}

// responseByDepartment 造一个按 department_id 分派的成员响应，便于用例只关心数据。
func responseByDepartment(byID map[string]string) func(int, string) string {
	return func(_ int, departmentID string) string {
		if body, ok := byID[departmentID]; ok {
			return body
		}
		return memberBody("")
	}
}

// TestDirectoryTranslatesParallelArrays 锁定后端最关键的一次翻译：
// 企微的 department[] 与 is_leader_in_dept[] 是靠**下标**对齐的并行数组，
// 必须翻成「部门 + 是否负责人」的对象数组，否则错位会静默地张冠李戴。
func TestDirectoryTranslatesParallelArrays(t *testing.T) {
	f := newFakeWecom(t)
	c := newFakeClient(t, f)
	f.departmentResponse = func(int) string { return twoDepartmentsResponse }
	f.memberResponse = responseByDepartment(map[string]string{
		"1": memberBody(`{"userid":"boss","name":"曾迈宽","position":"负责人","status":1,
			"main_department":1,"department":[1,2],"is_leader_in_dept":[1,0],
			"direct_leader":[],"avatar":"","mobile":""}`),
		"2": memberBody(`{"userid":"dev1","name":"张伟","position":"开发","status":4,
			"main_department":2,"department":[2],"is_leader_in_dept":[0],"direct_leader":["boss"]}`),
	})

	snapshot, err := c.Directory(context.Background(), false)
	if err != nil {
		t.Fatalf("抓取通讯录失败: %v", err)
	}

	if len(snapshot.Departments) != 2 {
		t.Fatalf("部门数期望 2，实际 %d", len(snapshot.Departments))
	}
	// 按 id 取而不是按下标：部门已按 order 排序（99999000 的研发部排在 100000000 的根部门之前），
	// 下标依赖排序规则，用例不该跟着它走。
	byID := make(map[int64]Department, len(snapshot.Departments))
	for _, d := range snapshot.Departments {
		byID[d.ID] = d
	}
	root, ok := byID[1]
	if !ok {
		t.Fatalf("部门 1 缺失: %+v", snapshot.Departments)
	}
	if root.Name != "根部门" || root.ParentID != 0 {
		t.Errorf("根部门翻译错误: %+v", root)
	}
	if len(root.LeaderUserIDs) != 1 || root.LeaderUserIDs[0] != "boss" {
		t.Errorf("部门负责人期望 [boss]，实际 %v", root.LeaderUserIDs)
	}
	if child := byID[2]; child.ParentID != 1 {
		t.Errorf("子部门的 parent_id 期望 1，实际 %d", child.ParentID)
	}

	if len(snapshot.Members) != 2 {
		t.Fatalf("成员数期望 2，实际 %d", len(snapshot.Members))
	}

	var boss, dev Member
	for _, m := range snapshot.Members {
		switch m.UserID {
		case "boss":
			boss = m
		case "dev1":
			dev = m
		}
	}

	// boss 的并行数组是 [1,2] / [1,0]：在部门 1 是负责人，在部门 2 不是。
	if len(boss.Departments) != 2 {
		t.Fatalf("boss 的部门数期望 2，实际 %+v", boss.Departments)
	}
	if boss.Departments[0] != (MemberDepartment{ID: 1, IsLeader: true}) {
		t.Errorf("部门 1 应标记为负责人，实际 %+v", boss.Departments[0])
	}
	if boss.Departments[1] != (MemberDepartment{ID: 2, IsLeader: false}) {
		t.Errorf("部门 2 不应标记为负责人，实际 %+v", boss.Departments[1])
	}
	if boss.MainDepartmentID != 1 || boss.Status != MemberStatusActivated {
		t.Errorf("boss 的主部门/状态翻译错误: %+v", boss)
	}
	if len(dev.DirectLeaderIDs) != 1 || dev.DirectLeaderIDs[0] != "boss" {
		t.Errorf("直属上级期望 [boss]，实际 %v", dev.DirectLeaderIDs)
	}
	if dev.Status != MemberStatusInactive {
		t.Errorf("status 应原样透传（4 = 未激活），实际 %d", dev.Status)
	}
}

// TestDirectoryToleratesMismatchedParallelArrays 是防 panic 用例：
// department[] 比 is_leader_in_dept[] 长时，按下标盲取会 panic，
// 把一次接口抖动放大成整个请求 500。约定是「以 department 为准，缺的当非负责人」。
func TestDirectoryToleratesMismatchedParallelArrays(t *testing.T) {
	f := newFakeWecom(t)
	c := newFakeClient(t, f)
	f.departmentResponse = func(int) string { return twoDepartmentsResponse }
	f.memberResponse = responseByDepartment(map[string]string{
		"1": memberBody(`{"userid":"u1","name":"甲","department":[1,2,3],"is_leader_in_dept":[1],"status":1,"main_department":1}`),
	})

	snapshot, err := c.Directory(context.Background(), false)
	if err != nil {
		t.Fatalf("并行数组长度不一致时不应报错，实际: %v", err)
	}
	if len(snapshot.Members) != 1 {
		t.Fatalf("成员数期望 1，实际 %d", len(snapshot.Members))
	}

	member := snapshot.Members[0]
	if len(member.Departments) != 3 {
		t.Fatalf("应以 department 为准保留 3 个部门，实际 %+v", member.Departments)
	}
	if !member.Departments[0].IsLeader {
		t.Error("下标 0 的 is_leader_in_dept=1，应标记为负责人")
	}
	if member.Departments[1].IsLeader || member.Departments[2].IsLeader {
		t.Errorf("越界部分应按非负责人处理，实际 %+v", member.Departments)
	}
}

// TestDirectoryDeduplicatesMembersAcrossDepartments 锁定跨部门去重：
// 一个成员属于多个部门时，每个部门都会返回他一次；同一人只能出现一次，
// 否则前端表格会出现重复行、按 userid 做 rowKey 还会直接撞 key。
func TestDirectoryDeduplicatesMembersAcrossDepartments(t *testing.T) {
	f := newFakeWecom(t)
	c := newFakeClient(t, f)
	f.departmentResponse = func(int) string { return twoDepartmentsResponse }

	shared := `{"userid":"boss","name":"曾迈宽","department":[1,2],"is_leader_in_dept":[1,0],"status":1,"main_department":1}`
	f.memberResponse = responseByDepartment(map[string]string{
		"1": memberBody(shared),
		"2": memberBody(shared),
	})

	snapshot, err := c.Directory(context.Background(), false)
	if err != nil {
		t.Fatalf("抓取通讯录失败: %v", err)
	}

	if len(snapshot.Members) != 1 {
		t.Fatalf("跨部门成员应去重为 1 人，实际 %d", len(snapshot.Members))
	}
	if got := f.membersFetched(); got != 2 {
		t.Errorf("两个部门应各取一次成员，实际 %d 次", got)
	}
}

// TestDirectoryNormalizesEmptyCollections 锁定「空集合序列化成 [] 而不是 null」。
// 这不是洁癖：null 会让前端多写一层判空，漏掉一处就是运行期的
// "Cannot read properties of null (reading 'map')"。
func TestDirectoryNormalizesEmptyCollections(t *testing.T) {
	f := newFakeWecom(t)
	c := newFakeClient(t, f)
	f.departmentResponse = func(int) string { return twoDepartmentsResponse }
	f.memberResponse = responseByDepartment(map[string]string{
		"2": memberBody(`{"userid":"u1","name":"甲","status":4,"main_department":2,"department":[2]}`),
	})

	snapshot, err := c.Directory(context.Background(), false)
	if err != nil {
		t.Fatalf("抓取通讯录失败: %v", err)
	}

	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("序列化快照失败: %v", err)
	}
	body := string(raw)

	// 根部门的负责人为空、成员的直属上级为空、department 里缺 is_leader_in_dept。
	for _, field := range []string{
		`"leader_user_ids":[]`,
		`"direct_leader_ids":[]`,
	} {
		if !strings.Contains(body, field) {
			t.Errorf("响应里应出现 %s，实际 %s", field, body)
		}
	}
	if strings.Contains(body, "null") {
		t.Errorf("快照里不应出现 null，实际 %s", body)
	}

	member := snapshot.Members[0]
	if member.Departments[0].IsLeader {
		t.Error("缺少 is_leader_in_dept 时应按非负责人处理")
	}
}

// TestDirectorySortsDepartments 锁定部门排序：order 升序、同序按 id。
// 企微的 order 是 99994000 这类偏移量，前端不该去猜它的语义。
func TestDirectorySortsDepartments(t *testing.T) {
	f := newFakeWecom(t)
	c := newFakeClient(t, f)
	// 故意乱序返回，且两个部门的 order 相同，考验 id 作为第二排序键。
	f.departmentResponse = func(int) string {
		return departmentList(`
		  {"id":30,"name":"丙","parentid":1,"order":99999000},
		  {"id":20,"name":"乙","parentid":1,"order":100000000},
		  {"id":10,"name":"甲","parentid":1,"order":100000000}
		`)
	}

	snapshot, err := c.Directory(context.Background(), false)
	if err != nil {
		t.Fatalf("抓取通讯录失败: %v", err)
	}

	got := make([]int64, 0, len(snapshot.Departments))
	for _, d := range snapshot.Departments {
		got = append(got, d.ID)
	}
	want := []int64{30, 10, 20}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("部门顺序期望 %v，实际 %v", want, got)
		}
	}
}

// TestDirectoryCachesSnapshot 锁定缓存：TTL 内的第二次调用不该再打企微。
// 官方口径是 user/list 要逐部门取，不缓存的话每刷一次页面就是 N 次调用。
func TestDirectoryCachesSnapshot(t *testing.T) {
	f := newFakeWecom(t)
	c := newFakeClient(t, f)
	f.departmentResponse = func(int) string { return twoDepartmentsResponse }

	first, err := c.Directory(context.Background(), false)
	if err != nil {
		t.Fatalf("首次抓取失败: %v", err)
	}
	if first.FromCache {
		t.Error("首次抓取不应标记为命中缓存")
	}

	second, err := c.Directory(context.Background(), false)
	if err != nil {
		t.Fatalf("二次抓取失败: %v", err)
	}
	if !second.FromCache {
		t.Error("TTL 内的第二次调用应命中缓存")
	}
	if !second.FetchedAt.Equal(first.FetchedAt) {
		t.Error("命中缓存时应返回同一份快照（fetched_at 一致）")
	}
	if got := f.departmentsFetched(); got != 1 {
		t.Errorf("部门列表应只取 1 次，实际 %d 次", got)
	}
	if got := f.membersFetched(); got != 2 {
		t.Errorf("两个部门应各取 1 次成员，实际 %d 次", got)
	}
}

// TestDirectoryForceRefreshBypassesCache 锁定 ?refresh=1 的语义。
func TestDirectoryForceRefreshBypassesCache(t *testing.T) {
	f := newFakeWecom(t)
	c := newFakeClient(t, f)
	f.departmentResponse = func(int) string { return twoDepartmentsResponse }

	if _, err := c.Directory(context.Background(), false); err != nil {
		t.Fatalf("首次抓取失败: %v", err)
	}

	forced, err := c.Directory(context.Background(), true)
	if err != nil {
		t.Fatalf("强制刷新失败: %v", err)
	}
	if forced.FromCache {
		t.Error("强制刷新不应标记为命中缓存")
	}
	if got := f.departmentsFetched(); got != 2 {
		t.Errorf("强制刷新应重新抓取（部门列表共 2 次），实际 %d 次", got)
	}
}

// TestDirectoryKeepsCacheWhenRefreshFails 锁定「抓取失败不动缓存」：
// 一次网络抖动不该把已经抓到的组织架构一起丢掉。
func TestDirectoryKeepsCacheWhenRefreshFails(t *testing.T) {
	f := newFakeWecom(t)
	c := newFakeClient(t, f)
	f.departmentResponse = func(call int) string {
		if call == 1 {
			return twoDepartmentsResponse
		}
		return `{"errcode":60020,"errmsg":"not allow to access from your ip"}`
	}

	if _, err := c.Directory(context.Background(), false); err != nil {
		t.Fatalf("首次抓取失败: %v", err)
	}

	if _, err := c.Directory(context.Background(), true); !errors.Is(err, ErrCorpConfig) {
		t.Fatalf("刷新失败时期望 ErrCorpConfig，实际 %v", err)
	}

	// 缓存仍在：不带 refresh 的请求依然拿得到上次的数据。
	cached, err := c.Directory(context.Background(), false)
	if err != nil {
		t.Fatalf("刷新失败不应清掉缓存，实际报错: %v", err)
	}
	if !cached.FromCache || len(cached.Departments) != 2 {
		t.Errorf("期望拿到上次的快照，实际 %+v", cached)
	}
}

// TestDirectoryCoalescesConcurrentRefreshes 锁定并发合并：
// 20 个管理员同时点刷新，只应打一遍企微（频率配额是全局的，谁也逃不掉）。
func TestDirectoryCoalescesConcurrentRefreshes(t *testing.T) {
	f := newFakeWecom(t)
	c := newFakeClient(t, f)

	arrived := make(chan struct{})
	gate := make(chan struct{})
	var once sync.Once
	f.departmentResponse = func(int) string {
		once.Do(func() { close(arrived) })
		<-gate
		return twoDepartmentsResponse
	}

	const workers = 20
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if _, err := c.Directory(context.Background(), true); err != nil {
				t.Errorf("并发刷新失败: %v", err)
			}
		}()
	}

	<-arrived
	close(gate)
	wg.Wait()

	if got := f.departmentsFetched(); got != 1 {
		t.Errorf("20 个并发刷新应合并成 1 次抓取，实际 %d 次", got)
	}
}

// TestDirectoryFailsOnPartialDepartmentFailure 锁定「不返回残缺数据」：
// 少几个人看起来只是数据少，实际会被读成「这几个同事被移出企业了」，
// 比直接报错更难发现。
func TestDirectoryFailsOnPartialDepartmentFailure(t *testing.T) {
	f := newFakeWecom(t)
	c := newFakeClient(t, f)
	f.departmentResponse = func(int) string { return twoDepartmentsResponse }

	// 只在第一次抓取时让部门 2 失败，第二次放行 —— 这样才能区分
	// 「重试时真的又打了企微」与「重试时命中了（不该存在的）缓存」。
	var mu sync.Mutex
	attempts := 0
	f.memberResponse = func(_ int, departmentID string) string {
		if departmentID == "2" {
			mu.Lock()
			attempts++
			first := attempts == 1
			mu.Unlock()
			if first {
				return `{"errcode":60011,"errmsg":"no privilege"}`
			}
		}
		return memberBody(`{"userid":"boss","name":"曾迈宽","status":1,"department":[1],"main_department":1}`)
	}

	_, err := c.Directory(context.Background(), false)
	if !errors.Is(err, ErrNoPermission) {
		t.Fatalf("任一部门读取失败即应整体失败，实际 %v", err)
	}

	if _, err := c.Directory(context.Background(), false); err != nil {
		t.Fatalf("二次抓取失败: %v", err)
	}
	if got := f.departmentsFetched(); got != 2 {
		t.Errorf("上次抓取失败不应写缓存，重新抓取应再打一次部门列表，实际共 %d 次", got)
	}
}

// TestDirectoryTranslatesErrorCodes 锁定通讯录接口的错误翻译。
// 数组接口失败时返回的是 errcode 对象 —— 按数组解析会报「解析失败」，
// 把「可信 IP 变了」这种能一眼看出的原因藏起来。
func TestDirectoryTranslatesErrorCodes(t *testing.T) {
	cases := []struct {
		name     string
		response string
		wantErr  error
	}{
		{"可信 IP 失效", `{"errcode":60020,"errmsg":"not allow to access from your ip"}`, ErrCorpConfig},
		{"频率超限", `{"errcode":45009,"errmsg":"api freq out of limit"}`, ErrRateLimited},
		{"无权限", `{"errcode":60011,"errmsg":"no privilege"}`, ErrNoPermission},
		{"未识别错误码", `{"errcode":12345,"errmsg":"unknown"}`, ErrWecomUnavailable},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeWecom(t)
			c := newFakeClient(t, f)
			f.departmentResponse = func(int) string { return tc.response }

			_, err := c.Directory(context.Background(), false)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("期望哨兵 %v，实际 %v", tc.wantErr, err)
			}
		})
	}
}

// TestDirectoryRejectsUnexpectedShape 锁定「解析层面上就失败的响应必须报错」：
// 绝不能把解析失败当成「公司里没有人」。
//
// 注意这里**不包含**「errcode=0 但缺少 department 字段」那种情况：它与
// 「这家企业的可见范围是空的」返回的形状完全一样（企微两种情况下都返回 errcode=0），
// 拦掉它就会把空范围误报成故障，而不拦则最坏是一个空列表。
// 这个取舍由前端负责补齐——空态会明确写出「没读到任何部门」的两种可能。
func TestDirectoryRejectsUnexpectedShape(t *testing.T) {
	for _, response := range []string{
		`[{"id":1,"name":"根部门"}]`, // 万一将来真改成了裸数组
		`not json at all`,
		`{"errcode":0,"errmsg":"ok","department":"oops"}`,
	} {
		f := newFakeWecom(t)
		c := newFakeClient(t, f)
		f.departmentResponse = func(int) string { return response }

		if _, err := c.Directory(context.Background(), false); err == nil {
			t.Errorf("响应体 %s 期望报错，实际成功了", response)
		}
	}
}

// TestDirectoryTreatsMissingDepartmentAsEmpty 明确记录上一条里那个取舍的另一面：
// 没有 department 字段时得到「空通讯录」而不是报错。这是有意为之，不是漏判。
func TestDirectoryTreatsMissingDepartmentAsEmpty(t *testing.T) {
	f := newFakeWecom(t)
	c := newFakeClient(t, f)
	f.departmentResponse = func(int) string { return `{"errcode":0,"errmsg":"ok"}` }

	snapshot, err := c.Directory(context.Background(), false)
	if err != nil {
		t.Fatalf("缺少 department 字段应视为空结果，实际: %v", err)
	}
	if len(snapshot.Departments) != 0 || len(snapshot.Members) != 0 {
		t.Errorf("期望空通讯录，实际 %+v", snapshot)
	}
}

// TestDirectorySurvivesCallerCancellation 锁定「抓取不挂死在调用方的 ctx 上」。
// 抓取是被 singleflight 合并后共享的：如果挂在第一个调用方的请求 ctx 上，
// 那个人一关页面，所有人都会收到 context canceled 这种看不出所以然的错误。
func TestDirectorySurvivesCallerCancellation(t *testing.T) {
	f := newFakeWecom(t)
	c := newFakeClient(t, f)
	f.departmentResponse = func(int) string { return twoDepartmentsResponse }

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := c.Directory(ctx, false); err != nil {
		t.Fatalf("调用方取消不应影响本次抓取，实际: %v", err)
	}
	if got := f.departmentsFetched(); got != 1 {
		t.Errorf("期望完成一次抓取，实际 %d 次", got)
	}
}

// TestDirectoryEmptyDepartments 锁定「一个部门都看不到」的边界：
// 空结果也是正常结果（可见范围为空），不能因此报错。
func TestDirectoryEmptyDepartments(t *testing.T) {
	f := newFakeWecom(t)
	c := newFakeClient(t, f)
	f.departmentResponse = func(int) string { return departmentList("") }

	snapshot, err := c.Directory(context.Background(), false)
	if err != nil {
		t.Fatalf("空部门列表不应报错，实际: %v", err)
	}
	if len(snapshot.Departments) != 0 || len(snapshot.Members) != 0 {
		t.Errorf("期望两个空集合，实际 %+v", snapshot)
	}
	if got := f.membersFetched(); got != 0 {
		t.Errorf("没有部门时不应调用 user/list，实际 %d 次", got)
	}

	raw, _ := json.Marshal(snapshot)
	if strings.Contains(string(raw), "null") {
		t.Errorf("空集合应序列化为 []，实际 %s", raw)
	}
}

// TestDirectoryTTLExpiry 锁定缓存过期后会重新抓取。
// 用一个超短的 TTL 观察过期行为，不必真等 5 分钟。
func TestDirectoryTTLExpiry(t *testing.T) {
	f := newFakeWecom(t)
	c := newFakeClient(t, f)
	f.departmentResponse = func(int) string { return twoDepartmentsResponse }

	if _, err := c.Directory(context.Background(), false); err != nil {
		t.Fatalf("首次抓取失败: %v", err)
	}

	// 直接把缓存时间戳往前拨到 TTL 之外，等价于「5 分钟过去了」。
	c.dirMu.Lock()
	c.dirTime = time.Now().Add(-directoryTTL - time.Second)
	c.dirMu.Unlock()

	snapshot, err := c.Directory(context.Background(), false)
	if err != nil {
		t.Fatalf("缓存过期后应重新抓取，实际: %v", err)
	}
	if snapshot.FromCache {
		t.Error("缓存已过期，不应标记为命中缓存")
	}
	if got := f.departmentsFetched(); got != 2 {
		t.Errorf("缓存过期后应重新抓取，实际共 %d 次", got)
	}
}

// TestDirectorySortsMembersDeterministically 锁定成员顺序的确定性。
//
// 成员是并发抓取的，到达顺序每次都可能不同；不排的话前端分页表格在每次刷新后
// 都会「换一批人站在前面」，看起来像数据变了。
func TestDirectorySortsMembersDeterministically(t *testing.T) {
	f := newFakeWecom(t)
	c := newFakeClient(t, f)
	f.departmentResponse = func(int) string { return twoDepartmentsResponse }

	// 让部门 1 与部门 2 返回顺序不同的两批人，模拟「谁先返回谁先入」。
	f.memberResponse = func(_ int, departmentID string) string {
		if departmentID == "1" {
			return memberBody(`{"userid":"ZengMaiKuan","name":"曾迈宽","status":1,"department":[1],"main_department":1},
				{"userid":"renzhen","name":"任真","status":4,"department":[1],"main_department":1}`)
		}
		return memberBody(`{"userid":"haolv","name":"郝律","status":4,"department":[2],"main_department":2},
			{"userid":"zhangmin","name":"章敏","status":4,"department":[2],"main_department":2}`)
	}

	snapshot, err := c.Directory(context.Background(), false)
	if err != nil {
		t.Fatalf("抓取通讯录失败: %v", err)
	}

	got := make([]string, 0, len(snapshot.Members))
	for _, member := range snapshot.Members {
		got = append(got, member.UserID)
	}
	want := []string{"haolv", "renzhen", "ZengMaiKuan", "zhangmin"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("成员顺序期望（帐号升序、大小写不敏感）%v，实际 %v", want, got)
		}
	}

	// 再抓一次（强制刷新），顺序必须完全一致。
	forced, err := c.Directory(context.Background(), true)
	if err != nil {
		t.Fatalf("强制刷新失败: %v", err)
	}
	for i, member := range forced.Members {
		if member.UserID != got[i] {
			t.Fatalf("两次抓取的成员顺序不一致：第 %d 位 %q vs %q", i, member.UserID, got[i])
		}
	}
}

// TestDirectoryAttachesAccessToken 锁定每个请求都带上 access_token。
// department/list 与 user/list 都是**必须**带 token 的接口，漏了会一直 41001。
func TestDirectoryAttachesAccessToken(t *testing.T) {
	var missing []string
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case tokenPath:
			writeJSONBody(w, `{"errcode":0,"errmsg":"ok","access_token":"token-1","expires_in":7200}`)
		case departmentListPath:
			mu.Lock()
			if r.URL.Query().Get("access_token") == "" {
				missing = append(missing, departmentListPath)
			}
			mu.Unlock()
			writeJSONBody(w, twoDepartmentsResponse)
		case userListPath:
			mu.Lock()
			if r.URL.Query().Get("access_token") == "" {
				missing = append(missing, userListPath)
			}
			mu.Unlock()
			writeJSONBody(w, memberBody(""))
		default:
			return
		}
	}))
	t.Cleanup(server.Close)

	c := NewClient(Config{
		CorpID: "ww-test", AgentID: "1", Secret: "s", WebBaseURL: "http://localhost:3000",
		OpenAPIBase: server.URL,
	})
	if _, err := c.Directory(context.Background(), false); err != nil {
		t.Fatalf("抓取通讯录失败: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("这些请求没带 access_token: %v", missing)
	}
}
