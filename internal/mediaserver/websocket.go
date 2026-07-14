package mediaserver

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

const maxWebSocketMessageSize int64 = 8 << 20

// isWebSocketRequest 判断是否为 WebSocket 请求
func isWebSocketRequest(c *gin.Context) bool {
	return websocket.IsWebSocketUpgrade(c.Request)
}

// handleWebSocket 先连接后端，成功后再升级客户端，避免后端失败时提前返回 101。
func handleWebSocket(c *gin.Context, svr *ProxyServer, upstreamPath string) {
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	go func() {
		select {
		case <-svr.shutdown:
			cancel()
		case <-ctx.Done():
		}
	}()

	backendURL, err := svr.buildTargetURL(upstreamPath, c.Request.URL.RawQuery)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": "无效的 WebSocket 路径"})
		return
	}
	if backendURL.Scheme == "https" {
		backendURL.Scheme = "wss"
	} else {
		backendURL.Scheme = "ws"
	}

	header := http.Header{}
	if apiKey := svr.cfg.Server.Auth; apiKey != "" {
		header.Set("X-Emby-Token", apiKey)
	}
	header.Set("User-Agent", c.Request.UserAgent())

	requestedProtocols := websocket.Subprotocols(c.Request)
	dialer := websocket.Dialer{
		Proxy: http.ProxyFromEnvironment,
		NetDialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		HandshakeTimeout: 10 * time.Second,
		Subprotocols:     requestedProtocols,
	}
	backendWs, backendResp, err := dialer.DialContext(ctx, backendURL.String(), header)
	if err != nil {
		status := http.StatusBadGateway
		if backendResp != nil {
			if backendResp.StatusCode >= http.StatusBadRequest && backendResp.StatusCode <= 599 {
				status = backendResp.StatusCode
			}
			_ = backendResp.Body.Close()
		}
		log.Error().
			Str("target", urlForLog(backendURL.String())).
			Str("error", errorWithoutURL(err)).
			Msg("连接后端 WebSocket 失败")
		c.JSON(status, gin.H{"code": 1, "message": "连接后端 WebSocket 失败"})
		return
	}
	defer backendWs.Close()
	backendWs.SetReadLimit(maxWebSocketMessageSize)

	selectedProtocol := backendWs.Subprotocol()
	if selectedProtocol != "" && !containsString(requestedProtocols, selectedProtocol) {
		log.Warn().
			Str("target", urlForLog(backendURL.String())).
			Str("subprotocol", selectedProtocol).
			Msg("后端选择了客户端未请求的 WebSocket 子协议")
		c.JSON(http.StatusBadGateway, gin.H{"code": 1, "message": "后端 WebSocket 子协议无效"})
		return
	}

	upgrader := websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool {
			return true
		},
	}
	if selectedProtocol != "" {
		upgrader.Subprotocols = []string{selectedProtocol}
	}
	clientWs, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Error().Err(err).Msg("WebSocket 升级失败")
		return
	}
	defer clientWs.Close()
	clientWs.SetReadLimit(maxWebSocketMessageSize)

	log.Info().Str("path", logPath(backendURL)).Msg("WebSocket 代理已建立")

	// 双向转发
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		<-ctx.Done()
		_ = clientWs.Close()
		_ = backendWs.Close()
	}()

	// 客户端 → 后端
	go func() {
		defer wg.Done()
		defer cancel()
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
		defer cancel()
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
	log.Info().Str("path", logPath(backendURL)).Msg("WebSocket 代理已关闭")
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func logPath(target *url.URL) string {
	if target == nil || target.EscapedPath() == "" {
		return "/"
	}
	return target.EscapedPath()
}
