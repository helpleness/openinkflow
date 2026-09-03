package initialize

import (
	"InkFlow/global"
	"InkFlow/middleware"
	appRouter "InkFlow/router"
	appService "InkFlow/service"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// 1. 模型安全白名单：只允许下载和缓存你前端用到的这两个模型
// allowedModels 限制模型代理只能访问客户端实际使用的 Embedding 和 Rerank 模型。
var allowedModels = []string{
	"onnx-community/Qwen3-Embedding-0.6B-ONNX",
	"onnx-community/bge-reranker-v2-m3-ONNX",
}

// 2. 并发下载锁控制，防止多个用户同时触发同一个文件的下载请求
var (
	// downloadLocks 保存“缓存文件路径 -> 下载锁”的映射，globalMutex 保护锁的创建过程。
	downloadLocks sync.Map
	globalMutex   sync.Mutex
)

// Routers 创建 Gin 路由，注册基础健康检查、模型缓存服务和 Web 静态资源。
func Routers() *gin.Engine {
	Router := gin.Default()
	_ = Router.SetTrustedProxies([]string{"127.0.0.1", "::1"})
	Router.Use(func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if isTrustedLocalOrigin(origin) {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			// 系统管理 API 通过租户头区分数据；浏览器推理 worker 使用独立客户端
			// 标识，因此预检必须允许两个自定义请求头。
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-InkFlow-Inference-Client, X-InkFlow-Tenant-ID, X-InkFlow-Desktop-Client")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		}
		if c.Request.Method == http.MethodOptions {
			if origin != "" && !isTrustedLocalOrigin(origin) {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})

	Router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "msg": "InkFlow document-writing foundation is running"})
	})
	// GVA-style router injection: authenticated routes use Router, while login
	// and registration use PublicRouter. Route-specific authorization remains
	// explicit beside the individual endpoint declaration.
	systemPrivateRouter := Router.Group("", middleware.SystemAuth(), middleware.SystemAudit())
	systemPublicRouter := Router.Group("")
	routes := appRouter.RouterGroupApp
	systemRoutes := routes.SystemRouterGroup
	systemRoutes.SysApiRouter.InitSysApiRouter(systemPrivateRouter, systemPublicRouter)
	systemRoutes.SysAuthRouter.InitSysAuthRouter(systemPrivateRouter, systemPublicRouter)
	systemRoutes.SysTenantRouter.InitSysTenantRouter(systemPrivateRouter, systemPublicRouter)
	systemRoutes.SysOrganizationRouter.InitSysOrganizationRouter(systemPrivateRouter, systemPublicRouter)
	systemRoutes.SysMembershipRouter.InitSysMembershipRouter(systemPrivateRouter, systemPublicRouter)
	systemRoutes.SysMembershipApplicationRouter.InitSysMembershipApplicationRouter(systemPrivateRouter, systemPublicRouter)
	systemRoutes.SysRoleRouter.InitSysRoleRouter(systemPrivateRouter, systemPublicRouter)
	systemRoutes.SysMenuRouter.InitSysMenuRouter(systemPrivateRouter, systemPublicRouter)
	systemRoutes.SysAuditRouter.InitSysAuditRouter(systemPrivateRouter, systemPublicRouter)
	systemRoutes.SysModelSettingRouter.InitSysModelSettingRouter(systemPrivateRouter, systemPublicRouter)
	systemRoutes.SysInferenceRouter.InitSysInferenceRouter(systemPrivateRouter, systemPublicRouter)
	routes.OfficialDocRouterGroup.InitOfficialDocRouter(systemPrivateRouter, systemPublicRouter)
	if err := appService.ServiceGroupApp.SystemServiceGroup.SysApiService.SyncSysApis(context.Background(), Router.Routes()); err != nil {
		panic(fmt.Errorf("sync system APIs: %w", err))
	}

	registerHFCachedServer(Router)
	// Web 开发/服务端模式没有桌面壳注入配置时保持空对象；桌面模式由
	// Wails AssetServer 的同名动态资源覆盖，向前端提供随机 loopback 端口。
	Router.GET("/inkflow-runtime.js", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Data(http.StatusOK, "application/javascript; charset=utf-8", []byte("globalThis.__INKFLOW_RUNTIME__={};"))
	})

	if frontendDir, ok := frontendDistDirectory(); ok {
		Router.Static("/assets", filepath.Join(frontendDir, "assets"))
		Router.NoRoute(func(c *gin.Context) {
			if isBackendAPIPath(c.Request.URL.Path) {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "not found"})
				return
			}
			c.File(filepath.Join(frontendDir, "index.html"))
		})
	}

	return Router
}

// isBackendAPIPath identifies backend endpoints that must return JSON 404 rather
// than falling through to the frontend single-page application. API prefixes are
// deliberately not assumed: deployment-level prefixes belong to Nginx.
func isBackendAPIPath(requestPath string) bool {
	return requestPath == "/health" ||
		strings.HasPrefix(requestPath, "/auth/") ||
		strings.HasPrefix(requestPath, "/system/") ||
		strings.HasPrefix(requestPath, "/officialdoc/") ||
		strings.HasPrefix(requestPath, "/inference/") ||
		strings.HasPrefix(requestPath, "/hf-mirror/")
}

// getFileLock 获取指定路径的专属锁
// getFileLock 为每个缓存文件返回独立互斥锁，防止并发请求重复下载同一文件。
func getFileLock(path string) *sync.Mutex {
	globalMutex.Lock()
	defer globalMutex.Unlock()

	if l, exists := downloadLocks.Load(path); exists {
		return l.(*sync.Mutex)
	}
	newLock := &sync.Mutex{}
	downloadLocks.Store(path, newLock)
	return newLock
}

// isPathAllowed 校验请求路径是否包含白名单中的模型
// isPathAllowed 检查 Hugging Face 请求路径是否属于允许缓存的模型白名单。
func isPathAllowed(path string) bool {
	for _, model := range allowedModels {
		if strings.Contains(path, model) {
			return true
		}
	}
	return false
}

// registerHFCachedServer 注册全自动按需下载缓存与防刷限速服务
// registerHFCachedServer 注册模型文件代理：优先读取本地缓存，缺失时按需下载。
func registerHFCachedServer(router *gin.Engine) {
	router.GET("/hf-mirror/*path", func(c *gin.Context) {
		hfPath := c.Param("path")

		// 【防御机制 1】白名单判定：防止恶意刷非相关模型或恶意路由
		if !isPathAllowed(hfPath) {
			c.JSON(http.StatusForbidden, gin.H{"msg": "请求的模型不在系统白名单内，禁止访问"})
			return
		}
		localPath, err := modelCachePath(hfPath)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"msg": "invalid model cache path"})
			return
		}

		// 【安全机制 2】如果本地文件不存在，触发从 Hugging Face 自动下载逻辑
		if _, err := os.Stat(localPath); os.IsNotExist(err) {
			// 获取该文件的下载锁，防止多并发重复请求
			fileLock := getFileLock(localPath)
			fileLock.Lock()

			// 双重检查：可能在排队等锁期间，上一个协程已经把文件下载好了
			if _, err := os.Stat(localPath); os.IsNotExist(err) {
				fmt.Printf("[HF-Cache] 本地未找到文件，正在从官方下载并缓存: %s\n", hfPath)

				// 执行安全的原子下载
				downloadErr := downloadFromHF(hfPath, localPath)
				if downloadErr != nil {
					fileLock.Unlock()
					c.JSON(http.StatusInternalServerError, gin.H{"msg": "从 Hugging Face 自动同步模型失败: " + downloadErr.Error()})
					return
				}
			}
			fileLock.Unlock()
		}

		// 3. 打开本地已经就绪的文件准备流式返回
		file, err := os.Open(localPath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"msg": "打开本地缓存模型失败"})
			return
		}
		defer file.Close()

		fileInfo, err := file.Stat()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"msg": "读取文件状态失败"})
			return
		}

		// 4. 计算进度条与跨域 Worker 所需的响应头。CORS 由 Routers 的
		// 统一中间件仅向本地桌面页面开放，不能在这里放宽为任意来源。
		c.Header("Cross-Origin-Resource-Policy", "cross-origin")
		c.Header("Content-Length", strconv.FormatInt(fileInfo.Size(), 10)) // 必须返回总大小，否则前端无法渲染进度条
		c.Header("Content-Type", "application/octet-stream")
		c.Header("Content-Disposition", "attachment; filename="+fileInfo.Name())

		// 5. 限速策略：JSON 配置文件不限速，大模型文件（.onnx）平滑限速
		var maxBytesPerSecond int64 = 0 // 0 表示不限速
		if strings.HasSuffix(localPath, ".onnx") {
			// 设定单连接最大下载带宽（例如：2MB/s = 2 * 1024 * 1024 字节）
			// 你的海外服务器配置这个速度非常安全，既能保证前端感知，又不会把你的整体出口带宽撑满
			maxBytesPerSecond = 2 * 1024 * 1024
		}

		// 如果不限速（如 JSON 文件），直接快速返回
		if maxBytesPerSecond <= 0 {
			io.Copy(c.Writer, file)
			return
		}

		// 6. 分块限速传输逻辑
		chunkSize := int64(64 * 1024) // 每次读取 64KB
		buffer := make([]byte, chunkSize)
		// 计算发送一个 chunk 应该消耗的固定时间
		sleepDuration := time.Duration(chunkSize) * time.Second / time.Duration(maxBytesPerSecond)

		for {
			startTime := time.Now()
			n, readErr := file.Read(buffer)
			if n > 0 {
				_, writeErr := c.Writer.Write(buffer[:n])
				if writeErr != nil {
					return // 客户端中途取消下载，直接退出
				}
				c.Writer.Flush() // 强刷缓冲区发送给前端

				// 动态补齐等待时间实现平滑限速
				elapsed := time.Since(startTime)
				if elapsed < sleepDuration {
					time.Sleep(sleepDuration - elapsed)
				}
			}
			if readErr == io.EOF {
				break
			}
		}
	})
}

// modelCacheDirectory 返回模型镜像缓存目录。桌面入口会在启动时把该目录
// 统一配置到用户数据目录；Web 开发模式未配置时继续兼容仓库内相对路径。
func modelCacheDirectory() string {
	if configured := strings.TrimSpace(global.GVA_CONFIG.System.ModelCachePath); configured != "" {
		return configured
	}
	return "./model_cache"
}

func modelCachePath(hfPath string) (string, error) {
	relative := path.Clean("/" + strings.TrimPrefix(hfPath, "/"))
	if relative == "/" || strings.Contains(relative, "..") {
		return "", fmt.Errorf("invalid model path %q", hfPath)
	}
	return filepath.Join(modelCacheDirectory(), filepath.FromSlash(strings.TrimPrefix(relative, "/"))), nil
}

func isTrustedLocalOrigin(rawOrigin string) bool {
	origin, err := url.Parse(strings.TrimSpace(rawOrigin))
	if err != nil || origin.Scheme == "" || origin.Hostname() == "" {
		return false
	}
	host := strings.ToLower(origin.Hostname())
	if host == "wails.localhost" {
		return origin.Scheme == "http" || origin.Scheme == "https"
	}
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return origin.Scheme == "http" || origin.Scheme == "https"
	}
	return false
}

// frontendDistDirectory 同时支持开发目录和桌面发布包：发布版将 web/dist
// 放在可执行文件旁，开发模式继续读取仓库内的 web/dist。
func frontendDistDirectory() (string, bool) {
	candidates := []string{"./web/dist"}
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), "web", "dist"))
	}
	for _, dir := range candidates {
		if info, err := os.Stat(filepath.Join(dir, "index.html")); err == nil && !info.IsDir() {
			return dir, true
		}
	}
	return "", false
}

// downloadFromHF 负责从 Hugging Face 官方安全的把文件拉取到本地
// downloadFromHF 将白名单内的 Hugging Face 文件下载到指定本地缓存路径。
func downloadFromHF(hfPath, localPath string) error {
	// 创建本地所需的父级目录
	err := os.MkdirAll(filepath.Dir(localPath), 0755)
	if err != nil {
		return err
	}

	// 拼接官方下载链接
	hfURL := "https://huggingface.co" + hfPath

	// 发起请求（美国服务器直连 Hugging Face 速度极快，几秒钟即可下载完毕）
	resp, err := http.Get(hfURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("官方服务器返回异常状态码: %d", resp.StatusCode)
	}

	// 使用 .tmp 临时文件写入，避免因中途断网产生损坏的残缺模型文件
	tmpPath := localPath + ".tmp"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	_, err = io.Copy(tmpFile, resp.Body)
	tmpFile.Close() // 必须先关闭文件句柄才能重命名
	if err != nil {
		os.Remove(tmpPath) // 失败则清理临时文件
		return err
	}

	// 下载完成，原子性重命名为正式模型名字
	return os.Rename(tmpPath, localPath)
}
