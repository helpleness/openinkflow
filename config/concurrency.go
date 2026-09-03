package config

// Concurrency 定义并发控制组件的运行参数。
type Concurrency struct {
	NodeID            int64   `mapstructure:"node-id" json:"node-id" yaml:"node-id"`
	RequestsPerSecond float64 `mapstructure:"requests-per-second" json:"requests-per-second" yaml:"requests-per-second"`
	Burst             int     `mapstructure:"burst" json:"burst" yaml:"burst"`
	CacheEntries      int     `mapstructure:"cache-entries" json:"cache-entries" yaml:"cache-entries"`
}
