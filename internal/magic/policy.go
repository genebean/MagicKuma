package magic

import (
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
var dnsLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
var queryPattern = regexp.MustCompile(`^(\$\.)?[A-Za-z_][A-Za-z_0-9]*(\.[A-Za-z_][A-Za-z_0-9]*)*$`)
var types = []string{"http", "keyword", "json-query", "port", "ping"}

type Policy struct {
	TokenHashFiles      []string `json:"tokenHashFiles"`
	AllowedDomains      []string `json:"allowedDomains"`
	AllowedNetworks     []string `json:"allowedNetworks"`
	AllowedPorts        []int    `json:"allowedPorts"`
	AllowedMonitorTypes []string `json:"allowedMonitorTypes"`
	Unrestricted        bool     `json:"unrestricted"`
	MaxMonitors         int      `json:"maxMonitors"`
	MinInterval         int      `json:"minInterval"`
}

func (p *Policy) defaults() error {
	if p.MaxMonitors == 0 {
		p.MaxMonitors = 100
	}
	if p.MinInterval == 0 {
		p.MinInterval = 60
	}
	if p.MaxMonitors < 1 || p.MaxMonitors > 10000 || p.MinInterval < 1 || p.MinInterval > 86400 {
		return errors.New("invalid maxMonitors or minInterval")
	}
	for _, typ := range p.AllowedMonitorTypes {
		if !slices.Contains(types, typ) {
			return errors.New("unsupported allowedMonitorTypes entry")
		}
	}
	for _, s := range p.AllowedPorts {
		if s < 1 || s > 65535 {
			return errors.New("invalid allowedPorts entry")
		}
	}
	for i, s := range p.AllowedDomains {
		s = strings.TrimSuffix(strings.ToLower(s), ".")
		if !validDNS(strings.TrimPrefix(s, "*.")) {
			return errors.New("invalid allowedDomains entry")
		}
		p.AllowedDomains[i] = s
	}
	for _, s := range p.AllowedNetworks {
		n, e := netip.ParsePrefix(s)
		if e != nil || n.Addr().Is4In6() || n != n.Masked() {
			return errors.New("expected canonical CIDR in allowedNetworks")
		}
	}
	return nil
}

func validDNS(s string) bool {
	parts := strings.Split(s, ".")
	if len(s) > 253 || len(parts) < 2 {
		return false
	}
	for _, label := range parts {
		if !dnsLabel.MatchString(label) {
			return false
		}
	}
	// WHATWG treats a numeric or hex final label as an IPv4 candidate.
	last := parts[len(parts)-1]
	return strings.ContainsAny(last, "abcdefghijklmnopqrstuvwxyz") && !strings.HasPrefix(last, "0x")
}

func (p Policy) host(input string) (string, error) {
	host := strings.TrimSuffix(strings.ToLower(input), ".")
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.Zone() != "" {
			return "", errors.New("scoped addresses are unsupported")
		}
		ip = ip.Unmap()
		if p.Unrestricted {
			return ip.String(), nil
		}
		for _, s := range p.AllowedNetworks {
			n, err := netip.ParsePrefix(s)
			if err == nil && n.Contains(ip) {
				return ip.String(), nil
			}
		}
	} else {
		if !validDNS(host) {
			return "", errors.New("invalid DNS name or ambiguous IP address")
		}
		if p.Unrestricted {
			return host, nil
		}
		_, suffix, _ := strings.Cut(host, ".")
		for _, s := range p.AllowedDomains {
			if s == host || s == "*."+suffix {
				return host, nil
			}
		}
	}
	return "", errors.New("destination is outside client policy")
}

func (p Policy) permitsPort(port int) bool {
	return port > 0 && port <= 65535 && (p.Unrestricted || slices.Contains(p.AllowedPorts, port))
}

func (p Policy) normalizeURL(s string) (string, error) {
	u, e := url.Parse(s)
	if e != nil || u.Opaque != "" || u.User != nil || u.Host == "" || strings.ContainsAny(s, "\\# \t\n\r") || (u.Scheme != "https" && u.Scheme != "http") {
		return "", errors.New("expected an absolute HTTP(S) URL without credentials, fragments or backslashes")
	}
	if strings.HasSuffix(u.Host, ":") || strings.Contains(u.Host, "%") {
		return "", errors.New("invalid authority")
	}
	h, e := p.host(u.Hostname())
	if e != nil {
		return "", e
	}
	port := 80
	if u.Scheme == "https" {
		port = 443
	}
	if explicit := u.Port(); explicit != "" {
		port, e = strconv.Atoi(explicit)
		if e != nil {
			return "", errors.New("invalid port")
		}
	}
	if !p.permitsPort(port) {
		return "", errors.New("port outside client policy")
	}
	if (port == 443 && u.Scheme == "https") || (port == 80 && u.Scheme == "http") {
		u.Host = h
		if strings.Contains(h, ":") {
			u.Host = "[" + h + "]"
		}
	} else {
		u.Host = net.JoinHostPort(h, strconv.Itoa(port))
	}
	return u.String(), nil
}

func number(v any) (int, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	i, e := strconv.Atoi(string(n))
	return i, e == nil
}

func safeString(s string) bool {
	if s == "" || len(s) > 2048 || strings.HasPrefix(s, "\"") || strings.Contains(s, "{{") || strings.Contains(s, "{%") || strings.Contains(s, "{#") {
		return false
	}
	return strings.IndexFunc(s, unicode.IsControl) < 0
}

func (p Policy) Validate(m Manifest) error {
	if len(m.Monitors) > p.MaxMonitors {
		return invalid("monitors", "monitor quota exceeded")
	}
	for id, item := range m.Monitors {
		if !idPattern.MatchString(id) {
			return invalid("monitors", "invalid local ID")
		}
		if err := p.monitor(id, item); err != nil {
			return err
		}
	}
	return nil
}

// Normalize in place. The resulting fields are exactly those sent to the static reader.
func (p Policy) monitor(id string, m Monitor) error {
	problem := func(k, why string) error { return invalid("monitors."+id+"."+k, why) }
	typ, _ := m["type"].(string)
	if !slices.Contains(types, typ) || (!p.Unrestricted && !slices.Contains(p.AllowedMonitorTypes, typ)) {
		return problem("type", "unsupported or unauthorized monitor type")
	}
	httpType := typ == "http" || typ == "keyword" || typ == "json-query"
	for k, v := range m {
		if s, ok := v.(string); ok && !safeString(s) {
			return problem(k, "unsafe or empty string")
		}
		switch k {
		case "type", "name", "interval":
		case "hostname":
			if httpType {
				return problem(k, "unsupported property")
			}
		case "port":
			if typ != "port" {
				return problem(k, "unsupported property")
			}
		case "url", "max_redirects":
			if !httpType {
				return problem(k, "unsupported property")
			}
		case "keyword":
			if typ != "keyword" {
				return problem(k, "unsupported property")
			}
		case "expected_value", "json_path", "json_path_operator":
			if typ != "json-query" {
				return problem(k, "unsupported property")
			}
		default:
			return problem(k, "unsupported property")
		}
	}
	if _, ok := m["name"].(string); !ok {
		return problem("name", "expected display name")
	}
	if _, ok := m["interval"]; !ok {
		m["interval"] = json.Number("60")
	}
	if i, ok := number(m["interval"]); !ok || i < p.MinInterval || i > 86400 {
		return problem("interval", "outside policy bounds")
	}
	if httpType {
		s, _ := m["url"].(string)
		u, err := p.normalizeURL(s)
		if err != nil {
			return problem("url", err.Error())
		}
		m["url"] = u
		if v, ok := m["max_redirects"]; ok {
			if n, ok := number(v); !ok || n != 0 {
				return problem("max_redirects", "redirects must be disabled")
			}
		}
		m["max_redirects"] = json.Number("0")
	} else {
		s, _ := m["hostname"].(string)
		h, e := p.host(s)
		if e != nil {
			return problem("hostname", e.Error())
		}
		m["hostname"] = h
		if typ == "port" {
			n, ok := number(m["port"])
			if !ok || !p.permitsPort(n) {
				return problem("port", "invalid or unauthorized port")
			}
		}
	}
	if typ == "keyword" {
		if _, ok := m["keyword"].(string); !ok {
			return problem("keyword", "expected literal text")
		}
	}
	if typ == "json-query" {
		s, _ := m["json_path"].(string)
		if !queryPattern.MatchString(s) {
			return problem("json_path", "expected a simple JSONata field path")
		}
		if op, ok := m["json_path_operator"]; ok {
			if !slices.Contains([]any{"==", "!=", ">", ">=", "<", "<=", "contains"}, op) {
				return problem("json_path_operator", "unsupported comparison")
			}
		}
		switch m["expected_value"].(type) {
		case string, json.Number:
		default:
			return problem("expected_value", "expected a string or number")
		}
	}
	return nil
}
