package inference

import (
	"context"
	"testing"
	"time"
)

func TestFrontendBrokerRegistrySeparatesUsersAndClients(t *testing.T) {
	registry := newFrontendBrokerRegistry(time.Second)
	first := registry.BrokerForContext(WithFrontendClient(context.Background(), 1, "browser-a"))
	same := registry.BrokerForContext(WithFrontendClient(context.Background(), 1, "browser-a"))
	otherClient := registry.BrokerForContext(WithFrontendClient(context.Background(), 1, "browser-b"))
	otherUser := registry.BrokerForContext(WithFrontendClient(context.Background(), 2, "browser-a"))

	if first != same {
		t.Fatal("same user and client did not reuse the same broker")
	}
	if first == otherClient {
		t.Fatal("different browser clients unexpectedly shared a broker")
	}
	if first == otherUser {
		t.Fatal("different users unexpectedly shared a broker")
	}
}

func TestFrontendClientIDDefaultsWhenMissing(t *testing.T) {
	ctx := WithFrontendClient(context.Background(), 7, "")
	userID, clientID := FrontendClientFromContext(ctx)
	if userID != 7 || clientID != defaultFrontendClientID {
		t.Fatalf("identity = (%d, %q), want (7, %q)", userID, clientID, defaultFrontendClientID)
	}
}
