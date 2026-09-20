package system

// SysCasbinRule 是 system 领域的 Casbin 策略持久化表，不使用软删除。
type SysCasbinRule struct {
	ID    uint   `gorm:"primaryKey"`
	Ptype string `gorm:"size:32;not null;uniqueIndex:idx_system_casbin_rule,priority:1"`
	V0    string `gorm:"size:255;not null;default:'';uniqueIndex:idx_system_casbin_rule,priority:2"`
	V1    string `gorm:"size:255;not null;default:'';uniqueIndex:idx_system_casbin_rule,priority:3"`
	V2    string `gorm:"size:255;not null;default:'';uniqueIndex:idx_system_casbin_rule,priority:4"`
	V3    string `gorm:"size:255;not null;default:'';uniqueIndex:idx_system_casbin_rule,priority:5"`
	V4    string `gorm:"size:255;not null;default:'';uniqueIndex:idx_system_casbin_rule,priority:6"`
	V5    string `gorm:"size:255;not null;default:'';uniqueIndex:idx_system_casbin_rule,priority:7"`
}

// TableName 返回 Casbin 策略模型对应的数据表名。
func (SysCasbinRule) TableName() string { return "sys_casbin_rules" }
