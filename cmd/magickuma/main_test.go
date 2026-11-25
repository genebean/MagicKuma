package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestPublisherTLSBoundary(t *testing.T) {
	token := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(token, []byte("synthetic-test-credential"), 0600); err != nil {
		t.Fatal(err)
	}
	var forwarded atomic.Bool
	destination := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded.Store(true)
	}))
	defer destination.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	if err := request(context.Background(), "status", source.URL, token, nil); err == nil {
		t.Fatal("accepted an untrusted test certificate")
	}
	original := http.DefaultTransport
	http.DefaultTransport = source.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = original })
	if err := request(context.Background(), "status", source.URL, token, nil); err == nil || forwarded.Load() {
		t.Fatal("expected a redirect error without forwarding the bearer token")
	}
}

func TestTokenGenerationPreservesExistingFile(t *testing.T) {
	dir := t.TempDir()
	token, hash := filepath.Join(dir, "token"), filepath.Join(dir, "hash")
	if err := os.WriteFile(hash, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := generate(token, hash); err == nil {
		t.Fatal("overwrote an existing hash")
	}
	if _, err := os.Stat(token); !os.IsNotExist(err) {
		t.Fatal("left a token after a failed generation")
	}
	b, err := os.ReadFile(hash)
	if err != nil || string(b) != "existing" {
		t.Fatal("changed an existing hash")
	}
}

func TestPublisherRetryClassification(t *testing.T) {
	token := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(token, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	var code atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(int(code.Load()))
	}))
	defer server.Close()
	original := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = original })
	for status, rejected := range map[int]bool{401: true, 413: true, 422: true, 429: false, 503: false} {
		code.Store(int32(status))
		err := request(context.Background(), "publish", server.URL, token, nil)
		if err == nil || errors.Is(err, errRejected) != rejected {
			t.Errorf("HTTP %d: unexpected error classification: %v", status, err)
		}
	}
}
