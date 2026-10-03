package httpserver

import (
	"net/http"
	"testing"
)

func TestNewRequiresHandler(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("New(nil) error = nil, want error")
	}
}

func TestNewUsesDefaultAddress(t *testing.T) {
	t.Setenv("HTTP_PORT", "")
	server, err := New(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if server.httpServer.Addr != ":8080" {
		t.Fatalf("Addr = %q, want :8080", server.httpServer.Addr)
	}
}

func TestNewUsesConfiguredPort(t *testing.T) {
	t.Setenv("HTTP_PORT", "9090")
	server, err := New(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if server.httpServer.Addr != ":9090" {
		t.Fatalf("Addr = %q, want :9090", server.httpServer.Addr)
	}
}
