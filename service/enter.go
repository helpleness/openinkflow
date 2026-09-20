package service

import (
	officialdoc "InkFlow/service/officialdoc"
	system "InkFlow/service/system"
)

// ServiceGroup 按领域聚合服务，实现与 GVA 一致的统一装配入口。
// 领域内部继续通过各自的 ServiceGroupApp 访问，避免业务逻辑相互耦合。
type ServiceGroup struct {
	SystemServiceGroup      system.ServiceGroup
	OfficialDocServiceGroup officialdoc.ServiceGroup
}

// ServiceGroupApp 供 API、路由和初始化流程统一访问。
var ServiceGroupApp = new(ServiceGroup)
