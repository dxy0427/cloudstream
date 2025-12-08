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
	// 初始化日志
	logger.Init()

	const dbPath = "./data/cloudstream.db"
	if err := database.ConnectDatabase(dbPath); err != nil {
		log.Fatal().Err(err).Msg("无法连接到数据库")
	}

	// 初始化调度器
	core.InitScheduler()

	// 初始化路由
	r := api.InitRouter()

	listenAddr := "0.0.0.0:12398"
	log.Info().Str("address", listenAddr).Msg("主服务已启动")

	fmt.Printf("\n🚀 CloudStream 服务已启动! \n")
	fmt.Printf(" - 控制面板: http://<IP>:12398\n\n")

	if err := r.Run(listenAddr); err != nil {
		log.Fatal().Err(err).Msg("服务启动失败")
	}
}