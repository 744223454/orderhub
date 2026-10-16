// Package main 是开发调试用的种子数据工具：向本地数据库灌注一批「像真的」订单数据，
// 供数据看板等功能在真实数据不足时验证展示效果。
//
// 用法（在仓库根目录执行）：
//
//	go run ./cmd/seed              # 先清理旧种子数据，再重新灌注（幂等，可反复执行）
//	go run ./cmd/seed -clean       # 仅清理种子数据
//	go run ./cmd/seed -days 30 -users 12
//
// 工具只写 users 与 orders 两张表，种子用户以 seed_ 前缀标识，可随时整体清理；
// 仅面向本地开发数据库，切勿对生产库执行。
package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"time"

	"github.com/744223454/orderhub/internal/order"
	"github.com/744223454/orderhub/internal/user"
	"github.com/joho/godotenv"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const (
	// seedUserPrefix 是种子用户名的固定前缀，用于识别与清理。
	seedUserPrefix = "seed_"
	// seedPassword 是全部种子用户的统一登录密码（仅本地调试用，可登录查看「我的订单」）。
	seedPassword = "seed123456"
)

// beijing 返回北京时间时区。中国没有夏令时，固定 +8 偏移即可，
// 与统计接口的「北京时间切天」口径保持一致。
func beijing() *time.Location {
	return time.FixedZone("CST", 8*3600)
}

// product 是可下单的商品及其价格区间（单位：分）。
type product struct {
	name   string
	minFen int64
	maxFen int64
}

// products 种子订单使用的商品池（价格区间参照常见的数码配件价位）。
var products = []product{
	{"无线鼠标", 3900, 12900},
	{"机械键盘", 19900, 59900},
	{"显示器支架", 8900, 29900},
	{"移动固态硬盘 1TB", 29900, 89900},
	{"降噪耳机", 39900, 129900},
	{"充电宝 20000mAh", 9900, 24900},
	{"USB-C 扩展坞", 12900, 39900},
	{"笔记本内胆包", 4900, 15900},
	{"蓝牙音箱", 9900, 49900},
	{"屏幕挂灯", 7900, 25900},
}

func main() {
	days := flag.Int("days", 14, "生成最近多少天的订单数据")
	userCount := flag.Int("users", 8, "生成的种子用户数量（1~99）")
	cleanOnly := flag.Bool("clean", false, "仅清理种子数据，不重新生成")
	flag.Parse()

	if *days < 1 || *userCount < 1 || *userCount > 99 {
		log.Fatalf("参数不合法: -days 需 ≥ 1，-users 需在 1~99 之间")
	}

	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Fatalf("加载 .env 文件失败: %v", err)
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("未设置 DATABASE_URL 环境变量")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}
	// 表结构以 server 启动时的迁移为准，这里只兜底迁移 seed 会写入的两张表。
	if err := db.AutoMigrate(&user.User{}, &order.Order{}); err != nil {
		log.Fatalf("数据库自动迁移失败: %v", err)
	}

	removedOrders, removedUsers, err := cleanSeedData(db)
	if err != nil {
		log.Fatalf("清理旧种子数据失败: %v", err)
	}
	if removedOrders > 0 || removedUsers > 0 {
		log.Printf("已清理旧种子数据：订单 %d 条、用户 %d 个", removedOrders, removedUsers)
	}
	if *cleanOnly {
		return
	}

	seedUsers, err := createSeedUsers(db, *userCount)
	if err != nil {
		log.Fatalf("创建种子用户失败: %v", err)
	}
	orders := createSeedOrders(seedUsers, *days)
	if len(orders) > 0 {
		if err := db.Create(&orders).Error; err != nil {
			log.Fatalf("创建种子订单失败: %v", err)
		}
	}

	// 汇总打印，便于与看板对账。
	byStatus := map[order.Status]int{}
	for _, o := range orders {
		byStatus[o.Status]++
	}
	log.Printf("种子用户 %d 个：%s01 ~ %s%02d（密码 %s）", *userCount, seedUserPrefix, seedUserPrefix, *userCount, seedPassword)
	log.Printf("种子订单 %d 条，覆盖最近 %d 天；各状态：pending %d / paid %d / shipped %d / completed %d / refunded %d",
		len(orders), *days,
		byStatus[order.StatusPending], byStatus[order.StatusPaid], byStatus[order.StatusShipped],
		byStatus[order.StatusCompleted], byStatus[order.StatusRefunded])
}

// cleanSeedData 删除全部种子用户及其订单，返回（删除的订单数, 删除的用户数）。
//
// 按用户名前缀识别；`\_` 转义下划线，避免「seedX…」之类的普通用户名被误删
// （PostgreSQL 的 LIKE 默认以反斜杠为转义符）。
func cleanSeedData(db *gorm.DB) (int64, int64, error) {
	var users []user.User
	if err := db.Where("name LIKE ?", `seed\_%`).Find(&users).Error; err != nil {
		return 0, 0, fmt.Errorf("查询种子用户失败: %w", err)
	}
	if len(users) == 0 {
		return 0, 0, nil
	}

	ids := make([]uint, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.ID)
	}

	orderRes := db.Where("user_id IN ?", ids).Delete(&order.Order{})
	if orderRes.Error != nil {
		return 0, 0, fmt.Errorf("删除种子订单失败: %w", orderRes.Error)
	}
	userRes := db.Delete(&users)
	if userRes.Error != nil {
		return 0, 0, fmt.Errorf("删除种子用户失败: %w", userRes.Error)
	}
	return orderRes.RowsAffected, userRes.RowsAffected, nil
}

// createSeedUsers 创建 n 个种子用户（seed_01…seed_nn，统一密码、角色为普通用户）。
// 密码逐个生成 bcrypt 哈希，与实际注册链路一致（每个哈希的盐不同）。
func createSeedUsers(db *gorm.DB, n int) ([]user.User, error) {
	users := make([]user.User, 0, n)
	for i := 1; i <= n; i++ {
		hash, err := bcrypt.GenerateFromPassword([]byte(seedPassword), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("生成密码哈希失败: %w", err)
		}
		users = append(users, user.User{
			Name:         fmt.Sprintf("%s%02d", seedUserPrefix, i),
			PasswordHash: string(hash),
			Role:         user.RoleUser,
		})
	}
	if err := db.Create(&users).Error; err != nil {
		return nil, fmt.Errorf("写入种子用户失败: %w", err)
	}
	return users, nil
}

// createSeedOrders 生成覆盖最近 days 天的订单（每天 3~12 条）。
//
// 时间用北京时间构造：创建时间随机落在当天之内，「今天」只生成到当前时刻为止，
// 不会出现未来时间的订单。CreatedAt / UpdatedAt 均为非零值，
// GORM 创建时不会用当前时间覆盖它们。
func createSeedOrders(users []user.User, days int) []order.Order {
	loc := beijing()
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	orders := make([]order.Order, 0, days*8)
	for daysAgo := 0; daysAgo < days; daysAgo++ {
		dayStart := today.AddDate(0, 0, -daysAgo)
		dayEnd := dayStart.Add(24 * time.Hour)
		if daysAgo == 0 {
			dayEnd = now
		}
		span := dayEnd.Sub(dayStart)
		if span <= 0 {
			continue
		}

		count := 3 + rand.IntN(10) // 每天 3~12 单
		for i := 0; i < count; i++ {
			u := users[rand.IntN(len(users))]
			p := products[rand.IntN(len(products))]
			createdAt := dayStart.Add(time.Duration(rand.Int64N(int64(span))))
			status := pickStatus(daysAgo)
			orders = append(orders, order.Order{
				UserID:      u.ID,
				ProductName: p.name,
				Amount:      p.minFen + rand.Int64N(p.maxFen-p.minFen+1),
				Status:      status,
				CreatedAt:   createdAt,
				UpdatedAt:   updatedAtFor(status, createdAt, now),
			})
		}
	}
	return orders
}

// pickStatus 按「越新的订单越可能停在早期状态」的规律抽取状态：
// 5 天以前只出现终态（completed / refunded），2~5 天之间到 shipped 为止，
// 最近两天才可能出现 paid 与 pending——贴近真实漏斗的形状。
func pickStatus(daysAgo int) order.Status {
	var pool []order.Status
	switch {
	case daysAgo >= 5:
		pool = []order.Status{
			order.StatusCompleted, order.StatusCompleted, order.StatusCompleted,
			order.StatusCompleted, order.StatusCompleted, order.StatusCompleted,
			order.StatusRefunded,
		}
	case daysAgo >= 2:
		pool = []order.Status{
			order.StatusCompleted, order.StatusCompleted, order.StatusCompleted,
			order.StatusCompleted, order.StatusCompleted,
			order.StatusShipped, order.StatusShipped,
			order.StatusRefunded,
		}
	default:
		pool = []order.Status{
			order.StatusCompleted, order.StatusCompleted, order.StatusCompleted,
			order.StatusCompleted,
			order.StatusShipped, order.StatusShipped,
			order.StatusPaid, order.StatusPaid,
			order.StatusRefunded,
			order.StatusPending, order.StatusPending, order.StatusPending,
		}
	}
	return pool[rand.IntN(len(pool))]
}

// updatedAtFor 按状态给出「最后一次流转」的时间偏移，保证 updated_at 不早于 created_at；
// 延迟超过当前时刻时收敛到当前时刻，避免生成未来时间。
func updatedAtFor(status order.Status, createdAt, now time.Time) time.Time {
	var delay time.Duration
	switch status {
	case order.StatusPending:
		delay = 0
	case order.StatusPaid:
		delay = time.Duration(5+rand.IntN(115)) * time.Minute // 5 分钟 ~ 2 小时
	case order.StatusShipped:
		delay = time.Duration(2+rand.IntN(10)) * time.Hour // 2 ~ 12 小时
	case order.StatusCompleted:
		delay = time.Duration(24+rand.IntN(48)) * time.Hour // 1 ~ 3 天
	case order.StatusRefunded:
		delay = time.Duration(1+rand.IntN(48)) * time.Hour // 1 小时 ~ 2 天
	}
	updated := createdAt.Add(delay)
	if updated.After(now) {
		updated = now
	}
	return updated
}
