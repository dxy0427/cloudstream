package main

import (
	"cloudstream/internal/api"
	"cloudstream/internal/core"
	"cloudstream/internal/database"
	"cloudstream/internal/logger"
	"cloudstream/internal/mediaserver"
	"context"
	"fmt"
	"github.com/rs/zerolog/log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
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

	// 初始化媒体服务器管理器
	if err := mediaserver.GetManager().ReloadAll(); err != nil {
		log.Warn().Err(err).Msg("加载媒体服务器配置失败")
	}

	// 初始化路由
	r := api.InitRouter()

	// 启动主服务（12398端口）
	listenAddr := "0.0.0.0:12398"
	srv := &http.Server{
		Addr:    listenAddr,
		Handler: r,
	}

	go func() {
		log.Info().Str("address", listenAddr).Msg("主服务已启动")
		fmt.Printf("\n🚀 CloudStream 服务已启动! \n")
		fmt.Printf(" - 控制面板: http://<IP>:12398\n\n")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("服务启动失败")
		}
	}()

	// 启动媒体服务器代理服务（8091端口）
	proxyAddr := "0.0.0.0:8091"
	proxyRouter := mediaserver.InitProxyRouter()
	proxySrv := &http.Server{
		Addr:    proxyAddr,
		Handler: proxyRouter,
	}

	go func() {
		log.Info().Str("address", proxyAddr).Msg("媒体服务器代理服务已启动")
		fmt.Printf(" - 媒体服务器代理: http://<IP>:8091\n\n")
		if err := proxySrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("媒体服务器代理服务启动失败")
		}
	}()

	// 优雅停机 (Graceful Shutdown)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info().Msg("正在停止服务...")

	// 给予 5 秒时间让正在处理的请求完成
	// 两个服务各用独立 context，避免第一个超时影响第二个
	ctx1, cancel1 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel1()
	if err := srv.Shutdown(ctx1); err != nil {
		log.Error().Err(err).Msg("主服务强制停止")
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if err := proxySrv.Shutdown(ctx2); err != nil {
		log.Error().Err(err).Msg("代理服务强制停止")
	}

	log.Info().Msg("服务已退出")
}
