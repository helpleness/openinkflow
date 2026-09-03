package v1

import (
	officialdoc "InkFlow/api/v1/officialdoc"
	system "InkFlow/api/v1/system"
)

// ApiGroup 按领域聚合 API，实现与 GVA 一致的统一装配入口。
// 具体业务 API 只依赖自己的领域分组，避免跨领域共享泛化的处理器。
type ApiGroup struct {
	SystemApiGroup      system.ApiGroup
	OfficialDocApiGroup officialdoc.ApiGroup
}

// ApiGroupApp 供路由层和后续领域模块统一访问。
var ApiGroupApp = new(ApiGroup)
