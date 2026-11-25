package magic

import (
	"fmt"
	"net/http"
	"time"
)

// Statistics contains process-wide operational data safe to expose to a scraper.
type Statistics struct {
	Declared, Stale, Pending int
	GenerationFailures       uint64
	GenerationPending        bool
}

func (s *Store) Statistics() Statistics {
	s.mu.Lock()
	defer s.mu.Unlock()
	stats := Statistics{GenerationFailures: s.generationFailures, GenerationPending: s.dirty}
	now := time.Now()
	for id, policy := range s.c.Clients {
		if len(policy.TokenHashFiles) > 0 && (s.last[id].IsZero() || now.Sub(s.last[id]) > s.c.stale) {
			stats.Stale++
		}
	}
	for _, r := range s.records {
		stats.Declared += len(r.Manifest.Monitors)
		stats.Pending += len(r.Pending)
	}
	return stats
}

func (a *API) metrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	s := a.Store.Statistics()
	metric := func(kind, name, help string, value any) {
		fmt.Fprintf(w, "# HELP magickuma_%s %s\n# TYPE magickuma_%s %s\nmagickuma_%s %v\n", name, help, name, kind, name, value)
	}
	fmt.Fprintf(w, "# TYPE magickuma_build_info gauge\nmagickuma_build_info{version=%q} 1\n", Version)
	metric("counter", "accepted_total", "Durably accepted publications.", a.accepted.Load())
	metric("counter", "rejected_total", "Rejected API requests.", a.rejected.Load())
	metric("counter", "auth_failures_total", "Requests with missing or invalid credentials.", a.authFailed.Load())
	metric("counter", "policy_failures_total", "Requests rejected by schema or client policy.", a.policyFailed.Load())
	metric("counter", "generation_failures_total", "Failed aggregate writes or reads during generation.", s.GenerationFailures)
	pending := 0
	if s.GenerationPending {
		pending = 1
	}
	metric("gauge", "generation_pending", "Desired state awaiting local generation.", pending)
	metric("gauge", "declared_monitors", "Monitors in current persisted declarations.", s.Declared)
	metric("gauge", "stale_clients", "Credentialed clients with missing or overdue publications.", s.Stale)
	metric("gauge", "pending_retirements", "Checks awaiting explicit operator retirement.", s.Pending)
}
