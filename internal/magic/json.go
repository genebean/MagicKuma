// Package magic implements the authenticated snapshot protocol.
package magic

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

const MaxBody = 1 << 20

// Decode enforces unique keys and a depth limit before decoding into the destination.
// Duplicate keys are ambiguous across clients, policy evaluators and reconcilers.
func Decode(data []byte, v any) error {
	if !utf8.Valid(data) {
		return errors.New("invalid UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := checkValue(d, 0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("expected one JSON value")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	d.DisallowUnknownFields()
	return d.Decode(v)
}

func checkValue(d *json.Decoder, depth int) error {
	if depth > 16 {
		return errors.New("JSON exceeds depth limit")
	}
	tok, err := d.Token()
	if err != nil {
		return err
	}
	switch tok {
	case json.Delim('{'):
		seen := map[string]bool{}
		for d.More() {
			k, err := d.Token()
			if err != nil {
				return err
			}
			key, ok := k.(string)
			if !ok || seen[key] {
				return errors.New("invalid or duplicate property")
			}
			seen[key] = true
			if err = checkValue(d, depth+1); err != nil {
				return err
			}
		}
	case json.Delim('['):
		for d.More() {
			if err = checkValue(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return nil
	}
	_, err = d.Token()
	return err
}

type Problem struct {
	Path   string
	Reason string
}

func (e *Problem) Error() string        { return fmt.Sprintf("%s: %s", e.Path, e.Reason) }
func invalid(path, reason string) error { return &Problem{path, reason} }

type Monitor map[string]any
type Manifest struct {
	SchemaVersion int                `json:"schemaVersion"`
	Monitors      map[string]Monitor `json:"monitors"`
}

func Parse(data []byte) (Manifest, error) {
	var m Manifest
	if len(data) > MaxBody {
		return m, errors.New("manifest exceeds 1 MiB")
	}
	var root map[string]json.RawMessage
	if err := Decode(data, &root); err != nil {
		return m, err
	}
	if len(root) != 2 || root["schemaVersion"] == nil || root["monitors"] == nil {
		return m, invalid("$", "expected schemaVersion and monitors")
	}
	if err := json.Unmarshal(root["schemaVersion"], &m.SchemaVersion); err != nil {
		return m, err
	}
	d := json.NewDecoder(bytes.NewReader(root["monitors"]))
	d.UseNumber()
	if err := d.Decode(&m.Monitors); err != nil {
		return m, err
	}
	if m.SchemaVersion != 1 || m.Monitors == nil {
		return m, invalid("$", "expected schemaVersion 1 and a monitor map")
	}
	return m, nil
}
