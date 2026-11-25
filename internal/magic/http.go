package magic

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

const Version = "0.0.0-dev"

type API struct {
	Config                                                  Config
	Store                                                   *Store
	accepted, rejected, authFailed, policyFailed, genFailed atomic.Uint64
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func (a *API) reject(w http.ResponseWriter, status int, code, message, path string) {
	a.rejected.Add(1)
	reply(w, status, map[string]string{"error": code, "message": message, "path": path})
	slog.Warn("request rejected", "status", status, "reason", code)
}
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, map[string]bool{"healthy": true}) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ok := a.Store.Ready()
		code := 200
		if !ok {
			code = 503
		}
		reply(w, code, map[string]bool{"ready": ok})
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		d, s, p := a.Store.Counts()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprintf(w, "magickuma_build_info{version=%q} 1\nmagickuma_accepted_total %d\nmagickuma_rejected_total %d\nmagickuma_auth_failures_total %d\nmagickuma_policy_failures_total %d\nmagickuma_generation_retry_failures_total %d\nmagickuma_declared_monitors %d\nmagickuma_stale_clients %d\nmagickuma_pending_retirements %d\n", Version, a.accepted.Load(), a.rejected.Load(), a.authFailed.Load(), a.policyFailed.Load(), a.genFailed.Load(), d, s, p)
	})
	auth := func(w http.ResponseWriter, r *http.Request) string {
		values := r.Header.Values("Authorization")
		if len(values) == 1 {
			kind, token, _ := strings.Cut(values[0], " ")
			if strings.EqualFold(kind, "Bearer") && token != "" {
				if id := a.Config.Authenticate(token); id != "" {
					return id
				}
			}
		}
		a.authFailed.Add(1)
		w.Header().Set("WWW-Authenticate", "Bearer")
		a.reject(w, 401, "unauthorized", "valid bearer token required", "")
		return ""
	}
	mux.HandleFunc("GET /api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		if id := auth(w, r); id != "" {
			reply(w, 200, a.Store.Status(id, time.Now().UTC()))
		}
	})
	mux.HandleFunc("PUT /api/v1/monitors", func(w http.ResponseWriter, r *http.Request) {
		id := auth(w, r)
		if id == "" {
			return
		}
		typ, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || typ != "application/json" || r.Header.Get("Content-Encoding") != "" {
			a.reject(w, 415, "media_type", "send uncompressed application/json", "")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, MaxBody)
		b, err := io.ReadAll(r.Body)
		if err != nil {
			var big *http.MaxBytesError
			if errors.As(err, &big) {
				a.reject(w, 413, "too_large", "maximum 1 MiB", "")
			} else {
				a.reject(w, 400, "invalid_body", "incomplete body", "")
			}
			return
		}
		now := time.Now().UTC()
		m, err := Parse(b)
		var changed bool
		if err == nil {
			changed, err = a.Store.Publish(id, m, now)
			if err == nil {
				a.accepted.Add(1)
				slog.Info("publication accepted", "client", id, "changed", changed, "monitors", len(m.Monitors))
				reply(w, 200, map[string]any{"acceptedAt": now, "changed": changed})
				return
			}
			var p *Problem
			if !errors.As(err, &p) {
				slog.Error("storage failed", "error", err)
				a.reject(w, 503, "storage", "storage unavailable", "")
				return
			}
		}
		var p *Problem
		if errors.As(err, &p) {
			a.policyFailed.Add(1)
			a.reject(w, 422, "invalid_monitor", p.Reason, p.Path)
		} else {
			a.reject(w, 400, "invalid_json", "invalid JSON or incompatible field type", "")
		}
	})
	return mux
}
func (a *API) Retry() {
	if err := a.Store.Retry(); err != nil {
		a.genFailed.Add(1)
		slog.Error("generation retry failed", "error", err)
	}
}
