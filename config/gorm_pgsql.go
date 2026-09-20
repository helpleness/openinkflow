package config

type Pgsql struct {
	Path         string `mapstructure:"path" json:"path" yaml:"path"`                               // 服务器地址
	Port         string `mapstructure:"port" json:"port" yaml:"port"`                               // 端口
	Config       string `mapstructure:"config" json:"config" yaml:"config"`                         // 高级配置，如 sslmode=disable
	Dbname       string `mapstructure:"db-name" json:"db-name" yaml:"db-name"`                      // 数据库名
	Username     string `mapstructure:"username" json:"username" yaml:"username"`                   // 用户名
	Password     string `mapstructure:"password" json:"password" yaml:"password"`                   // 密码
	MaxIdleConns int    `mapstructure:"max-idle-conns" json:"max-idle-conns" yaml:"max-idle-conns"` // 空闲连接数
	MaxOpenConns int    `mapstructure:"max-open-conns" json:"max-open-conns" yaml:"max-open-conns"` // 最大连接数
	LogMode      string `mapstructure:"log-mode" json:"log-mode" yaml:"log-mode"`                   // 日志级别: silent, error, warn, info
}

// Dsn 拼接连接字符串
func (p *Pgsql) Dsn() string {
	return "host=" + p.Path + " user=" + p.Username + " password=" + p.Password + " dbname=" + p.Dbname + " port=" + p.Port + " " + p.Config
}
