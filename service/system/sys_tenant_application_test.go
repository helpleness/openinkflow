package system

import (
	"context"
	"testing"

	"InkFlow/global"
	model "InkFlow/model/system"
	casbinUtils "InkFlow/utils/casbin"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestWorkspaceApplicationGrantsConfiguredRole(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:tenant-application-test?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.SysUser{}, &model.SysTenant{}, &model.SysOrganization{}, &model.SysRole{}, &model.SysMembership{}, &model.SysTenantApplication{}, &model.SysSession{}, &model.SysCasbinRule{}); err != nil {
		t.Fatal(err)
	}
	previousDB := global.GVA_DB
	global.GVA_DB = db
	t.Cleanup(func() { global.GVA_DB = previousDB })
	if err := casbinUtils.InitializeCasbin(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := ServiceGroupApp.SysTenantService.BootstrapOwner(ctx, "workspace-owner", "correct-horse-battery", "测试工作空间"); err != nil {
		t.Fatal(err)
	}
	owner, err := ServiceGroupApp.SysAuthService.LoginLocal(ctx, "workspace-owner", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	tenants, err := ServiceGroupApp.SysTenantService.ListTenants(ctx, owner.User.ID)
	if err != nil || len(tenants) != 1 {
		t.Fatalf("owner tenants = %#v, %v", tenants, err)
	}
	reader, err := ServiceGroupApp.SysRoleService.CreateRole(ctx, tenants[0].ID, "默认成员", "workspace-reader", "申请批准后的默认角色", []string{"workspace"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ServiceGroupApp.SysTenantService.UpdateTenantSettings(ctx, tenants[0].ID, owner.User.ID, reader.ID, true); err != nil {
		t.Fatal(err)
	}
	applicant, err := ServiceGroupApp.SysAuthService.RegisterLocal(ctx, "workspace-applicant", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	if items, err := ServiceGroupApp.SysTenantService.ListTenants(ctx, applicant.User.ID); err != nil || len(items) != 0 {
		t.Fatalf("new account tenants = %#v, %v", items, err)
	}
	if items, err := ServiceGroupApp.SysTenantApplicationService.ListPublicTenants(ctx, applicant.User.ID); err != nil || len(items) != 1 || items[0].ID != tenants[0].ID {
		t.Fatalf("public workspaces = %#v, %v", items, err)
	}
	application, err := ServiceGroupApp.SysTenantApplicationService.ApplyToTenant(ctx, tenants[0].ID, applicant.User.ID)
	if err != nil || application.Status != model.ApplicationPending {
		t.Fatalf("apply workspace = %#v, %v", application, err)
	}
	if err := ServiceGroupApp.SysTenantApplicationService.ReviewTenantApplication(ctx, tenants[0].ID, application.ID, owner.User.ID, true); err != nil {
		t.Fatal(err)
	}
	items, err := ServiceGroupApp.SysTenantService.ListTenants(ctx, applicant.User.ID)
	if err != nil || len(items) != 1 || items[0].ID != tenants[0].ID {
		t.Fatalf("approved applicant tenants = %#v, %v", items, err)
	}
	if items, err := ServiceGroupApp.SysTenantApplicationService.ListPublicTenants(ctx, applicant.User.ID); err != nil || len(items) != 0 {
		t.Fatalf("joined workspace should be absent from application directory: %#v, %v", items, err)
	}
	access, err := ServiceGroupApp.SysRoleService.AccessForUser(ctx, tenants[0].ID, applicant.User.ID)
	if err != nil || access.RoleID != reader.ID || access.RoleCode != reader.Code {
		t.Fatalf("approved applicant access = %#v, %v", access, err)
	}
	otherOwner, err := ServiceGroupApp.SysAuthService.RegisterLocal(ctx, "other-workspace-owner", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ServiceGroupApp.SysTenantService.CreateTenant(ctx, otherOwner.User.ID, "其他工作空间", "other-workspace"); err != nil {
		t.Fatal(err)
	}
	unassigned, err := ServiceGroupApp.SysAuthService.RegisterLocal(ctx, "unassigned-directory-user", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	directory, err := ServiceGroupApp.SysUserService.ListGlobalUsers(ctx, tenants[0].ID, owner.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, item := range directory {
		seen[item.Username] = true
	}
	if !seen[unassigned.User.Username] || !seen[applicant.User.Username] || seen[otherOwner.User.Username] {
		t.Fatalf("workspace directory visibility = %#v", seen)
	}
}
