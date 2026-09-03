package core

import "testing"

func TestLoopbackAddress(t *testing.T) {
	tests := map[string]string{
		"":               "127.0.0.1:8888",
		":8888":          "127.0.0.1:8888",
		"localhost:9000": "127.0.0.1:9000",
		"127.0.0.1:0":    "127.0.0.1:0",
		"[::1]:9100":     "[::1]:9100",
	}
	for input, want := range tests {
		got, err := LoopbackAddress(input)
		if err != nil || got != want {
			t.Fatalf("LoopbackAddress(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := LoopbackAddress("0.0.0.0:8888"); err == nil {
		t.Fatal("expected wildcard address to be rejected")
	}
}
