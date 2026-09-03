//go:build desktop

package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"sync"
	"time"

	"InkFlow/core"
	"InkFlow/global"
	"InkFlow/initialize"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

// desktopAssets 是桌面发布包内置的 Vue 构建产物。业务 API、SSE 和 WebSocket
// 不由 Wails AssetServer 转发，而是继续由下方的 Gin 服务直接提供。
//
//go:embed all:web/dist
var desktopAssets embed.FS

type desktopApplication struct {
	server        *http.Server
	listener      net.Listener
	closeServices func()
	apiBase       string
	sessionToken  string
	shutdownOnce  sync.Once
}

func main() {
	global.GVA_VP = core.InitializeClientViper()
	paths, err := core.ConfigureClientRuntime()
	if err != nil {
		panic(fmt.Errorf("configure desktop runtime: %w", err))
	}

	app := &desktopApplication{}
	app.closeServices = initialize.InitializeServices()
	if err := app.startLocalServer(initialize.Routers()); err != nil {
		app.shutdown()
		panic(err)
	}

	assets, err := fs.Sub(desktopAssets, "web/dist")
	if err != nil {
		app.shutdown()
		panic(fmt.Errorf("load desktop assets: %w", err))
	}

	err = wails.Run(&options.App{
		Title: "InkFlow",
		// Keep the first window within common laptop work areas after Windows
		// DPI scaling. Oversized dimensions are clamped to the top edge and
		// obscure other applications behind the client.
		Width:            1200,
		Height:           760,
		MinWidth:         960,
		MinHeight:        600,
		BackgroundColour: options.NewRGB(247, 250, 248),
		AssetServer: &assetserver.Options{
			Assets:  assets,
			Handler: http.HandlerFunc(app.handleAssetFallback),
		},
		Windows: &windows.Options{
			WebviewUserDataPath: paths.WebViewData,
		},
		OnShutdown: func(context.Context) {
			app.shutdown()
		},
	})
	app.shutdown()
	if err != nil {
		panic(err)
	}
}

func (a *desktopApplication) startLocalServer(handler http.Handler) error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen on local loopback: %w", err)
	}
	a.listener = listener
	a.apiBase = "http://" + listener.Addr().String()
	a.server = &http.Server{Handler: handler}
	go func() {
		if err := a.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			fmt.Printf("InkFlow local service stopped unexpectedly: %v\n", err)
		}
	}()
	return nil
}

// handleAssetFallback 只为内嵌前端提供运行时配置，不能把 API 请求交给
// Wails AssetServer，否则 Windows 下的 SSE 响应会失去流式能力。
func (a *desktopApplication) handleAssetFallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Path != "/inkflow-runtime.js" {
		http.NotFound(w, r)
		return
	}
	config, err := json.Marshal(struct {
		APIBase                   string `json:"apiBase"`
		InferenceProvider         string `json:"inferenceProvider"`
		BackendEmbeddingReady     bool   `json:"backendEmbeddingReady"`
		BackendEmbeddingModelPath string `json:"backendEmbeddingModelPath"`
		BackendRerankReady        bool   `json:"backendRerankReady"`
		BackendRerankModelPath    string `json:"backendRerankModelPath"`
		SessionToken              string `json:"sessionToken"`
	}{
		APIBase:                   a.apiBase,
		InferenceProvider:         global.GVA_CONFIG.LLM.InferenceProvider,
		BackendEmbeddingReady:     global.GVA_LLM_EMBEDDING != nil,
		BackendEmbeddingModelPath: global.GVA_CONFIG.LLMLocal.Embedding.ModelPath,
		BackendRerankReady:        global.GVA_LLM_RERANK != nil,
		BackendRerankModelPath:    global.GVA_CONFIG.LLMLocal.Rerank.ModelPath,
		SessionToken:              a.sessionToken,
	})
	if err != nil {
		http.Error(w, "encode desktop runtime configuration", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprintf(w, "globalThis.__INKFLOW_RUNTIME__=%s;", config)
}

func (a *desktopApplication) shutdown() {
	a.shutdownOnce.Do(func() {
		if a.server != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = a.server.Shutdown(ctx)
			cancel()
		}
		if a.closeServices != nil {
			a.closeServices()
		}
	})
}
