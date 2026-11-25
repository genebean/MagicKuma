package magic

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Types reserves every ID ever accepted, including omissions. Pending entries
// name checks that an operator must retire in Kuma. One disk commit covers both.
type record struct {
	Manifest  Manifest          `json:"manifest"`
	Types     map[string]string `json:"types"`
	Pending   map[string]string `json:"pending"`
	ChangedAt time.Time         `json:"changedAt"`
}
type Status struct {
	LastPublication time.Time         `json:"lastPublication"`
	Stale           bool              `json:"stale"`
	Declared        int               `json:"declared"`
	Pending         map[string]string `json:"pendingRetirement,omitempty"`
}

var errUncertain = errors.New("rename committed but directory sync failed; restart receiver")

type Store struct {
	fenced  bool
	mu      sync.Mutex
	c       Config
	lock    *os.File
	records map[string]record
	last    map[string]time.Time
	dirty   bool
}

func atomicFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".pending-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("%w: %v", errUncertain, err)
	}
	return nil
}
func syncDir(dir string) error {
	d, e := os.Open(dir)
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
func Open(c Config) (*Store, error) {
	for _, d := range []string{c.StateDir, filepath.Join(c.StateDir, "manifests"), filepath.Join(c.StateDir, "metadata"), filepath.Dir(c.GeneratedFile)} {
		if e := os.MkdirAll(d, 0700); e != nil {
			return nil, e
		}
	}
	if e := syncDir(c.StateDir); e != nil {
		return nil, e
	}
	lock, e := os.OpenFile(filepath.Join(c.StateDir, "writer.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		lock.Close()
		return nil, errors.New("state directory already has a writer")
	}
	s := &Store{c: c, lock: lock, records: map[string]record{}, last: map[string]time.Time{}, dirty: true}
	if e = s.restore(); e != nil {
		s.Close()
		return nil, e
	}
	return s, nil
}
func (s *Store) Close() error               { return s.lock.Close() }
func (s *Store) path(dir, id string) string { return filepath.Join(s.c.StateDir, dir, id+".json") }
func (s *Store) restore() error {
	entries, err := os.ReadDir(filepath.Join(s.c.StateDir, "manifests"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".pending-") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !idPattern.MatchString(id) || entry.Name() != id+".json" || !entry.Type().IsRegular() {
			return errors.New("unexpected file in manifests")
		}
		b, e := os.ReadFile(s.path("manifests", id))
		if e != nil {
			return e
		}
		var r record
		if e = Decode(b, &r); e != nil {
			return fmt.Errorf("invalid record %s: %w", id, e)
		}
		if r.Manifest.SchemaVersion != 1 || r.Manifest.Monitors == nil || r.Types == nil || r.Pending == nil || r.ChangedAt.IsZero() {
			return fmt.Errorf("incomplete record %s", id)
		}
		p, configured := s.c.Clients[id]
		changed := false
		for key, m := range r.Manifest.Monitors {
			if !idPattern.MatchString(key) || m == nil || m["type"] != r.Types[key] {
				return fmt.Errorf("inconsistent identity in %s", id)
			}
			reason := ""
			switch {
			case !configured:
				reason = "client missing from policy"
			case len(r.Manifest.Monitors) > p.MaxMonitors:
				reason = "quota reduced"
			default:
				if e := p.monitor(key, m); e != nil {
					reason = e.Error()
				}
			}
			if reason != "" && r.Pending[key] == "" {
				r.Pending[key] = reason
				changed = true
			}
		}
		if changed {
			if err := s.save(id, r); err != nil {
				return err
			}
		}
		s.records[id] = r
		s.last[id] = r.ChangedAt
		b, e = os.ReadFile(s.path("metadata", id))
		if e == nil {
			var t time.Time
			if err := json.Unmarshal(b, &t); err != nil {
				return err
			}
			if t.After(r.ChangedAt) {
				s.last[id] = t
			}
		} else if !os.IsNotExist(e) {
			return e
		}
	}
	return s.render()
}
func (s *Store) save(id string, r record) error {
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	return atomicFile(s.path("manifests", id), b, 0600)
}
func (s *Store) Publish(id string, m Manifest, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fenced {
		return false, errUncertain
	}
	if now.Before(s.last[id]) {
		now = s.last[id]
	}
	p, ok := s.c.Clients[id]
	if !ok {
		return false, errors.New("unconfigured client")
	}
	if err := p.Validate(m); err != nil {
		return false, err
	}
	old := s.records[id]
	for key, item := range m.Monitors {
		if old.Pending[key] != "" {
			return false, invalid("monitors."+key, "awaiting administrative retirement")
		}
		if t := old.Types[key]; t != "" && item["type"] != t {
			return false, invalid("monitors."+key+".type", "type is reserved; use a new ID")
		}
	}
	next, _ := json.Marshal(m)
	prev, _ := json.Marshal(old.Manifest)
	if bytes.Equal(next, prev) {
		stamp, _ := json.Marshal(now)
		if e := atomicFile(s.path("metadata", id), stamp, 0600); e != nil {
			return false, e
		}
		s.last[id] = now
		return false, nil
	}
	r := record{m, maps.Clone(old.Types), maps.Clone(old.Pending), now}
	if r.Types == nil {
		r.Types = map[string]string{}
	}
	if r.Pending == nil {
		r.Pending = map[string]string{}
	}
	for key, item := range m.Monitors {
		r.Types[key] = item["type"].(string)
	}
	// Missing checks keep executing until an operator pauses them. Include them
	// in the quota so cycling snapshots cannot create unlimited live checks.
	for key := range old.Manifest.Monitors {
		if _, ok := m.Monitors[key]; !ok && r.Pending[key] == "" {
			r.Pending[key] = "removed from snapshot"
		}
	}
	if len(m.Monitors) > 0 && len(m.Monitors)+len(r.Pending) > p.MaxMonitors {
		return false, invalid("monitors", "quota includes checks pending retirement")
	}
	if len(r.Types) > 10000 {
		return false, invalid("monitors", "namespace lifetime identity limit exceeded")
	}
	if e := s.save(id, r); e != nil {
		if errors.Is(e, errUncertain) {
			s.fenced = true
		}
		return false, e
	}
	s.records[id] = r
	s.last[id] = now
	s.dirty = true
	if e := s.render(); e != nil {
		slog.Error("generation pending", "error", e)
	}
	return true, nil
}
func (s *Store) render() error {
	out := map[string]Monitor{}
	for client, r := range s.records {
		for id, m := range r.Manifest.Monitors {
			if r.Pending[id] == "" {
				item := maps.Clone(m)
				item["description"] = "Managed by MagicKuma: " + client + "/" + id
				out[client+"."+id] = item
			}
		}
	}
	b, e := json.Marshal(out)
	if e != nil {
		return e
	}
	old, e := os.ReadFile(s.c.GeneratedFile)
	if e == nil && bytes.Equal(old, b) {
		s.dirty = false
		return nil
	}
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	if e = atomicFile(s.c.GeneratedFile, b, 0640); e != nil {
		return e
	}
	s.dirty = false
	return nil
}
func (s *Store) Retry() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dirty {
		return s.render()
	}
	return nil
}
func (s *Store) status(id string, now time.Time) Status {
	r := s.records[id]
	t := s.last[id]
	return Status{t, t.IsZero() || now.Sub(t) > s.c.stale, len(r.Manifest.Monitors), maps.Clone(r.Pending)}
}
func (s *Store) Status(id string, now time.Time) Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status(id, now)
}
func (s *Store) Ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dirty || s.fenced {
		return false
	}
	for _, r := range s.records {
		if len(r.Pending) > 0 {
			return false
		}
	}
	return true
}
func (s *Store) Counts() (declared, stale, pending int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.c.Clients {
		if s.status(id, time.Now()).Stale {
			stale++
		}
	}
	for _, r := range s.records {
		declared += len(r.Manifest.Monitors)
		pending += len(r.Pending)
	}
	return
}

// Admin runs with the receiver stopped and the same exclusive writer lock.
func (s *Store) Admin(action, id, key string) error {
	r, ok := s.records[id]
	if !ok {
		return errors.New("unknown persisted client")
	}
	switch action {
	case "decommission":
		if len(s.c.Clients[id].TokenHashFiles) > 0 {
			return errors.New("revoke client tokens before decommissioning")
		}
		for k := range r.Manifest.Monitors {
			r.Pending[k] = "client decommissioned"
		}
		r.Manifest.Monitors = map[string]Monitor{}
	case "ack-retired":
		if r.Pending[key] == "" {
			return errors.New("no retirement pending for this monitor")
		}
		delete(r.Pending, key)
		delete(r.Manifest.Monitors, key)
	default:
		return errors.New("unknown admin operation")
	}
	if e := s.save(id, r); e != nil {
		return e
	}
	s.records[id] = r
	s.dirty = true
	return s.render()
}
