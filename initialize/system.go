package initialize

import (
	"context"
	"fmt"

	"InkFlow/global"
	system "InkFlow/model/system"
	systemService "InkFlow/service/system"
	casbinUtils "InkFlow/utils/casbin"

	"gorm.io/gorm"
)

func initializeSystemModule(db *gorm.DB) {
	if err := migrateLegacySystemTables(db); err != nil {
		panic(fmt.Errorf("rename legacy system tables: %w", err))
	}
	if err := migrateLegacyMFAColumns(db); err != nil {
		panic(fmt.Errorf("migrate MFA columns: %w", err))
	}
	if err := db.AutoMigrate(
		&system.SysUser{}, &system.SysTenant{}, &system.SysOrganization{}, &system.SysRole{},
		&system.SysMembership{}, &system.SysMembershipApplication{}, &system.SysSession{}, &system.SysAuthChallenge{}, &system.SysAuditLog{}, &system.SysCasbinRule{}, &system.SysApi{}, &system.SysMenu{}, &system.SysModelSetting{}, &system.SysOAuthUser{},
	); err != nil {
		panic(fmt.Errorf("migrate system schema: %w", err))
	}
	if err := casbinUtils.InitializeCasbin(); err != nil {
		panic(fmt.Errorf("initialize casbin: %w", err))
	}
	if err := systemService.ServiceGroupApp.SysTenantService.BootstrapOwner(
		context.Background(),
		global.GVA_CONFIG.System.BootstrapOwnerUsername,
		global.GVA_CONFIG.System.BootstrapOwnerPassword,
		global.GVA_CONFIG.System.BootstrapOrganization,
	); err != nil {
		panic(fmt.Errorf("bootstrap initial owner: %w", err))
	}
	if err := casbinUtils.EnsureBuiltinPolicies(context.Background()); err != nil {
		panic(fmt.Errorf("repair built-in authorization policies: %w", err))
	}
}

// migrateLegacySystemTables renames the pre-SysApi system_* tables in place.
// Renaming preserves all deployment data, including users, memberships and
// Casbin policies; AutoMigrate alone would otherwise create empty sys_* tables.
func migrateLegacySystemTables(db *gorm.DB) error {
	tableNames := []struct {
		legacy  string
		current string
	}{
		{"system_users", "sys_users"},
		{"system_tenants", "sys_tenants"},
		{"system_organizations", "sys_organizations"},
		{"system_roles", "sys_roles"},
		{"system_memberships", "sys_memberships"},
		{"system_membership_applications", "sys_membership_applications"},
		{"system_sessions", "sys_sessions"},
		{"system_audit_logs", "sys_audit_logs"},
		{"system_casbin_rules", "sys_casbin_rules"},
	}
	for _, pair := range tableNames {
		hasLegacy := db.Migrator().HasTable(pair.legacy)
		hasCurrent := db.Migrator().HasTable(pair.current)
		if hasLegacy && hasCurrent {
			return fmt.Errorf("both %s and %s exist; merge them explicitly before startup", pair.legacy, pair.current)
		}
		if hasLegacy {
			if err := db.Migrator().RenameTable(pair.legacy, pair.current); err != nil {
				return fmt.Errorf("%s to %s: %w", pair.legacy, pair.current, err)
			}
		}
	}
	return nil
}

// migrateLegacyMFAColumns preserves authentication data when MFA fields are
// renamed. AutoMigrate creates new columns but does not copy or rename existing
// data, so this runs first and keeps already-bound TOTP secrets and login-step
// state intact.
func migrateLegacyMFAColumns(db *gorm.DB) error {
	if db == nil {
		return gorm.ErrInvalidDB
	}
	if !db.Migrator().HasTable(&system.SysUser{}) {
		return nil
	}

	columnNames := []struct{ legacy, current string }{
		{legacy: "mfa_secret_encrypted", current: "mfa_totp_secret_ciphertext"},
		{legacy: "mfa_required", current: "mfa_enrollment_required"},
		{legacy: "mfa_pending_token_hash", current: "mfa_login_step_token_hash"},
		{legacy: "mfa_pending_stage", current: "mfa_login_step_kind"},
		{legacy: "mfa_pending_expires_at", current: "mfa_login_step_expires_at"},
	}
	for _, column := range columnNames {
		hasLegacy := db.Migrator().HasColumn(&system.SysUser{}, column.legacy)
		hasCurrent := db.Migrator().HasColumn(&system.SysUser{}, column.current)
		if hasLegacy && hasCurrent {
			return fmt.Errorf("both sys_users.%s and sys_users.%s exist; merge MFA data explicitly before startup", column.legacy, column.current)
		}
		if hasLegacy {
			if err := db.Migrator().RenameColumn(&system.SysUser{}, column.legacy, column.current); err != nil {
				return fmt.Errorf("rename sys_users.%s to %s: %w", column.legacy, column.current, err)
			}
		}
	}
	return nil
}
