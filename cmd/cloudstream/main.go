package main

import (
	"cloudstream/internal/api"
	"cloudstream/internal/core"
	"cloudstream/internal/database"
	"cloudstream/internal/logger"
	"cloudstream/internal/mediaserver"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
)

func main() {
	logger.Init()

	const dbPath = "./data/cloudstream.db"
	if err := database.ConnectDatabase(dbPath); err != nil {
		log.Fatal().Err(err).Msg("无法连接到数据库")
	}

	core.InitScheduler()

	if err := mediaserver.GetManager().ReloadAll(); err != nil {
		log.Warn().Err(err).Msg("加载媒体服务器配置失败")
	}

	r := api.InitRouter()

	listenAddr := "0.0.0.0:12398"
	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}

	go func() {
		log.Info().Str("address", listenAddr).Msg("主服务已启动")
		fmt.Printf("\n🚀 CloudStream 服务已启动! \n")
		fmt.Printf(" - 控制面板: http://<IP>:12398\n\n")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("服务启动失败")
		}
	}()

	proxyAddr := "0.0.0.0:8091"
	proxyRouter := mediaserver.InitProxyRouter()
	proxySrv := &http.Server{
		Addr:              proxyAddr,
		Handler:           proxyRouter,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}

	go func() {
		log.Info().Str("address", proxyAddr).Msg("媒体服务器代理服务已启动")
		fmt.Printf(" - 媒体服务器代理: http://<IP>:8091\n\n")
		if err := proxySrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("媒体服务器代理服务启动失败")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info().Msg("正在停止服务...")
	var shutdownWG sync.WaitGroup
	shutdownWG.Add(2)
	go shutdownHTTPServer(&shutdownWG, "主服务", srv)
	go shutdownHTTPServer(&shutdownWG, "代理服务", proxySrv)
	shutdownWG.Wait()

	mediaserver.GetManager().CloseAll()
	if !core.ShutdownScheduler(30 * time.Second) {
		log.Error().Msg("任务调度器或运行中任务停止超时")
	}

	log.Info().Msg("服务已退出")
}

func shutdownHTTPServer(wg *sync.WaitGroup, name string, server *http.Server) {
	defer wg.Done()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Error().Err(err).Str("server", name).Msg("服务优雅停止超时，正在强制关闭")
		if closeErr := server.Close(); closeErr != nil {
			log.Error().Err(closeErr).Str("server", name).Msg("关闭服务连接失败")
		}
	}
}
