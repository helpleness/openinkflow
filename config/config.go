package config

type Server struct {
	System      System      `mapstructure:"system" json:"system" yaml:"system"`
	LLM         LLM         `mapstructure:"llm" json:"llm" yaml:"llm"`
	Pgsql       Pgsql       `mapstructure:"pgsql" json:"pgsql" yaml:"pgsql"`
	Zap         Zap         `mapstructure:"zap" json:"zap" yaml:"zap"`
	LLMLocal    LLMLocal    `mapstructure:"llm-local" json:"llm-local" yaml:"llm-local"`
	Auth        Auth        `mapstructure:"auth" json:"auth" yaml:"auth"`
	RAG         RAG         `mapstructure:"rag" json:"rag" yaml:"rag"`
	Concurrency Concurrency `mapstructure:"concurrency" json:"concurrency" yaml:"concurrency"`
	OCR         OCR         `mapstructure:"ocr" json:"ocr" yaml:"ocr"`
	OSS         OSS         `mapstructure:"oss" json:"oss" yaml:"oss"`
	Export      Export      `mapstructure:"export" json:"export" yaml:"export"`
	// 后续可以加 Log, Redis 等配置
}
