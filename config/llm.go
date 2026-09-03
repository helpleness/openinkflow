package config

type LLM struct {
	BaseUrl           string  `mapstructure:"base-url" json:"base-url" yaml:"base-url"`
	ApiKey            string  `mapstructure:"api-key" json:"api-key" yaml:"api-key"`
	ModelDefault      string  `mapstructure:"model-default" json:"model-default" yaml:"model-default"`
	TopP              float64 `mapstructure:"top-p" json:"top-p" yaml:"top-p"`
	TopK              int     `mapstructure:"top-k" json:"top-k" yaml:"top-k"`
	ModelPath         string  `mapstructure:"model-path" json:"model-path" yaml:"model-path"`
	ContextSize       int     `mapstructure:"context-size" json:"context-size" yaml:"context-size"`
	Temperature       float64 `mapstructure:"temperature" json:"temperature" yaml:"temperature"`
	Timeout           int     `mapstructure:"timeout" json:"timeout" yaml:"timeout"`
	InferenceProvider string  `mapstructure:"inference-provider" json:"inference-provider" yaml:"inference-provider"`
}
