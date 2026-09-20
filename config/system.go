package config

type System struct {
	DataDir                string `mapstructure:"data-dir" json:"data-dir" yaml:"data-dir"`
	ModelCachePath         string `mapstructure:"model-cache-path" json:"model-cache-path" yaml:"model-cache-path"`
	Env                    string `mapstructure:"env" json:"env" yaml:"env"`             // 环境值: public, develop
	Addr                   string `mapstructure:"addr" json:"addr" yaml:"addr"`          // 端口号: :8888
	DbType                 string `mapstructure:"db-type" json:"db-type" yaml:"db-type"` // 数据库类型: sqlite
	DbPath                 string `mapstructure:"db-path" json:"db-path" yaml:"db-path"` // SQLite 文件路径
	BootstrapOwnerUsername string `mapstructure:"bootstrap-owner-username" json:"bootstrap-owner-username" yaml:"bootstrap-owner-username"`
	BootstrapOwnerPassword string `mapstructure:"bootstrap-owner-password" json:"bootstrap-owner-password" yaml:"bootstrap-owner-password"`
	BootstrapOrganization  string `mapstructure:"bootstrap-organization" json:"bootstrap-organization" yaml:"bootstrap-organization"`
}
