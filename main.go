//go:build !desktop

package main

import (
	"InkFlow/core"
	"InkFlow/global"
	"InkFlow/initialize"
	"fmt"
)

// main 保留为本地 Web 调试入口。桌面发布请使用 -tags desktop，
// 两个入口共用相同的数据库、向量库和推理服务初始化流程。
func main() {
	global.GVA_VP = core.InitializeViper()
	defer initialize.InitializeServices()()

	router := initialize.Routers()
	addr, err := core.LoopbackAddress(global.GVA_CONFIG.System.Addr)
	if err != nil {
		panic(err)
	}

	fmt.Printf("InkFlow local service: http://%s\n", addr)
	if err := router.Run(addr); err != nil {
		panic(err)
	}
}
