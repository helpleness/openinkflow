package system

import (
	"context"
	"testing"

	"InkFlow/global"
	model "InkFlow/model/system"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestUpdateSysMenuCanDisableExistingMenu(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:sys-menu-disable?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.SysMenu{}); err != nil {
		t.Fatal(err)
	}
	previousDB := global.GVA_DB
	global.GVA_DB = db
	t.Cleanup(func() { global.GVA_DB = previousDB })

	service := SysMenuService{}
	created, err := service.CreateSysMenu(context.Background(), model.SysMenu{
		Name: "菜单配置", MenuKey: "menu_configs", ParentKey: "permission_management", ViewKey: "menu_configs", IsEnabled: true,
	})
	if err != nil {
		t.Fatalf("CreateSysMenu() error = %v", err)
	}
	updated, err := service.UpdateSysMenu(context.Background(), created.ID, model.SysMenu{
		Name: "菜单配置", MenuKey: "menu_configs", ParentKey: "permission_management", ViewKey: "menu_configs", IsEnabled: false,
	})
	if err != nil {
		t.Fatalf("UpdateSysMenu() error = %v", err)
	}
	if updated.IsEnabled {
		t.Fatal("UpdateSysMenu() left the menu enabled")
	}

	var persisted model.SysMenu
	if err := db.First(&persisted, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.IsEnabled {
		t.Fatal("disabled menu was not persisted")
	}
}
