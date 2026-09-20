package inference

import (
	"context"
	"strings"
	"sync"
	"time"

	ws "InkFlow/utils/websocket"

	gorilla "github.com/gorilla/websocket"
)

const defaultFrontendClientID = "default"

type frontendClientContextKey struct{}

type frontendClientIdentity struct {
	UserID   uint
	ClientID string
}

type frontendBrokerRegistry struct {
	mu       sync.RWMutex
	brokers  map[frontendClientIdentity]*ws.Broker
	active   map[frontendClientIdentity]bool
	latest   frontendClientIdentity
	timeout  time.Duration
	fallback *ws.Broker
}

func newFrontendBrokerRegistry(timeout time.Duration) *frontendBrokerRegistry {
	return &frontendBrokerRegistry{
		brokers:  make(map[frontendClientIdentity]*ws.Broker),
		active:   make(map[frontendClientIdentity]bool),
		timeout:  timeout,
		fallback: ws.NewBroker(timeout),
	}
}

var FrontendClients = newFrontendBrokerRegistry(5 * time.Minute)

// FrontendBroker remains available for callers that explicitly inject a
// broker in tests. Runtime websocket clients are registered in FrontendClients
// so one browser cannot replace another browser's worker connection.
var FrontendBroker = FrontendClients.fallback

func WithFrontendClient(ctx context.Context, userID uint, clientID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, frontendClientContextKey{}, frontendClientIdentity{
		UserID:   userID,
		ClientID: normalizeFrontendClientID(clientID),
	})
}

func FrontendClientFromContext(ctx context.Context) (uint, string) {
	if ctx == nil {
		return 0, ""
	}
	identity, _ := ctx.Value(frontendClientContextKey{}).(frontendClientIdentity)
	return identity.UserID, identity.ClientID
}

func normalizeFrontendClientID(clientID string) string {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return defaultFrontendClientID
	}
	if len(clientID) > 128 {
		return clientID[:128]
	}
	return clientID
}

func (r *frontendBrokerRegistry) broker(identity frontendClientIdentity) *ws.Broker {
	identity.ClientID = normalizeFrontendClientID(identity.ClientID)
	r.mu.Lock()
	defer r.mu.Unlock()
	broker := r.brokers[identity]
	if broker == nil {
		broker = ws.NewBroker(r.timeout)
		r.brokers[identity] = broker
	}
	return broker
}

func (r *frontendBrokerRegistry) Register(ctx context.Context, userID uint, clientID string, conn *gorilla.Conn) {
	identity := frontendClientIdentity{UserID: userID, ClientID: normalizeFrontendClientID(clientID)}
	broker := r.broker(identity)
	r.mu.Lock()
	r.active[identity] = true
	r.latest = identity
	r.mu.Unlock()

	broker.Register(ctx, conn)

	r.mu.Lock()
	r.active[identity] = false
	r.mu.Unlock()
}

func (r *frontendBrokerRegistry) BrokerForContext(ctx context.Context) *ws.Broker {
	userID, clientID := FrontendClientFromContext(ctx)
	if userID > 0 {
		return r.broker(frontendClientIdentity{UserID: userID, ClientID: clientID})
	}

	r.mu.RLock()
	latest := r.latest
	isActive := r.active[latest]
	broker := r.brokers[latest]
	r.mu.RUnlock()
	if isActive && broker != nil {
		return broker
	}
	return r.fallback
}
