package main

import (
	"cloudstream/internal/api"
	"cloudstream/internal/core"
	"cloudstream/internal/database"
	"cloudstream/internal/logger"
	"fmt"
	"github.com/rs/zerolog/log"
)

// main：CloudStream服务入口：初始化核心组件并启动HTTP服务
func main() {
	// 初始化全局日志（日志操作前执行）
	logger.Init()

	// 数据库路径：存储用户、任务、账户配置
	const dbPath = "./data/cloudstream.db"
	if err := database.ConnectDatabase(dbPath); err != nil {
		// 数据库连接失败，终止服务
		log.Fatal().Err(err).Msg("无法连接到数据库")
	}

	// 初始化任务调度器
	core.InitScheduler()
	// 初始化API路由
	r := api.InitRouter()

	// 服务监听地址：0.0.0.0（所有网卡）:12398
	listenAddr := "0.0.0.0:12398"

	// 记录服务启动状态
	log.Info().Str("address", listenAddr).Msg("服务已启动")

	// 用户访问提示
	fmt.Printf("\n🚀 服务已启动! 请通过浏览器访问: http://<您的服务器IP>:%s\n", "12398")
	fmt.Println("   - 用户名: admin")
	fmt.Println("   - 密码: 首次启动密码为 admin")
	fmt.Println("   - 重要: 登录后，请通过右上角菜单的“账户安全”修改您的管理员帐号密码。")
	fmt.Println()

	// 启动HTTP服务，失败终止
	if err := r.Run(listenAddr); err != nil {
		log.Fatal().Err(err).Msg("服务启动失败")
	}
}
