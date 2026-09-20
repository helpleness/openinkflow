package system

import (
	"context"
	"errors"

	"InkFlow/global"
	model "InkFlow/model/system"
	strutil "InkFlow/utils"

	"gorm.io/gorm"
)

// SysMenuService owns frontend menu configuration records.
type SysMenuService struct{}

func (s *SysMenuService) ListSysMenus(ctx context.Context) ([]model.SysMenu, error) {
	db := global.GVA_DB
	var menus []model.SysMenu
	err := db.WithContext(ctx).Order("sort, id").Find(&menus).Error
	return menus, err
}

// SyncSysMenus inserts frontend-declared defaults without changing configured
// records. Frontend ownership prevents backend route code from inventing menus.
func (s *SysMenuService) SyncSysMenus(ctx context.Context, menus []model.SysMenu) error {
	db := global.GVA_DB
	for _, menu := range menus {
		item, err := normalizeSysMenu(menu)
		if err != nil {
			return err
		}
		var existing model.SysMenu
		err = db.WithContext(ctx).Where("menu_key = ?", item.MenuKey).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := db.WithContext(ctx).Create(&item).Error; err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *SysMenuService) CreateSysMenu(ctx context.Context, menu model.SysMenu) (*model.SysMenu, error) {
	db := global.GVA_DB
	item, err := normalizeSysMenu(menu)
	if err != nil {
		return nil, err
	}
	if err := db.WithContext(ctx).Create(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *SysMenuService) UpdateSysMenu(ctx context.Context, menuID uint, menu model.SysMenu) (*model.SysMenu, error) {
	db := global.GVA_DB
	item, err := normalizeSysMenu(menu)
	if err != nil {
		return nil, err
	}
	var existing model.SysMenu
	if err := db.WithContext(ctx).First(&existing, menuID).Error; err != nil {
		return nil, err
	}
	if err := db.WithContext(ctx).Model(&existing).Updates(map[string]any{
		"name": item.Name, "menu_key": item.MenuKey, "parent_key": item.ParentKey,
		"view_key": item.ViewKey, "description": item.Description, "sort": item.Sort, "is_enabled": item.IsEnabled,
	}).Error; err != nil {
		return nil, err
	}
	return &existing, nil
}

func normalizeSysMenu(menu model.SysMenu) (model.SysMenu, error) {
	menu.Name = strutil.NormalizeText(menu.Name)
	menu.MenuKey = strutil.NormalizeText(menu.MenuKey)
	menu.ParentKey = strutil.NormalizeText(menu.ParentKey)
	menu.ViewKey = strutil.NormalizeText(menu.ViewKey)
	menu.Description = strutil.NormalizeText(menu.Description)
	if menu.Name == "" || menu.MenuKey == "" {
		return model.SysMenu{}, errors.New("菜单名称和菜单键不能为空")
	}
	if menu.MenuKey == menu.ParentKey {
		return model.SysMenu{}, errors.New("菜单不能将自身设为父级")
	}
	return menu, nil
}
