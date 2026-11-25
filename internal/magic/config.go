package magic

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Listen        string            `json:"listen"`
	StateDir      string            `json:"stateDir"`
	GeneratedFile string            `json:"generatedFile"`
	StaleAfter    string            `json:"staleAfter"`
	Clients       map[string]Policy `json:"clients"`
	credentials   []credential
	stale         time.Duration
}

type credential struct {
	client string
	hash   [32]byte
}

func Load(path string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err = Decode(b, &c); err != nil {
		return c, err
	}
	if c.Listen == "" {
		c.Listen = "127.0.0.1:8085"
	}
	host, _, err := net.SplitHostPort(c.Listen)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		return c, errors.New("listen must be a loopback IP:port behind HTTPS")
	}
	if !filepath.IsAbs(c.StateDir) {
		return c, errors.New("stateDir must be absolute")
	}
	if c.GeneratedFile == "" {
		c.GeneratedFile = filepath.Join(c.StateDir, "generated", "snapshot.json")
	}
	if !filepath.IsAbs(c.GeneratedFile) {
		return c, errors.New("generatedFile must be absolute")
	}
	if err := validateExport(c.StateDir, c.GeneratedFile); err != nil {
		return c, err
	}
	if c.StaleAfter == "" {
		c.StaleAfter = "1h"
	}
	c.stale, err = time.ParseDuration(c.StaleAfter)
	if err != nil || c.stale <= 0 {
		return c, errors.New("invalid staleAfter")
	}
	seen := map[[32]byte]bool{}
	for id, p := range c.Clients {
		if !idPattern.MatchString(id) {
			return c, errors.New("invalid client ID")
		}
		if err := p.defaults(); err != nil {
			return c, fmt.Errorf("client %s: %w", id, err)
		}
		c.Clients[id] = p
		for _, f := range p.TokenHashFiles {
			if name, ok := strings.CutPrefix(f, "credential:"); ok {
				if dir := os.Getenv("CREDENTIALS_DIRECTORY"); dir != "" && filepath.Base(name) == name && name != "." && name != ".." && name != "" {
					f = filepath.Join(dir, name)
				} else {
					return c, errors.New("invalid systemd credential reference")
				}
			}
			b, err := os.ReadFile(f)
			if err != nil {
				return c, err
			}
			d, err := hex.DecodeString(strings.TrimSpace(string(b)))
			if err != nil || len(d) != 32 {
				return c, errors.New("invalid SHA-256 token hash file")
			}
			h := [32]byte(d)
			if seen[h] {
				return c, errors.New("duplicate credential hash")
			}
			seen[h] = true
			c.credentials = append(c.credentials, credential{id, h})
		}
	}
	return c, nil
}

func (c Config) Authenticate(token string) string {
	h := sha256.Sum256([]byte(token))
	id := ""
	for _, cred := range c.credentials {
		if subtle.ConstantTimeCompare(h[:], cred.hash[:]) == 1 {
			id = cred.client
		}
	}
	return id
}

func NewToken() (string, string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(b[:])
	h := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(h[:]), nil
}

func ValidateOffline(data []byte) error {
	m, err := Parse(data)
	if err != nil {
		return err
	}
	p := Policy{Unrestricted: true, MaxMonitors: 10000, MinInterval: 1}
	return p.Validate(m)
}

// validateExport keeps generated writes away from manifests, metadata and the
// writer lock. Open repeats this check after resolving directory symlinks.
func validateExport(stateDir, file string) error {
	rel, err := filepath.Rel(stateDir, file)
	if err != nil {
		return err
	}
	first, _, _ := strings.Cut(rel, string(os.PathSeparator))
	switch first {
	case ".", "manifests", "metadata", "writer.lock":
		return errors.New("generatedFile overlaps authoritative state")
	}
	return nil
}
