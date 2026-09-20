package config

// OCR 定义本地 ONNX 文档版面检测器的配置。
//
// 检测器只判断图片中是否存在文字、表格、标题、图片等区域；它不做文字识别，也不
// 依赖 Python 或 PaddleX。ModelPath 为空时使用桌面安装目录的默认量化 ONNX 模型。
type OCR struct {
	Enabled        bool    `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
	ModelPath      string  `mapstructure:"model-path" json:"model-path" yaml:"model-path"`
	ScoreThreshold float32 `mapstructure:"score-threshold" json:"score-threshold" yaml:"score-threshold"`
	Threads        int     `mapstructure:"threads" json:"threads" yaml:"threads"`
}
