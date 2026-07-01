package mediaserver

import (
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// isWebSocketRequest 判断是否为 WebSocket 请求
func isWebSocketRequest(c *gin.Context) bool {
	if strings.EqualFold(c.GetHeader("Connection"), "upgrade") &&
		strings.EqualFold(c.GetHeader("Upgrade"), "websocket") {
		return true
	}
	path := c.Request.URL.Path
	return strings.HasSuffix(path, "/embywebsocket") || strings.HasSuffix(path, "/socket")
}

// handleWebSocket 代理 WebSocket 连接（参考 MediaRelay 实现）
func handleWebSocket(c *gin.Context, serverID uint) {
	svr, exists := GetManager().GetServer(serverID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "媒体服务器未找到"})
		return
	}

	hostUrl := svr.cfg.Server.Addr
	if !strings.HasPrefix(hostUrl, "http://") && !strings.HasPrefix(hostUrl, "https://") {
		hostUrl = "http://" + hostUrl
	}

	// 升级客户端连接
	clientWs, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Error().Err(err).Msg("WebSocket 升级失败")
		return
	}
	defer clientWs.Close()

	// 构建后端 WebSocket URL
	backendUrl := strings.Replace(hostUrl, "http://", "ws://", 1)
	backendUrl = strings.Replace(backendUrl, "https://", "wss://", 1)
	backendUrl += c.Request.URL.Path
	if c.Request.URL.RawQuery != "" {
		backendUrl += "?" + c.Request.URL.RawQuery
	}

	// 连接后端 WebSocket
	header := http.Header{}
	if apiKey := svr.cfg.Server.Auth; apiKey != "" {
		header.Set("X-Emby-Token", apiKey)
	}
	header.Set("User-Agent", c.Request.UserAgent())

	backendWs, _, err := websocket.DefaultDialer.Dial(backendUrl, header)
	if err != nil {
		log.Error().Err(err).Str("url", backendUrl).Msg("连接后端 WebSocket 失败")
		return
	}
	defer backendWs.Close()

	log.Info().Str("path", c.Request.URL.Path).Msg("WebSocket 代理已建立")

	// 双向转发
	var wg sync.WaitGroup
	wg.Add(2)

	// 客户端 → 后端
	go func() {
		defer wg.Done()
		defer backendWs.Close()
		for {
			msgType, msg, err := clientWs.ReadMessage()
			if err != nil {
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					log.Debug().Err(err).Msg("客户端 WebSocket 读取结束")
				}
				return
			}
			if err := backendWs.WriteMessage(msgType, msg); err != nil {
				log.Debug().Err(err).Msg("后端 WebSocket 写入失败")
				return
			}
		}
	}()

	// 后端 → 客户端
	go func() {
		defer wg.Done()
		defer clientWs.Close()
		for {
			msgType, msg, err := backendWs.ReadMessage()
			if err != nil {
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					log.Debug().Err(err).Msg("后端 WebSocket 读取结束")
				}
				return
			}
			if err := clientWs.WriteMessage(msgType, msg); err != nil {
				log.Debug().Err(err).Msg("客户端 WebSocket 写入失败")
				return
			}
		}
	}()

	wg.Wait()
	log.Info().Str("path", c.Request.URL.Path).Msg("WebSocket 代理已关闭")
}
