package config

import (
	"testing"
	"time"
)

func TestNormalizeRemoteAuthBaseURL(t *testing.T) {
	for _, testCase := range []struct {
		input string
		want  string
	}{
		{input: " https://AUTH.Example.com/api/ ", want: "https://auth.example.com/api"},
		{input: "http://localhost:8080", want: "http://localhost:8080"},
		{input: "not a URL", want: ""},
		{input: "", want: ""},
	} {
		if got := NormalizeRemoteAuthBaseURL(testCase.input); got != testCase.want {
			t.Fatalf("NormalizeRemoteAuthBaseURL(%q) = %q, want %q", testCase.input, got, testCase.want)
		}
	}
}

func TestNormalizeOAuthProvidersUsesKnownDefaults(t *testing.T) {
	auth := Auth{OAuthProviders: map[string]OAuthProvider{
		"google": {Enabled: true, ClientID: "id", ClientSecret: "secret"},
	}}
	auth.NormalizeOAuthProviders()

	if auth.PublicBaseURL != "http://127.0.0.1:8888" || auth.FrontendBaseURL != "http://localhost:5173" {
		t.Fatalf("unexpected OAuth base URLs: %#v", auth)
	}
	if auth.StateTTL != 10*time.Minute {
		t.Fatalf("unexpected state TTL: %s", auth.StateTTL)
	}
	provider := auth.OAuthProviders["google"]
	if provider.AuthURL != "https://accounts.google.com/o/oauth2/v2/auth" || !provider.UsePKCE || provider.IDField != "sub" {
		t.Fatalf("Google defaults were not applied: %#v", provider)
	}
	if provider.DisplayName != "Google" || len(provider.Scopes) != 3 {
		t.Fatalf("unexpected Google provider defaults: %#v", provider)
	}
}
