package casbin

import (
	"fmt"

	model "InkFlow/model/system"

	casbinmodel "github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type casbinAdapter struct{ db *gorm.DB }

// LoadPolicy 从数据库加载全部 Casbin 策略到权限模型。
func (a casbinAdapter) LoadPolicy(casbinModel casbinmodel.Model) error {
	var rules []model.SysCasbinRule
	if err := a.db.Order("id").Find(&rules).Error; err != nil {
		return err
	}
	for _, rule := range rules {
		values := trimRule([]string{rule.Ptype, rule.V0, rule.V1, rule.V2, rule.V3, rule.V4, rule.V5})
		if err := persist.LoadPolicyArray(values, casbinModel); err != nil {
			return err
		}
	}
	return nil
}

// SavePolicy 使用权限模型中的完整策略覆盖保存数据库策略。
func (a casbinAdapter) SavePolicy(casbinModel casbinmodel.Model) error {
	return a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.SysCasbinRule{}).Error; err != nil {
			return err
		}
		for section, assertions := range casbinModel {
			if section != "p" && section != "g" {
				continue
			}
			for ptype, assertion := range assertions {
				for _, policy := range assertion.Policy {
					if err := tx.Create(ruleFromPolicy(ptype, policy)).Error; err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
}

// AddPolicy 将单条 Casbin 策略写入数据库，重复策略会被忽略。
func (a casbinAdapter) AddPolicy(_ string, ptype string, rule []string) error {
	return a.db.Clauses(clause.OnConflict{DoNothing: true}).Create(ruleFromPolicy(ptype, rule)).Error
}

// RemovePolicy 从数据库删除与给定规则完全匹配的策略。
func (a casbinAdapter) RemovePolicy(_ string, ptype string, rule []string) error {
	value := ruleFromPolicy(ptype, rule)
	return a.db.
		Where(
			"ptype = ? AND v0 = ? AND v1 = ? AND v2 = ? AND v3 = ? AND v4 = ? AND v5 = ?",
			value.Ptype,
			value.V0,
			value.V1,
			value.V2,
			value.V3,
			value.V4,
			value.V5,
		).
		Delete(&model.SysCasbinRule{}).Error
}

// RemoveFilteredPolicy 按字段条件批量删除匹配的 Casbin 策略。
func (a casbinAdapter) RemoveFilteredPolicy(_ string, ptype string, fieldIndex int, values ...string) error {
	if fieldIndex < 0 || fieldIndex > 5 {
		return fmt.Errorf("invalid casbin rule field index %d", fieldIndex)
	}
	query := a.db.Where("ptype = ?", ptype)
	columns := []string{"v0", "v1", "v2", "v3", "v4", "v5"}
	for index, value := range values {
		if value != "" && fieldIndex+index < len(columns) {
			query = query.Where(columns[fieldIndex+index]+" = ?", value)
		}
	}
	return query.Delete(&model.SysCasbinRule{}).Error
}

// ruleFromPolicy 将 Casbin 规则数组转换为数据库模型。
func ruleFromPolicy(ptype string, values []string) *model.SysCasbinRule {
	fields := [6]string{}
	for index := 0; index < len(values) && index < len(fields); index++ {
		fields[index] = values[index]
	}
	return &model.SysCasbinRule{Ptype: ptype, V0: fields[0], V1: fields[1], V2: fields[2], V3: fields[3], V4: fields[4], V5: fields[5]}
}

// trimRule 移除策略数组末尾无意义的空字段。
func trimRule(values []string) []string {
	end := len(values)
	for end > 1 && values[end-1] == "" {
		end--
	}
	return values[:end]
}
