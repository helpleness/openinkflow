package system

import (
	"context"
	"errors"
	"testing"

	"InkFlow/global"
	commonResponse "InkFlow/model/common/response"
	model "InkFlow/model/system"
	request "InkFlow/model/system/request"
	response "InkFlow/model/system/response"
	casbinUtils "InkFlow/utils/casbin"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// TestLocalRegistrationCreatesTenantSessionAndPolicy 验证本地注册后的租户、会话和权限初始化流程。
func TestLocalRegistrationCreatesTenantSessionAndPolicy(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:system-service-test?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.SysUser{}, &model.SysTenant{}, &model.SysOrganization{}, &model.SysRole{}, &model.SysMembership{}, &model.SysMembershipApplication{}, &model.SysSession{}, &model.SysAuditLog{}, &model.SysCasbinRule{}, &model.SysApi{}); err != nil {
		t.Fatal(err)
	}
	previousDB := global.GVA_DB
	global.GVA_DB = db
	t.Cleanup(func() { global.GVA_DB = previousDB })
	if err := casbinUtils.InitializeCasbin(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := ServiceGroupApp.SysTenantService.BootstrapOwner(ctx, "system-owner", "correct-horse-battery", "测试单位"); err != nil {
		t.Fatalf("BootstrapOwner() error = %v", err)
	}
	if err := casbinUtils.EnsureBuiltinPolicies(ctx); err != nil {
		t.Fatalf("EnsureBuiltinPolicies() error = %v", err)
	}
	result, err := ServiceGroupApp.SysAuthService.LoginLocal(ctx, "system-owner", "correct-horse-battery")
	if err != nil {
		t.Fatalf("RegisterLocal() error = %v", err)
	}
	if result.SessionToken == "" || result.User.ID == 0 {
		t.Fatalf("unexpected auth result: %#v", result)
	}
	tenants, err := ServiceGroupApp.SysTenantService.ListTenants(ctx, result.User.ID)
	if err != nil || len(tenants) != 1 {
		t.Fatalf("ListTenants() = %#v, %v", tenants, err)
	}
	memberships, err := ServiceGroupApp.SysMembershipService.ListMemberships(ctx, tenants[0].ID)
	if err != nil || len(memberships) != 1 || memberships[0].Username != result.User.Username {
		t.Fatalf("ListMemberships() = %#v, %v", memberships, err)
	}
	allowed, err := casbinUtils.Enforce(result.User.ID, tenants[0].ID, "/system/organizations", "POST")
	if err != nil || !allowed {
		t.Fatalf("owner policy = %v, %v", allowed, err)
	}
	if err := ServiceGroupApp.SysAuditService.RecordAudit(ctx, AuditEntry{TenantID: tenants[0].ID, UserID: result.User.ID, Action: "test", Resource: "system", Result: "success"}); err != nil {
		t.Fatal(err)
	}
	logs, err := ServiceGroupApp.SysAuditService.ListAuditLogs(ctx, tenants[0].ID, 10)
	if err != nil || len(logs) != 1 {
		t.Fatalf("ListAuditLogs() = %#v, %v", logs, err)
	}

	member, err := ServiceGroupApp.SysAuthService.RegisterLocal(ctx, "document-member", "correct-horse-battery")
	if err != nil {
		t.Fatalf("RegisterLocal(member) error = %v", err)
	}
	adminRole, err := ServiceGroupApp.SysRoleService.CreateRole(ctx, tenants[0].ID, "管理员", model.RoleAdmin, "前端手动创建的管理员角色", []string{"workspace"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	roles, err := ServiceGroupApp.SysRoleService.ListRoles(ctx, tenants[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.GET("/system/example", func(c *gin.Context) {})
	if err := ServiceGroupApp.SysApiService.SyncSysApis(ctx, router.Routes()); err != nil {
		t.Fatalf("SyncSysApis() error = %v", err)
	}
	resources, err := ServiceGroupApp.SysApiService.ListSysApis(ctx, request.SysApiSearch{})
	if err != nil || len(resources) != 1 || resources[0].APIGroup != "系统" {
		t.Fatalf("ListSysApis() = %#v, %v", resources, err)
	}
	filteredResources, err := ServiceGroupApp.SysApiService.ListSysApis(ctx, request.SysApiSearch{Keyword: "example", Method: "get"})
	if err != nil || len(filteredResources) != 1 || filteredResources[0].Path != "/system/example" {
		t.Fatalf("ListSysApis(search) = %#v, %v", filteredResources, err)
	}
	group := apiMetadata("/system/membership-applications")
	if group != "成员授权" {
		t.Fatalf("membership application API group = %q", group)
	}
	group = apiMetadata("/system/users")
	if group != "成员授权" {
		t.Fatalf("global users API group = %q", group)
	}
	customRole, err := ServiceGroupApp.SysRoleService.CreateRole(ctx, tenants[0].ID, "示例角色", "example-role", "测试自定义权限", []string{"workspace"}, []uint{resources[0].ID})
	if err != nil {
		t.Fatalf("CreateRole() error = %v", err)
	}
	organizations, err := ServiceGroupApp.SysOrganizationService.ListOrganizations(ctx, tenants[0].ID, result.User.ID)
	if err != nil || len(organizations) == 0 {
		t.Fatalf("ListOrganizations() = %#v, %v", organizations, err)
	}
	if _, err := ServiceGroupApp.SysMembershipService.AddMembershipByUsername(ctx, tenants[0].ID, organizations[0].ID, result.User.Username, adminRole.ID); err == nil {
		t.Fatal("downgrading the last owner unexpectedly succeeded")
	}
	var ownerRole model.SysRole
	for _, role := range roles {
		if role.Code == model.RoleOwner {
			ownerRole = role
			break
		}
	}
	if _, err := ServiceGroupApp.SysMembershipService.AddMembershipByUsername(ctx, tenants[0].ID, organizations[0].ID, member.User.Username, ownerRole.ID); err != nil {
		t.Fatalf("assign second owner: %v", err)
	}
	// Reassigning an existing owner now overwrites the same membership row and
	// returns the freshly persisted role instead of the stale pre-update row.
	reassignedMembership, err := ServiceGroupApp.SysMembershipService.AddMembershipByUsername(ctx, tenants[0].ID, organizations[0].ID, result.User.Username, adminRole.ID)
	if err != nil {
		t.Fatalf("downgrade owner membership: %v", err)
	}
	if reassignedMembership.RoleID != adminRole.ID {
		t.Fatalf("downgrade response role ID = %d, want %d", reassignedMembership.RoleID, adminRole.ID)
	}
	memberships, err = ServiceGroupApp.SysMembershipService.ListMemberships(ctx, tenants[0].ID)
	var reassigned response.SysMembershipSummary
	for _, membership := range memberships {
		if membership.Username == result.User.Username {
			reassigned = membership
			break
		}
	}
	if err != nil || len(memberships) != 2 || reassigned.RoleCode != model.RoleAdmin {
		t.Fatalf("owner membership after update = %#v, %v", memberships, err)
	}
	// Restore the owner for the remaining owner-only assertions in this test.
	if _, err := ServiceGroupApp.SysMembershipService.AddMembershipByUsername(ctx, tenants[0].ID, organizations[0].ID, result.User.Username, ownerRole.ID); err != nil {
		t.Fatalf("restore owner membership: %v", err)
	}
	if _, err := ServiceGroupApp.SysMembershipService.AddMembershipByUsername(ctx, tenants[0].ID, organizations[0].ID, member.User.Username, customRole.ID); err != nil {
		t.Fatalf("AddMembershipByUsername() error = %v", err)
	}
	allowed, err = casbinUtils.Enforce(member.User.ID, tenants[0].ID, "/system/example", "GET")
	if err != nil || !allowed {
		t.Fatalf("custom role policy = %v, %v", allowed, err)
	}
	memberships, err = ServiceGroupApp.SysMembershipService.ListMemberships(ctx, tenants[0].ID)
	if err != nil || len(memberships) != 2 {
		t.Fatalf("ListMemberships() after grant = %#v, %v", memberships, err)
	}
	users, err := ServiceGroupApp.SysUserService.ListGlobalUsers(ctx, tenants[0].ID, result.User.ID)
	if err != nil || len(users) != 2 {
		t.Fatalf("ListGlobalUsers() = %#v, %v", users, err)
	}
	if _, err := ServiceGroupApp.SysUserService.ListGlobalUsers(ctx, tenants[0].ID, member.User.ID); !errors.Is(err, commonResponse.ErrForbidden) {
		t.Fatalf("non-owner ListGlobalUsers() error = %v", err)
	}
	if _, err := ServiceGroupApp.SysMembershipApplicationService.ApplyToOrganization(ctx, tenants[0].ID, organizations[0].ID, member.User.ID); err != nil {
		t.Fatalf("ApplyToOrganization() error = %v", err)
	}
	applications, err := ServiceGroupApp.SysMembershipApplicationService.ListMembershipApplications(ctx, tenants[0].ID, result.User.ID)
	if err != nil || len(applications) != 1 || applications[0].Username != member.User.Username {
		t.Fatalf("ListMembershipApplications() = %#v, %v", applications, err)
	}
	if err := ServiceGroupApp.SysMembershipApplicationService.ReviewMembershipApplication(ctx, tenants[0].ID, applications[0].ID, result.User.ID, true); err != nil {
		t.Fatalf("ReviewMembershipApplication() error = %v", err)
	}
	memberships, err = ServiceGroupApp.SysMembershipService.ListMemberships(ctx, tenants[0].ID)
	if err != nil {
		t.Fatalf("ListMemberships() after application approval = %v", err)
	}
	for _, membership := range memberships {
		if membership.Username == member.User.Username {
			if membership.RoleID != customRole.ID || membership.OrganizationID != organizations[0].ID {
				t.Fatalf("approval changed membership unexpectedly: %#v", membership)
			}
			return
		}
	}
	t.Fatal("approved applicant is missing from membership directory")
}
