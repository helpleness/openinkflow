package config

// Export controls server-side official-document rendering. DOCX generation is
// built in; PDF uses a configured LibreOffice/soffice executable so production
// fonts, pagination and PDF/A policy stay under the operator's control.
type Export struct {
	OfficeCommand  string `mapstructure:"office-command" json:"office-command" yaml:"office-command"`
	TimeoutSeconds int    `mapstructure:"timeout-seconds" json:"timeout-seconds" yaml:"timeout-seconds"`
}
