package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/744223454/orderhub/internal/auth"
	"github.com/744223454/orderhub/internal/order"
	"github.com/744223454/orderhub/internal/user"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// defaultJWTExpire 是令牌默认有效期，可由环境变量 JWT_EXPIRE 覆盖。
const defaultJWTExpire = 24 * time.Hour

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
	jwtExpire, err := loadJWTExpire()
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
	if err := db.AutoMigrate(&user.User{}, &order.Order{}); err != nil {
		panic("数据库自动迁移失败: " + err.Error())
	}

	repo := user.NewRepository(db)
	service := user.NewService(repo)
	// 通过注入的方式交出签发能力，避免 user 模块反向依赖 auth 模块。
	handler := user.NewHandler(service, func(u *user.User) (string, error) {
		return auth.GenerateToken(u, jwtSecret, jwtExpire)
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

	// 公开路由：注册 / 登录
	api := router.Group("/api")
	user.AuthRoutes(api, handler)

	// 登录层：需携带有效 JWT
	authed := api.Group("", auth.Auth(jwtSecret))
	authed.GET("/me", handler.Me)
	order.UserRoutes(authed, orderHandler)

	// 角色层：仅运营 / 管理员可访问（订单管理与用户管理接口）
	staff := authed.Group("", auth.RequireRole(user.RoleAdmin, user.RoleOps))
	order.StaffRoutes(staff, orderHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8888"
	}
	if err := router.Run(":" + port); err != nil {
		panic("启动 HTTP 服务失败: " + err.Error())
	}
}

// loadJWTExpire 读取 JWT_EXPIRE 环境变量并解析为时长，未设置时返回默认值。
func loadJWTExpire() (time.Duration, error) {
	raw := os.Getenv("JWT_EXPIRE")
	if raw == "" {
		return defaultJWTExpire, nil
	}
	expire, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("JWT_EXPIRE 格式非法（应形如 24h / 30m）: %w", err)
	}
	if expire <= 0 {
		return 0, errors.New("JWT_EXPIRE 必须为正数时长")
	}
	return expire, nil
}
