package config

// OSS configures private Alibaba Cloud Object Storage from the application YAML.
type OSS struct {
	Endpoint        string `mapstructure:"endpoint" json:"endpoint" yaml:"endpoint"`
	Bucket          string `mapstructure:"bucket" json:"bucket" yaml:"bucket"`
	Region          string `mapstructure:"region" json:"region" yaml:"region"`
	AccessKeyID     string `mapstructure:"access-key-id" json:"access-key-id" yaml:"access-key-id"`
	AccessKeySecret string `mapstructure:"access-key-secret" json:"access-key-secret" yaml:"access-key-secret"`
	SignedURLExpire string `mapstructure:"signed-url-expire" json:"signed-url-expire" yaml:"signed-url-expire"`
}
