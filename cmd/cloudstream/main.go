package main

import (
	"cloudstream/internal/api"
	"cloudstream/internal/core"
	"cloudstream/internal/database"
	"cloudstream/internal/logger"
	"fmt"
	"github.com/rs/zerolog/log"
)

func main() {
	// 初始化全局日志（所有日志操作前的前置步骤）
	logger.Init()

	// 数据库文件路径
	const dbPath = "./data/cloudstream.db"
	if err := database.ConnectDatabase(dbPath); err != nil {
		// 日志：数据库连接失败（致命错误，含详情）
		log.Fatal().Err(err).Msg("无法连接到数据库")
	}

	// 初始化任务调度器
	core.InitScheduler()
	// 初始化API路由（注册所有接口映射）
	r := api.InitRouter()

	// 服务监听地址（0.0.0.0=监听所有网卡，端口12398）
	listenAddr := "0.0.0.0:12398"

	// 日志：服务启动成功（携带监听地址）
	log.Info().Str("address", listenAddr).Msg("服务已启动")

	// 用户提示：访问方式、初始账号及安全提醒
	fmt.Printf("\n🚀 服务已启动! 请通过浏览器访问: http://<您的服务器IP>:%s\n", "12398")
	fmt.Println("   - 用户名: admin")
	fmt.Println("   - 密码: 首次启动密码为 admin")
	fmt.Println("   - 重要: 登录后，请通过右上角菜单的“账户安全”修改您的管理员帐号密码。")
	fmt.Println()

	if err := r.Run(listenAddr); err != nil {
		// 日志：服务启动失败（致命错误，含详情）
		log.Fatal().Err(err).Msg("服务启动失败")
	}
}
