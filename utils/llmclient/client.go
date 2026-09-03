package llmclient

import (
	"InkFlow/config"
	"InkFlow/global"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
)

var (
	cacheMu     sync.RWMutex
	cacheOnce   sync.Once
	clientCache = map[string]*cacheEntry{}
)

const (
	clientIdleTTL       = 30 * time.Minute
	clientSweepInterval = 5 * time.Minute
)

type cacheEntry struct {
	client   *resty.Client
	lastUsed time.Time
}

func Apply(cfg config.LLM) {
	cfg = Normalize(cfg)
	global.GVA_CONFIG.LLM = cfg
	global.GVA_LLM = ClientFor(cfg)
}

func Normalize(cfg config.LLM) config.LLM {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 300
	}
	return cfg
}

func ClientFor(cfg config.LLM) *resty.Client {
	cacheOnce.Do(startSweeper)

	cfg = Normalize(cfg)
	key := cacheKey(cfg)

	cacheMu.Lock()
	defer cacheMu.Unlock()
	if entry := clientCache[key]; entry != nil {
		entry.lastUsed = time.Now()
		return entry.client
	}
	client := BuildClient(cfg.BaseUrl, cfg.ApiKey, cfg.Timeout)
	clientCache[key] = &cacheEntry{client: client, lastUsed: time.Now()}
	return client
}

func Forget(cfg config.LLM) {
	key := cacheKey(Normalize(cfg))
	cacheMu.Lock()
	entry := clientCache[key]
	delete(clientCache, key)
	cacheMu.Unlock()
	if entry != nil {
		closeIdle(entry.client)
	}
}

func startSweeper() {
	go func() {
		for {
			if err := runSweeperLoop(); err != nil {
				log.Printf("llm client cache sweeper restarting after panic: %v", err)
				time.Sleep(time.Second)
				continue
			}
			return
		}
	}()
}

func runSweeperLoop() (panicValue any) {
	defer func() {
		panicValue = recover()
	}()
	ticker := time.NewTicker(clientSweepInterval)
	defer ticker.Stop()
	for range ticker.C {
		sweepIdle(time.Now())
	}
	return nil
}

func sweepIdle(now time.Time) {
	var expired []*resty.Client
	cacheMu.Lock()
	for key, entry := range clientCache {
		if entry == nil {
			delete(clientCache, key)
			continue
		}
		if now.Sub(entry.lastUsed) < clientIdleTTL {
			continue
		}
		expired = append(expired, entry.client)
		delete(clientCache, key)
	}
	cacheMu.Unlock()

	for _, client := range expired {
		closeIdle(client)
	}
}

func closeIdle(client *resty.Client) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("llm client cache close idle connections skipped after panic: %v", r)
		}
	}()
	if client != nil && client.GetClient() != nil {
		client.GetClient().CloseIdleConnections()
	}
}

func cacheKey(cfg config.LLM) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", cfg.BaseUrl, cfg.ApiKey, cfg.Timeout)))
	return hex.EncodeToString(sum[:])
}

func BuildClient(baseURL string, apiKey string, timeout int) *resty.Client {
	client := resty.New()
	client.SetTimeout(time.Duration(timeout) * time.Second)
	client.SetBaseURL(baseURL)
	if apiKey != "" {
		client.SetAuthToken(apiKey)
	}
	client.SetHeader("Content-Type", "application/json")
	client.SetHeader("Accept", "application/json")
	return client
}
