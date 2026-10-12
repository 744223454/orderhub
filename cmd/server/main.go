package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/744223454/orderhub/internal/auth"
	"github.com/744223454/orderhub/internal/order"
	"github.com/744223454/orderhub/internal/user"
	"github.com/744223454/orderhub/internal/wecom"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// defaultAccessExpire 是访问令牌的默认有效期，可由环境变量 ACCESS_EXPIRE 覆盖。
// 短寿命（分钟级）是刻意选择：配合刷新令牌机制，把令牌泄露的可用窗口压到最小。
const defaultAccessExpire = 15 * time.Minute

// defaultRefreshExpire 是刷新令牌的默认有效期，可由环境变量 REFRESH_EXPIRE 覆盖。
// 长寿命（周级）换取「用户长期不用重新登录」，同时它是可撤销的有状态令牌。
const defaultRefreshExpire = 168 * time.Hour

// @title 多角色订单系统 API
// @version 1.0
// @description 多角色订单系统后端接口：用户注册、登录与 JWT 认证，以及订单的下单、支付、发货、完成与退款。
// @host localhost:8888
// @BasePath /api
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
func main() {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		panic("加载 .env 文件失败: " + err.Error())
	}

	// JWT 签名密钥：缺失则拒绝启动
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		panic("未设置 JWT_SECRET 环境变量")
	}
	accessExpire, err := loadDuration("ACCESS_EXPIRE", defaultAccessExpire)
	if err != nil {
		panic(err.Error())
	}
	refreshExpire, err := loadDuration("REFRESH_EXPIRE", defaultRefreshExpire)
	if err != nil {
		panic(err.Error())
	}

	config := os.Getenv("DATABASE_URL")
	if config == "" {
		panic("未设置 DATABASE_URL 环境变量")
	}
	db, err := gorm.Open(postgres.Open(config), &gorm.Config{TranslateError: true})
	if err != nil {
		panic("连接数据库失败: " + err.Error())
	}
	if err := db.AutoMigrate(&user.User{}, &user.UserIdentity{}, &user.RefreshToken{}, &order.Order{}); err != nil {
		panic("数据库自动迁移失败: " + err.Error())
	}

	repo := user.NewRepository(db)
	service := user.NewService(repo, refreshExpire)
	// 通过注入的方式交出签发能力，避免 user 模块反向依赖 auth 模块。
	// 注入的只负责访问令牌（短命 JWT）；刷新令牌由 user 模块自己管理（有状态、需查表撤销）。
	handler := user.NewHandler(service, func(u *user.User) (string, error) {
		return auth.GenerateToken(u, jwtSecret, accessExpire)
	})

	orderRepo := order.NewRepository(db)
	orderService := order.NewService(orderRepo)
	orderHandler := order.NewHandler(orderService)

	router := gin.Default()
	router.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "Hello World",
		})
	})

	// 公开路由：注册 / 登录 / 刷新 / 登出
	api := router.Group("/api")
	user.AuthRoutes(api, handler)

	// 登录层：需携带有效 JWT
	authed := api.Group("", auth.Auth(jwtSecret))
	user.MeRoutes(authed, handler)
	order.UserRoutes(authed, orderHandler)

	// 角色层：仅运营 / 管理员可访问（订单管理与用户管理接口）
	staff := authed.Group("", auth.RequireRole(user.RoleAdmin, user.RoleOps))
	order.StaffRoutes(staff, orderHandler)

	// 管理员专属层：仅管理员可访问（用户管理与角色变更）
	adminOnly := authed.Group("", auth.RequireRole(user.RoleAdmin))
	user.AdminRoutes(adminOnly, handler)

	// 企业微信接入：可选能力。
	// 扫码登录的接口只在配置齐备时注册；通讯录接口始终注册（未配置时返回 503）。
	wecomClient, err := loadWecomClient()
	if err != nil {
		panic(err.Error())
	}
	if wecomClient == nil {
		log.Println("未配置企业微信登录（WECOM_* 全部为空），扫码登录接口未注册；通讯录接口将返回 503")
	}
	wecomHandler := newWecomHandler(wecomClient, service, jwtSecret, accessExpire)
	if wecomClient != nil {
		wecom.PublicRoutes(api, wecomHandler)
		wecom.AuthedRoutes(authed, wecomHandler)
	}
	wecom.AdminRoutes(adminOnly, wecomHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8888"
	}
	if err := router.Run(":" + port); err != nil {
		panic("启动 HTTP 服务失败: " + err.Error())
	}
}

// loadDuration 读取指定名称的环境变量并解析为时长，未设置时返回默认值。
// 解析失败或非正数时返回错误——让错误配置在启动时就暴露，而不是运行期才发作。
func loadDuration(key string, def time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return def, nil
	}
	duration, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s 格式非法（应形如 15m / 168h）: %w", key, err)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("%s 必须为正数时长", key)
	}
	return duration, nil
}

// loadWecomClient 依据环境变量创建企业微信客户端。
//
// 处理策略刻意区别于 JWT_SECRET / DATABASE_URL 的「缺失即拒绝启动」：
// 企业微信登录是可选能力，本地不接入它也应该能正常跑起来。因此
//   - 四项配置**全缺** → 返回 nil，调用方跳过注册相关路由；
//   - 只配了一部分 → 视为配置写错，直接拒绝启动。半套配置只会产出一堆
//     难以定位的 500（比如 Secret 没填却去建号），不如在启动时就说清楚。
func loadWecomClient() (*wecom.Client, error) {
	cfg := wecom.Config{
		CorpID:     os.Getenv("WECOM_CORP_ID"),
		AgentID:    os.Getenv("WECOM_AGENT_ID"),
		Secret:     os.Getenv("WECOM_SECRET"),
		WebBaseURL: os.Getenv("WECOM_WEB_BASE_URL"),
	}

	missing := cfg.Missing()
	switch {
	case len(missing) == 0:
		return wecom.NewClient(cfg), nil
	// 用零值配置反推字段总数：全缺 ⇔ 缺失数量等于字段总数。
	// 这样将来给 Config 加字段时不必回头改这里的判断。
	case len(missing) == len(wecom.Config{}.Missing()):
		return nil, nil
	default:
		return nil, fmt.Errorf("企业微信配置不完整，缺少: %s", strings.Join(missing, ", "))
	}
}

// newWecomHandler 装配企业微信接口处理器。
//
// user 模块与 wecom 模块之间用「函数注入」相连，与 user.TokenIssuer 的做法一致：
// wecom 因此不需要引用 auth 包，user 也不需要知道企业微信的存在，依赖方向仍是
// wecom → user 单向。
//
// client 允许为 nil（未配置企业微信）：此时扫码登录的接口不会被注册，
// 通讯录接口由处理器返回 503。因为要兜住 nil，登录 / 绑定的闭包在这里就建好，
// 而不是等确认配置齐备后再建。
func newWecomHandler(
	client *wecom.Client,
	service *user.Service,
	jwtSecret string,
	accessExpire time.Duration,
) *wecom.Handler {
	// 把企业微信 userid 换成「本地账号 + 令牌对」：首次登录自动建号。
	// 与密码登录一致：登录必须成套签发访问令牌与刷新令牌（刷新令牌由 user 模块管理），
	// 否则扫码登录的用户拿不到续期能力。
	login := func(ctx context.Context, externalID string) (*user.User, string, string, error) {
		u, err := service.LoginByExternal(ctx, user.ProviderWecom, externalID)
		if err != nil {
			return nil, "", "", err
		}
		token, err := auth.GenerateToken(u, jwtSecret, accessExpire)
		if err != nil {
			return nil, "", "", err
		}
		refresh, err := service.IssueRefresh(ctx, u.ID)
		if err != nil {
			return nil, "", "", err
		}
		return u, token, refresh, nil
	}

	// 把企业微信身份绑定到当前登录账号。
	bind := func(ctx context.Context, userID uint, externalID string) error {
		return service.BindExternal(ctx, userID, user.ProviderWecom, externalID)
	}

	return wecom.NewHandler(client, login, bind)
}
