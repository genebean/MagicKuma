package magic

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// Version is set by the release build.
var Version = "0.0.0-dev"

type API struct {
	Config Config
	Store  *Store

	accepted     atomic.Uint64
	rejected     atomic.Uint64
	authFailed   atomic.Uint64
	policyFailed atomic.Uint64
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		reply(w, http.StatusOK, map[string]bool{"healthy": true})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		probe(w, "ready", a.Store.Ready())
	})
	mux.HandleFunc("GET /freshz", func(w http.ResponseWriter, r *http.Request) {
		probe(w, "fresh", a.Store.Statistics().Stale == 0)
	})
	mux.HandleFunc("GET /metrics", a.metrics)
	mux.HandleFunc("GET /api/v1/status", a.status)
	mux.HandleFunc("PUT /api/v1/monitors", a.publish)
	return mux
}

func reply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func probe(w http.ResponseWriter, field string, healthy bool) {
	status := http.StatusOK
	if !healthy {
		status = http.StatusServiceUnavailable
	}
	reply(w, status, map[string]bool{field: healthy})
}

func (a *API) reject(w http.ResponseWriter, status int, code, message, path string) {
	a.rejected.Add(1)
	reply(w, status, map[string]string{"error": code, "message": message, "path": path})
	slog.Warn("request rejected", "status", status, "reason", code)
}

func (a *API) authenticate(w http.ResponseWriter, r *http.Request) string {
	values := r.Header.Values("Authorization")
	if len(values) == 1 {
		kind, token, found := strings.Cut(values[0], " ")
		if found && strings.EqualFold(kind, "Bearer") && token != "" {
			if id := a.Config.Authenticate(token); id != "" {
				return id
			}
		}
	}
	a.authFailed.Add(1)
	w.Header().Set("WWW-Authenticate", "Bearer")
	a.reject(w, http.StatusUnauthorized, "unauthorized", "valid bearer token required", "")
	return ""
}

func (a *API) status(w http.ResponseWriter, r *http.Request) {
	if id := a.authenticate(w, r); id != "" {
		reply(w, http.StatusOK, a.Store.Status(id, time.Now().UTC()))
	}
}

func (a *API) publish(w http.ResponseWriter, r *http.Request) {
	id := a.authenticate(w, r)
	if id == "" {
		return
	}
	typ, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || typ != "application/json" || r.Header.Get("Content-Encoding") != "" {
		a.reject(w, http.StatusUnsupportedMediaType, "media_type", "send uncompressed application/json", "")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			a.reject(w, http.StatusRequestEntityTooLarge, "too_large", "maximum 1 MiB", "")
		} else {
			a.reject(w, http.StatusBadRequest, "invalid_body", "incomplete body", "")
		}
		return
	}

	manifest, err := Parse(body)
	if err != nil {
		if !a.rejectProblem(w, err) {
			a.reject(w, http.StatusBadRequest, "invalid_json", "invalid JSON or incompatible field type", "")
		}
		return
	}
	now := time.Now().UTC()
	changed, err := a.Store.Publish(id, manifest, now)
	if err != nil {
		if !a.rejectProblem(w, err) {
			slog.Error("storage failed", "error", err)
			a.reject(w, http.StatusServiceUnavailable, "storage", "storage unavailable", "")
		}
		return
	}
	a.accepted.Add(1)
	slog.Info("publication accepted", "client", id, "changed", changed, "monitors", len(manifest.Monitors))
	reply(w, http.StatusOK, map[string]any{"acceptedAt": now, "changed": changed})
}

func (a *API) rejectProblem(w http.ResponseWriter, err error) bool {
	var problem *Problem
	if !errors.As(err, &problem) {
		return false
	}
	a.policyFailed.Add(1)
	a.reject(w, http.StatusUnprocessableEntity, "invalid_monitor", problem.Reason, problem.Path)
	return true
}

func (a *API) Retry() {
	if err := a.Store.Retry(); err != nil {
		slog.Error("generation retry failed", "error", err)
	}
}
