package router

import (
	officialdoc "InkFlow/router/officialdoc"
	system "InkFlow/router/system"
)

// RouterGroup 按领域聚合路由，实现与 GVA 一致的统一装配入口。
// 初始化器只负责注入公共中间件；具体路径绑定由对应领域负责。
type RouterGroup struct {
	SystemRouterGroup      system.RouterGroup
	OfficialDocRouterGroup officialdoc.RouterGroup
}

// RouterGroupApp 供 initialize/router.go 统一注册全部领域路由。
var RouterGroupApp = new(RouterGroup)
