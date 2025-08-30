package main

import (
	"cloudstream/internal/api"
	"cloudstream/internal/core"
	"cloudstream/internal/database"
	"cloudstream/internal/logger"
	"fmt"
	"github.com/rs/zerolog/log"
)

// main：CloudStream服务入口，负责初始化核心组件并启动HTTP服务
func main() {
	// 初始化全局日志（必须在所有日志操作前执行，确保日志正常输出）
	logger.Init()

	// 数据库文件路径（存储应用核心数据：用户、任务、云账户配置等）
	const dbPath = "./data/cloudstream.db"
	if err := database.ConnectDatabase(dbPath); err != nil {
		// 日志：数据库连接失败（致命错误，直接终止程序，含错误详情便于排查）
		log.Fatal().Err(err).Msg("无法连接到数据库")
	}

	// 初始化任务调度器（加载已启用的定时任务，启动后台调度逻辑）
	core.InitScheduler()
	// 初始化API路由（注册所有HTTP接口的路由规则，关联对应处理器）
	r := api.InitRouter()

	// 服务监听地址（0.0.0.0表示监听服务器所有网卡，端口固定为12398）
	listenAddr := "0.0.0.0:12398"

	// 日志：记录服务启动状态（携带监听地址，便于运维确认服务端口）
	log.Info().Str("address", listenAddr).Msg("服务已启动")

	// 用户友好提示：输出访问方式、初始账号及安全操作提醒（仅面向人工查看，非程序日志）
	fmt.Printf("\n🚀 服务已启动! 请通过浏览器访问: http://<您的服务器IP>:%s\n", "12398")
	fmt.Println("   - 用户名: admin")
	fmt.Println("   - 密码: 首次启动密码为 admin")
	fmt.Println("   - 重要: 登录后，请通过右上角菜单的“账户安全”修改您的管理员帐号密码。")
	fmt.Println()

	// 启动HTTP服务，监听指定地址；启动失败则记录致命错误并终止
	if err := r.Run(listenAddr); err != nil {
		log.Fatal().Err(err).Msg("服务启动失败")
	}
}
