package system

import (
	"InkFlow/global"
	casbinUtils "InkFlow/utils/casbin"
	"context"
	"errors"
	"fmt"
	"strings"

	model "InkFlow/model/system"
	request "InkFlow/model/system/request"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SysApiService owns system API-resource operations.
type SysApiService struct{}

// SyncSysApis synchronizes server routes into the role-configurable SysApi registry.
// 该方法在路由构建时运行，确保新增接口能够在界面中授权。
func (s *SysApiService) SyncSysApis(ctx context.Context, routes []gin.RouteInfo) error {
	db := global.GVA_DB
	resources := make([]model.SysApi, 0, len(routes))
	for _, route := range routes {
		if !isSysAPIPath(route.Path) {
			continue
		}
		group := apiMetadata(route.Path)
		resources = append(resources, model.SysApi{
			APIGroup: group, Name: fmt.Sprintf("%s %s", route.Method, route.Path), Path: route.Path,
			Method:      route.Method,
			Description: apiDescription(route.Method, route.Path),
		})
	}
	for _, resource := range resources {
		var persisted model.SysApi
		// A route is identified by method + path. API groups are display metadata
		// and can change when a feature is moved in the navigation; including the
		// group here would create duplicate permission rows for the same route.
		err := db.WithContext(ctx).
			Where("path = ? AND method = ?", resource.Path, resource.Method).
			First(&persisted).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := db.WithContext(ctx).Create(&resource).Error; err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		// Map updates deliberately include false and empty strings. Struct-based
		// Updates skips zero values, which previously left obsolete is_public
		// flags in the database and made APIs impossible to configure.
		if err := db.WithContext(ctx).Model(&persisted).Updates(map[string]any{
			"api_group":   resource.APIGroup,
			"name":        resource.Name,
			"path":        resource.Path,
			"method":      resource.Method,
			"description": resource.Description,
			"menu_key":    "",
			"menu_name":   "",
			"is_public":   resource.IsPublic,
		}).Error; err != nil {
			return err
		}
	}
	return s.grantOwnerAPIs(ctx)
}

// isSysAPIPath limits the permission catalog to application APIs. Static files,
// health checks and model downloads are intentionally not configurable roles.
func isSysAPIPath(path string) bool {
	return strings.HasPrefix(path, "/system/") || strings.HasPrefix(path, "/auth/") || strings.HasPrefix(path, "/officialdoc/")
}

// ListSysApis returns the role-configurable API registry with normalized filters.
func (s *SysApiService) ListSysApis(ctx context.Context, search request.SysApiSearch) ([]model.SysApi, error) {
	db := global.GVA_DB
	query := db.WithContext(ctx).Model(&model.SysApi{})
	if search.APIGroup != "" {
		query = query.Where("api_group = ?", search.APIGroup)
	}
	if search.Path != "" {
		query = query.Where("path LIKE ?", "%"+search.Path+"%")
	}
	if search.Method != "" {
		query = query.Where("method = ?", strings.ToUpper(search.Method))
	}
	if search.IsPublic != nil {
		query = query.Where("is_public = ?", *search.IsPublic)
	}
	if keyword := strings.TrimSpace(search.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("name LIKE ? OR description LIKE ? OR path LIKE ?", like, like, like)
	}
	var resources []model.SysApi
	err := query.Order("api_group, sort, path, method").Find(&resources).Error
	return resources, err
}

// grantOwnerAPIs 为所有租户所有者角色授予全部已登记的 API 权限。
func (s *SysApiService) grantOwnerAPIs(ctx context.Context) error {
	db := global.GVA_DB
	var owners []model.SysRole
	if err := db.WithContext(ctx).Where("code = ?", model.RoleOwner).Find(&owners).Error; err != nil {
		return err
	}
	resources, err := s.ListSysApis(ctx, request.SysApiSearch{})
	if err != nil {
		return err
	}
	return casbinUtils.ReplaceOwnerAPIs(owners, resources)
}

// apiMetadata only provides an API catalog group. Menu keys and menu names are
// owned by the frontend and are never inferred from backend routes.
func apiMetadata(path string) string {
	switch {
	case isPersonalCenterAPI(path):
		return "个人中心"
	case strings.Contains(path, "/membership-applications/"):
		return "申请审核"
	case strings.Contains(path, "/public-organizations"):
		return "组织申请"
	case strings.Contains(path, "/organizations"):
		return "组织管理"
	case strings.Contains(path, "/memberships") || strings.Contains(path, "/membership-applications") || strings.Contains(path, "/system/users"):
		return "成员授权"
	case strings.Contains(path, "/roles") || strings.Contains(path, "/apis"):
		return "角色与权限"
	case strings.Contains(path, "/menu-configs"):
		return "菜单配置"
	case strings.Contains(path, "/model-settings") || strings.Contains(path, "/inference/"):
		return "模型配置"
	case strings.Contains(path, "/audit"):
		return "审计日志"
	case strings.Contains(path, "/officialdoc/knowledge-documents") || strings.Contains(path, "/officialdoc/knowledge-search"):
		return "知识库"
	case strings.Contains(path, "/officialdoc/document-templates"):
		return "写作模板"
	case strings.Contains(path, "/officialdoc/writing-runs") || (strings.Contains(path, "/officialdoc/writing-tasks/") && strings.HasSuffix(path, "/runs")):
		return "MCP 写作运行"
	case strings.Contains(path, "/officialdoc/writing-tasks"):
		return "写作任务"
	default:
		return "系统"
	}
}

// isPersonalCenterAPI marks only the authenticated, tenant-scoped account
// management APIs. Login, captcha, pending-MFA and session bootstrap routes
// must remain outside role authorization so a user can complete sign-in.
func isPersonalCenterAPI(path string) bool {
	switch path {
	case "/auth/mfa/setup", "/auth/mfa/enable", "/auth/mfa/disable",
		"/auth/sessions", "/auth/sessions/:session_id", "/auth/sessions/revoke-others":
		return true
	default:
		return false
	}
}

// apiDescription 返回接口对应的中文权限用途说明。
func apiDescription(method, path string) string {
	key := method + " " + path
	purposes := map[string]string{
		"POST /auth/mfa/pending/setup":                                   "账号密码和图片验证码通过后，为尚未绑定 MFA 的账号生成一次性绑定密钥；不创建登录会话。",
		"POST /auth/mfa/pending/complete":                                "在独立 MFA 步骤验证动态验证码或完成首次绑定后，创建安全会话。",
		"POST /auth/mfa/setup":                                           "为当前登录成员生成一次 MFA 绑定密钥；需具备个人中心权限。",
		"POST /auth/mfa/enable":                                          "使用动态验证码启用当前成员的 MFA；需具备个人中心权限。",
		"POST /auth/mfa/disable":                                         "校验当前密码和动态验证码后关闭当前成员 MFA，并撤销全部会话；需具备个人中心权限。",
		"GET /auth/sessions":                                             "读取当前成员自己的已登录设备会话；需具备个人中心权限。",
		"DELETE /auth/sessions/:session_id":                              "撤销当前成员自己的指定设备会话；需具备个人中心权限。",
		"POST /auth/sessions/revoke-others":                              "撤销当前成员除本机以外的全部设备会话；需具备个人中心权限。",
		"GET /system/public-organizations":                               "读取可自行申请加入的公开组织；不包含隐藏组织。由角色 API 权限控制访问。",
		"GET /system/membership-applications":                            "读取组织加入申请；由角色 API 权限控制，普通成员仅能读取自己的申请。",
		"POST /system/membership-applications":                           "向公开组织提交加入申请，等待管理员或所有者审批。由角色 API 权限控制。",
		"PUT /system/membership-applications/:id":                        "管理员或所有者审核组织加入申请；批准时仅分配组织，不改变申请人角色。",
		"GET /system/organizations":                                      "管理端读取当前租户全部组织，包括隐藏组织。",
		"POST /system/organizations":                                     "创建组织。",
		"PUT /system/organizations/:id/visibility":                       "设置组织是否公开可申请。",
		"GET /system/memberships":                                        "读取当前租户成员、所属组织和角色。",
		"POST /system/memberships":                                       "直接授予或更新用户的组织和角色。",
		"GET /system/users":                                              "仅所有者读取全局已注册用户，用于成员分配。",
		"GET /system/roles":                                              "读取当前租户角色目录。",
		"POST /system/roles":                                             "创建自定义角色。",
		"PUT /system/roles/:id/permissions":                              "更新角色的菜单和 API 权限。",
		"GET /system/apis":                                               "读取可配置的 API 权限目录。",
		"GET /system/audit-logs":                                         "读取当前租户审计日志。",
		"GET /system/menus":                                              "读取当前登录用户的可见菜单和所属组织。",
		"GET /system/menu-configs":                                       "读取前端菜单目录，用于按角色渲染导航与权限勾选项。",
		"POST /system/menu-configs/sync":                                 "由前端写入缺失的默认菜单定义；不会覆盖管理员已配置的菜单。",
		"POST /system/menu-configs":                                      "创建前端导航菜单配置。菜单键会用于角色菜单授权。",
		"PUT /system/menu-configs/:id":                                   "更新前端菜单的名称、父级、视图键、排序与启用状态。",
		"GET /system/tenants":                                            "读取当前用户可访问的租户。",
		"GET /system/model-settings":                                     "读取当前用户在当前租户中的模型连接配置；密钥只返回是否已保存。",
		"PUT /system/model-settings":                                     "保存当前用户在当前租户中的主模型和 OCR 语义总结模型配置；密钥优先写入系统凭据管理器，服务端无此能力时加密保存。",
		"GET /system/inference/ws":                                       "建立浏览器本地推理 worker 通道，用于 Embedding 与 Rerank 任务。",
		"POST /officialdoc/knowledge-documents/import":                   "导入组织知识文档，解析为 Markdown、保存附件并建立知识切片与索引。",
		"GET /officialdoc/knowledge-documents":                           "读取当前组织可访问的知识文档目录和索引状态。",
		"GET /officialdoc/knowledge-documents/:id":                       "读取知识文档的切片详情，用于人工检查和证据回溯。",
		"POST /officialdoc/knowledge-documents/:id/reindex":              "重新建立指定知识文档的向量和词法索引。",
		"DELETE /officialdoc/knowledge-documents/:id":                    "人工删除知识文档、其切片及对应检索索引；该操作不会作为 MCP 工具暴露。",
		"POST /officialdoc/knowledge-search":                             "在当前组织执行词法与向量混合检索，返回可引用的知识证据。",
		"GET /officialdoc/document-templates":                            "读取当前组织的受控写作模板目录。",
		"POST /officialdoc/document-templates":                           "创建当前组织的 Markdown 写作模板、变量和约束。",
		"PUT /officialdoc/document-templates/:id":                        "更新当前组织的受控写作模板。",
		"GET /officialdoc/writing-tasks":                                 "读取当前组织的写作任务目录。",
		"GET /officialdoc/writing-tasks/:id":                             "读取写作任务、版本历史及每个版本的证据快照。",
		"POST /officialdoc/writing-tasks":                                "使用组织模板创建受控写作任务。",
		"GET /officialdoc/writing-tasks/:id/runs":                        "读取任务的 MCP 写作运行历史、检查点与状态。",
		"POST /officialdoc/writing-tasks/:id/runs":                       "启动一次 MCP 受控写作运行：检索证据、生成文稿并固化版本。运行可暂停和恢复。",
		"GET /officialdoc/writing-runs/:id":                              "读取一条 MCP 写作运行的多轮消息、工具轨迹和冻结证据。",
		"GET /officialdoc/writing-runs/:id/events":                       "以 SSE 持续推送一条 MCP 写作运行的状态、多轮消息、工具轨迹和检查点。",
		"POST /officialdoc/writing-runs/:id/pause":                       "请求暂停正在执行的 MCP 写作运行；已完成步骤会保留为检查点。",
		"POST /officialdoc/writing-runs/:id/resume":                      "恢复暂停、失败或服务重启后中断的 MCP 写作运行，从未完成步骤继续。",
		"POST /officialdoc/writing-tasks/:id/versions":                   "保存人工修订为写作任务的新版本，不覆盖历史版本。",
		"GET /officialdoc/writing-tasks/:id/versions/:version_id/export": "按不可变版本生成可下载的正式 DOCX 或 PDF；PDF 使用服务器配置的 LibreOffice 转换器。",
	}
	if description, ok := purposes[key]; ok {
		return description
	}
	return "系统接口。请结合路径、方法和所属功能确认授权范围。"
}
