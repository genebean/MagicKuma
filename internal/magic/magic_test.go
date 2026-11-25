package magic

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixture(t *testing.T) (Config, string, string) {
	t.Helper()
	dir := t.TempDir()
	var files []string
	var tokens []string
	for i := 0; i < 3; i++ {
		token, h, e := NewToken()
		if e != nil {
			t.Fatal(e)
		}
		if len(token) != 43 || h != fmt.Sprintf("%x", sha256.Sum256([]byte(token))) {
			t.Fatal("token encoding")
		}
		b, _ := hex.DecodeString(h)
		if len(b) != 32 {
			t.Fatal("hash size")
		}
		f := filepath.Join(dir, fmt.Sprintf("hash%d", i))
		if e = os.WriteFile(f, []byte(h), 0600); e != nil {
			t.Fatal(e)
		}
		files = append(files, f)
		tokens = append(tokens, token)
	}
	p := Policy{TokenHashFiles: files[:2], AllowedMonitorTypes: types, AllowedDomains: []string{"Web.example.org.", "*.apps.example.org"}, AllowedNetworks: []string{"192.0.2.0/24", "2001:db8::/48"}, AllowedPorts: []int{443, 22}, MaxMonitors: 50, MinInterval: 60}
	q := p
	q.TokenHashFiles = files[2:]
	c := Config{StateDir: filepath.Join(dir, "state"), Clients: map[string]Policy{"web": p, "other": q}}
	data, _ := json.Marshal(c)
	f := filepath.Join(dir, "config")
	os.WriteFile(f, data, 0600)
	c, e := Load(f)
	if e != nil {
		t.Fatal(e)
	}
	if c.Authenticate(tokens[0]) != "web" || c.Authenticate(tokens[1]) != "web" || c.Authenticate(tokens[2]) != "other" || c.Authenticate("incorrect") != "" {
		t.Fatal("rotation or isolation failed")
	}
	return c, tokens[0], f
}

const httpMonitor = `{"type":"http","name":"Site","url":"https://web.example.org"}`

func manifest(t *testing.T, items string) Manifest {
	t.Helper()
	m, e := Parse([]byte(`{"schemaVersion":1,"monitors":{` + items + `}}`))
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func TestDestinationPolicy(t *testing.T) {
	c, _, _ := fixture(t)
	p := c.Clients["web"]
	for _, h := range []string{"WEB.Example.org.", "a.apps.example.org", "192.0.2.42", "::ffff:192.0.2.42", "2001:db8::7"} {
		if _, e := p.host(h); e != nil {
			t.Errorf("%q: %v", h, e)
		}
	}
	for _, h := range []string{"apps.example.org", "evilweb.example.org", "a.web.example.org", "x.y.apps.example.org", "fakeapps.example.org", "127.1", "2130706433", "0177.0.0.1", "0x7f000001", "::ffff:127.0.0.1", "2001:db9::7", "fe80::1%eth0", "169.254.169.254", "127.0.0.1", "web.example.org.."} {
		if _, e := p.host(h); e == nil {
			t.Errorf("permitted %q", h)
		}
	}
	for _, u := range []string{"https://x@web.example.org", "http://web.example.org", "https://web.example.org:81", "https://web.example.org\\@other.example", "https://web.example.org#x", "file:///tmp/x", "https://web.example.org:", "https://web%2eexample.org"} {
		if _, e := p.normalizeURL(u); e == nil {
			t.Errorf("permitted %q", u)
		}
	}
	if s, e := p.normalizeURL("https://WEB.example.org.:443/health"); e != nil || s != "https://web.example.org/health" {
		t.Fatal(s, e)
	}
}
func TestFailClosedFields(t *testing.T) {
	c, _, _ := fixture(t)
	for _, fields := range []string{`"type":"database"`, `"headers":{}`, `"proxy_id":1`, `"method":"POST"`, `"body":"a"`, `"notificationIDList":{}`, `"max_redirects":2`, `"interval":1`, `"retry_interval":1`, `"proxyId":null`, `"description":"x"`, `"name":"{{ value }}"`, `"name":"\"raw\""`, `"name":"{% block %}"`, `"name":"a\nb"`, `"url":"https://other.example"`} {
		var override Monitor
		Decode([]byte("{"+fields+"}"), &override)
		m := manifest(t, `"site":`+httpMonitor)
		for k, v := range override {
			m.Monitors["site"][k] = v
		}
		if e := c.Clients["web"].Validate(m); e == nil {
			t.Errorf("allowed %s", fields)
		}
	}
	for _, body := range []string{`{}`, `{"schemaVersion":1,"monitors":null}`, `{"SchemaVersion":1,"monitors":{}}`, `{"schemaVersion":2,"monitors":{}}`, `{"schemaVersion":1,"monitors":{},"extra":3}`, `{"schemaVersion":1,"monitors":{},"monitors":{}}`, `{"schemaVersion":1,"monitors":{}}{}`} {
		if _, e := Parse([]byte(body)); e == nil {
			t.Fatal(body)
		}
	}
	if e := c.Clients["web"].Validate(manifest(t, `"../site":`+httpMonitor)); e == nil {
		t.Fatal("accepted traversal")
	}
}
func TestLifecycle(t *testing.T) {
	c, _, f := fixture(t)
	s, e := Open(c)
	if e != nil {
		t.Fatal(e)
	}
	if s2, e := Open(c); e == nil {
		s2.Close()
		t.Fatal("second writer")
	}
	now := time.Now().UTC()
	put := func(id, items string, changed bool) {
		t.Helper()
		b, e := s.Publish(id, manifest(t, items), now)
		if e != nil || b != changed {
			t.Fatalf("publish %s: %t %v", id, b, e)
		}
	}
	input := `"site":` + httpMonitor
	put("web", input, true)
	put("other", input, true)
	before, _ := os.Stat(c.GeneratedFile)
	desired, _ := os.Stat(s.path("manifests", "web"))
	now = now.Add(time.Minute)
	put("web", input, false)
	after, _ := os.Stat(c.GeneratedFile)
	desired2, _ := os.Stat(s.path("manifests", "web"))
	if !os.SameFile(before, after) || !os.SameFile(desired, desired2) || !s.Status("web", now).LastPublication.Equal(now) {
		t.Fatal("idempotence")
	}
	if _, e := s.Publish("web", manifest(t, input+`,"bad":{"type":"ping","hostname":"else.example","name":"bad"}`), now); e == nil {
		t.Fatal("partial acceptance")
	}
	if s.Status("web", now).Declared != 1 || !s.Status("web", now.Add(2*time.Hour)).Stale {
		t.Fatal("stale or atomic rejection")
	}
	put("web", "", true)
	b, _ := os.ReadFile(c.GeneratedFile)
	if strings.Contains(string(b), "web.site") || !strings.Contains(string(b), "other.site") || s.Ready() {
		t.Fatal("removal isolation or pending alarm")
	}
	if e = s.Admin("ack-retired", "web", "site"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Publish("web", manifest(t, `"site":{"type":"ping","name":"new","hostname":"web.example.org"}`), now); e == nil {
		t.Fatal("type reservation lost")
	}
	put("web", input, true)
	s.Close()
	os.Remove(c.GeneratedFile)
	// Restart with tighter policy and revoked tokens. Persist the retirement so
	// restoring the old policy cannot silently re-enable an unreviewed check.
	p := c.Clients["web"]
	p.AllowedDomains = nil
	p.TokenHashFiles = nil
	c.Clients["web"] = p
	data, _ := json.Marshal(c)
	os.WriteFile(f, data, 0600)
	c, e = Load(f)
	if e != nil {
		t.Fatal(e)
	}
	s, e = Open(c)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	b, _ = os.ReadFile(c.GeneratedFile)
	if strings.Contains(string(b), "web.site") || !strings.Contains(string(b), "other.site") || len(s.Status("web", now).Pending) != 1 {
		t.Fatal("restart did not quarantine")
	}
	if e = s.Admin("decommission", "other", ""); e == nil {
		t.Fatal("decommissioned a live credential")
	}
	if e = s.Admin("decommission", "web", ""); e != nil {
		t.Fatal(e)
	}
}
func TestConcurrentHTTPS(t *testing.T) {
	c, token, _ := fixture(t)
	s, e := Open(c)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	api := API{Config: c, Store: s}
	server := httptest.NewTLSServer(api.Handler())
	defer server.Close()
	put := func(body, cred, media string) int {
		req, _ := http.NewRequest("PUT", server.URL+"/api/v1/monitors", strings.NewReader(body))
		req.Header.Set("Content-Type", media)
		req.Header.Set("Authorization", "Bearer "+cred)
		r, e := server.Client().Do(req)
		if e != nil {
			t.Error(e)
			return 0
		}
		defer r.Body.Close()
		return r.StatusCode
	}
	body := `{"schemaVersion":1,"monitors":{"site":` + httpMonitor + `}}`
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if code := put(body, token, "application/json"); code != 200 {
				t.Error(code)
			}
		}()
	}
	wg.Wait()
	if put(body, "wrong", "application/json") != 401 || put(body, token, "text/plain") != 415 || put(strings.Repeat(" ", MaxBody+1), token, "application/json") != 413 || put("", token, "application/json") != 400 || put(strings.Replace(body, "web.example.org", "bad.example.org", 1), token, "application/json") != 422 {
		t.Fatal("invalid response status")
	}
}
func TestRecovery(t *testing.T) {
	c, _, _ := fixture(t)
	s, e := Open(c)
	if e != nil {
		t.Fatal(e)
	}
	// Simulate disk failure in derived output after the authoritative commit.
	original := s.c.GeneratedFile
	s.c.GeneratedFile = filepath.Join(t.TempDir(), "missing", "output")
	_, e = s.Publish("web", manifest(t, `"s":`+httpMonitor), time.Now())
	if e != nil || s.Ready() {
		t.Fatal("lost acceptance or reported generation success")
	}
	s.c.GeneratedFile = original
	if e = s.Retry(); e != nil || !s.Ready() {
		t.Fatal(e)
	}
	s.Close()
	b, _ := os.ReadFile(original)
	os.WriteFile(s.path("manifests", "web"), []byte("{"), 0600)
	if _, e = Open(c); e == nil {
		t.Fatal("accepted corrupt state")
	}
	after, _ := os.ReadFile(original)
	if !bytes.Equal(b, after) {
		t.Fatal("corruption discarded last generated snapshot")
	}
}
