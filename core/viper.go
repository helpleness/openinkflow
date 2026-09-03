package core

import (
	"InkFlow/global"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

// desktopInferenceProvider is written by scripts/build_client.ps1 through
// -ldflags. "local" remains the safe release default.
var desktopInferenceProvider = "local"

// desktopBackend is also written by scripts/build_client.ps1. It records the
// llama.cpp backend linked into this executable; changing YAML cannot switch a
// compiled CPU/CUDA/Vulkan binary to another backend.
var desktopBackend = "cpu"

const clientConfigFileName = "config.yaml"

func InitializeViper() *viper.Viper {
	v := viper.New()
	v.SetConfigFile("config.yaml") // 指定配置文件路径
	v.SetConfigType("yaml")
	v.SetDefault("auth.remote-timeout-seconds", 15)
	v.SetDefault("auth.mfa-enrollment-required", false)

	err := v.ReadInConfig()
	if err != nil {
		panic(fmt.Errorf("Fatal error config file: %s \n", err))
	}

	// 监听配置文件变化，热加载
	v.WatchConfig()
	v.OnConfigChange(func(e fsnotify.Event) {
		fmt.Println("Config file changed:", e.Name)
		if err = v.Unmarshal(&global.GVA_CONFIG); err != nil {
			fmt.Println(err)
		} else {
			global.GVA_CONFIG.Auth.NormalizeOAuthProviders()
			applyEnvironmentCredentialOverrides()
		}
	})

	// 将配置赋值给全局变量
	if err = v.Unmarshal(&global.GVA_CONFIG); err != nil {
		fmt.Println(err)
	} else {
		global.GVA_CONFIG.Auth.NormalizeOAuthProviders()
		applyEnvironmentCredentialOverrides()
	}
	return v
}

// InitializeClientViper initializes desktop configuration. The installer seeds
// %LOCALAPPDATA%\InkFlow\config.yaml on first install and never overwrites it
// on upgrades. INKFLOW_CLIENT_CONFIG has priority for portable/development use.
func InitializeClientViper() *viper.Viper {
	v := viper.New()
	v.SetConfigType("yaml")
	inferenceProvider := normalizeDesktopInferenceProvider(desktopInferenceProvider)
	backend := normalizeDesktopBackend(desktopBackend)
	defaultGPULayers := desktopDefaultGPULayers(backend)

	for key, value := range map[string]any{
		"system.env":                          "desktop",
		"system.addr":                         "127.0.0.1:0",
		"system.db-type":                      "sqlite",
		"auth.remote-timeout-seconds":         15,
		"auth.mfa-enrollment-required":        false,
		"llm.inference-provider":              inferenceProvider,
		"llm.context-size":                    4096,
		"llm.temperature":                     0.7,
		"llm.timeout":                         600,
		"llm-local.backend":                   backend,
		"llm-local.chat.context-size":         4096,
		"llm-local.chat.threads":              8,
		"llm-local.chat.threads-batch":        8,
		"llm-local.chat.flash-attn-auto":      true,
		"llm-local.chat.gpu-layers":           defaultGPULayers,
		"llm-local.chat.main-gpu":             0,
		"llm-local.chat.split-mode":           "none",
		"llm-local.embedding.context-size":    2048,
		"llm-local.embedding.threads":         8,
		"llm-local.embedding.threads-batch":   8,
		"llm-local.embedding.flash-attn-auto": true,
		"llm-local.embedding.gpu-layers":      defaultGPULayers,
		"llm-local.embedding.main-gpu":        0,
		"llm-local.embedding.split-mode":      "none",
		"llm-local.rerank.context-size":       8192,
		"llm-local.rerank.threads":            8,
		"llm-local.rerank.threads-batch":      8,
		"llm-local.rerank.flash-attn-auto":    true,
		"llm-local.rerank.max-sequences":      desktopDefaultRerankMaxSequences(backend),
		"llm-local.rerank.gpu-layers":         defaultGPULayers,
		"llm-local.rerank.main-gpu":           0,
		"llm-local.rerank.split-mode":         "none",
		"rag.vector-backend":                  "usearch",
		"rag.vector-dimension":                1024,
		"rag.hnsw-m":                          32,
		"rag.hnsw-ef-construction":            256,
		"rag.hnsw-ef-search":                  64,
		"ocr.enabled":                         true,
		"ocr.score-threshold":                 0.3,
		"ocr.threads":                         2,
		"zap.level":                           "info",
		"zap.format":                          "console",
		"zap.prefix":                          "[InkFlow]",
		"zap.encode-level":                    "LowercaseLevelEncoder",
		"zap.stacktrace-key":                  "stacktrace",
		"zap.max-age":                         7,
		"zap.show-line":                       true,
		"zap.log-in-console":                  false,
	} {
		v.SetDefault(key, value)
	}

	configPath, explicitConfig, err := clientConfigPath()
	if err != nil {
		panic(fmt.Errorf("resolve desktop config path: %w", err))
	}
	if configPath != "" {
		if _, statErr := os.Stat(configPath); statErr == nil {
			v.SetConfigFile(configPath)
			if err := v.ReadInConfig(); err != nil {
				panic(fmt.Errorf("read desktop config %q: %w", configPath, err))
			}
			v.WatchConfig()
			v.OnConfigChange(func(e fsnotify.Event) {
				fmt.Println("Desktop config file changed:", e.Name)
				applyDesktopBuildSelection(v, inferenceProvider, backend)
				if err := v.Unmarshal(&global.GVA_CONFIG); err != nil {
					fmt.Println(err)
				}
			})
		} else if explicitConfig || !os.IsNotExist(statErr) {
			panic(fmt.Errorf("read desktop config %q: %w", configPath, statErr))
		}
	}
	if strings.TrimSpace(v.GetString("auth.jwt-secret")) == "" {
		secret, secretErr := desktopCredentialSecret(filepath.Dir(configPath))
		if secretErr != nil {
			panic(fmt.Errorf("prepare desktop authentication secret: %w", secretErr))
		}
		v.Set("auth.jwt-secret", secret)
	}

	applyDesktopBuildSelection(v, inferenceProvider, backend)
	if err := v.Unmarshal(&global.GVA_CONFIG); err != nil {
		panic(fmt.Errorf("load desktop defaults: %w", err))
	}
	applyEnvironmentCredentialOverrides()
	return v
}

// applyEnvironmentCredentialOverrides keeps credentials out of committed YAML
// files. Deployment-specific values must be supplied through the process
// environment or an untracked local configuration file.
func applyEnvironmentCredentialOverrides() {
	set := func(name string, assign func(string)) {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			assign(value)
		}
	}

	set("INKFLOW_SYSTEM_BOOTSTRAP_OWNER_PASSWORD", func(value string) {
		global.GVA_CONFIG.System.BootstrapOwnerPassword = value
	})
	set("INKFLOW_AUTH_JWT_SECRET", func(value string) {
		global.GVA_CONFIG.Auth.JWTSecret = value
	})
	set("INKFLOW_OSS_ACCESS_KEY_ID", func(value string) {
		global.GVA_CONFIG.OSS.AccessKeyID = value
	})
	set("INKFLOW_OSS_ACCESS_KEY_SECRET", func(value string) {
		global.GVA_CONFIG.OSS.AccessKeySecret = value
	})
	set("INKFLOW_LLM_API_KEY", func(value string) {
		global.GVA_CONFIG.LLM.ApiKey = value
	})
	set("INKFLOW_PGSQL_PASSWORD", func(value string) {
		global.GVA_CONFIG.Pgsql.Password = value
	})

	setOAuth := func(providerName, envPrefix string) {
		provider := global.GVA_CONFIG.Auth.OAuthProviders[providerName]
		set(envPrefix+"_CLIENT_ID", func(value string) { provider.ClientID = value })
		set(envPrefix+"_CLIENT_SECRET", func(value string) { provider.ClientSecret = value })
		if provider.ClientID != "" || provider.ClientSecret != "" {
			global.GVA_CONFIG.Auth.OAuthProviders[providerName] = provider
		}
	}
	setOAuth("google", "INKFLOW_AUTH_OAUTH_GOOGLE")
	setOAuth("github", "INKFLOW_AUTH_OAUTH_GITHUB")
	global.GVA_CONFIG.Auth.NormalizeOAuthProviders()
}

// desktopCredentialSecret keeps one random authentication secret beside the
// desktop user's local database. It is only used when the deployment did not
// explicitly configure auth.jwt-secret, so local sessions remain valid across
// an application restart without exposing the secret in YAML.
func desktopCredentialSecret(dataDir string) (string, error) {
	// 保持历史文件名，避免已有桌面用户在更新后失去本地会话签名密钥。
	secretPath := filepath.Join(dataDir, "model-config.key")
	if raw, err := os.ReadFile(secretPath); err == nil {
		if secret := strings.TrimSpace(string(raw)); secret != "" {
			return secret, nil
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return "", err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	secret := hex.EncodeToString(raw)
	if err := os.WriteFile(secretPath, []byte(secret), 0o600); err != nil {
		return "", err
	}
	return secret, nil
}

func clientConfigPath() (path string, explicit bool, err error) {
	if configured := strings.TrimSpace(os.Getenv("INKFLOW_CLIENT_CONFIG")); configured != "" {
		path, err = filepath.Abs(configured)
		return path, true, err
	}
	root, err := defaultClientDataDir()
	if err != nil {
		return "", false, err
	}
	return filepath.Join(root, clientConfigFileName), false, nil
}

func applyDesktopBuildSelection(v *viper.Viper, inferenceProvider, backend string) {
	// These values are selected at build time. The remaining llm-local settings
	// (including gpu-layers, model paths, batches, and threads) stay configurable.
	v.Set("llm.inference-provider", inferenceProvider)
	v.Set("llm-local.backend", backend)
	if backend == "cpu" {
		// A CPU-only executable must not retain -1 from a config previously
		// used by CUDA/Vulkan. llama.cpp rejects GPU offload when that backend
		// was not compiled into the binary.
		v.Set("llm-local.chat.gpu-layers", 0)
		v.Set("llm-local.embedding.gpu-layers", 0)
		v.Set("llm-local.rerank.gpu-layers", 0)
	}
}

func desktopDefaultGPULayers(backend string) int {
	if backend == "cpu" {
		return 0
	}
	return -1
}

func desktopDefaultRerankMaxSequences(backend string) int {
	// On Ada CUDA, two sequences per graph keeps all 107 documents while
	// avoiding the large elementwise and quantization working sets observed
	// with the former 16-sequence graph. Vulkan and CPU retain their existing
	// scheduling default unless explicitly configured in YAML.
	if backend == "cuda" {
		return 2
	}
	return 16
}

func normalizeDesktopInferenceProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "frontend":
		return "frontend"
	case "", "local":
		return "local"
	default:
		return "local"
	}
}

func normalizeDesktopBackend(backend string) string {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "cuda", "vulkan", "cpu":
		return strings.ToLower(strings.TrimSpace(backend))
	default:
		return "cpu"
	}
}
