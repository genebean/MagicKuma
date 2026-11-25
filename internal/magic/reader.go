package magic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type reader struct {
	cmd  *exec.Cmd
	done chan error
	dir  string
}

func (r *reader) stop() {
	_ = r.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-r.done:
	case <-time.After(15 * time.Second):
		_ = r.cmd.Process.Kill()
		<-r.done
	}
	os.RemoveAll(r.dir)
}
func startReader(program, dir string) (*reader, error) {
	cmd := exec.Command(program)
	cmd.Env = os.Environ()
	for k, v := range map[string]string{"STATIC_MONITORS": dir, "ON_DELETE": "keep", "DOCKER__ENABLED": "false", "FILES__ENABLED": "true", "FILES__FOLLOW_SYMLINKS": "false", "KUBERNETES__ENABLED": "false", "DEFAULT_SETTINGS": "", "INSECURE_ENV_ACCESS": "false", "MIGRATE": "false", "TAG_NAME": "MagicKuma-managed"} {
		cmd.Env = append(cmd.Env, "AUTOKUMA__"+k+"="+v)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	r := &reader{cmd, make(chan error, 1), dir}
	go func() { r.done <- cmd.Wait() }()
	return r, nil
}
func materialize(work string, b []byte) (string, error) {
	var monitors map[string]json.RawMessage
	if e := Decode(b, &monitors); e != nil {
		return "", e
	}
	if monitors == nil {
		return "", errors.New("invalid aggregate")
	}
	dir, e := os.MkdirTemp(work, "input-")
	if e != nil {
		return "", e
	}
	for id, item := range monitors {
		client, key, ok := strings.Cut(id, ".")
		if !ok || !idPattern.MatchString(client) || !idPattern.MatchString(key) {
			os.RemoveAll(dir)
			return "", errors.New("invalid aggregate ID")
		}
		if e = os.WriteFile(filepath.Join(dir, id+".json"), item, 0600); e != nil {
			os.RemoveAll(dir)
			return "", e
		}
	}
	return dir, nil
}

// RunAutoKuma pins one directory per child. A failed read or materialization
// leaves a healthy reader on its previous complete input.
func RunAutoKuma(ctx context.Context, file, program, work string) error {
	if !filepath.IsAbs(work) || !filepath.IsAbs(file) {
		return errors.New("snapshot and work-dir must be absolute")
	}
	if os.Getenv("AUTOKUMA__DATA_PATH") == "" {
		return errors.New("set AUTOKUMA__DATA_PATH to a dedicated persistent directory")
	}
	if err := os.MkdirAll(work, 0700); err != nil {
		return err
	}
	lock, e := os.OpenFile(filepath.Join(work, "reader.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer lock.Close()
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		return fmt.Errorf("reader lock: %w", e)
	}
	var active *reader
	var previous []byte
	defer func() {
		if active != nil {
			active.stop()
		}
	}()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if active != nil {
			select {
			case e := <-active.done:
				os.RemoveAll(active.dir)
				active = nil
				return fmt.Errorf("AutoKuma exited: %v", e)
			default:
			}
		}
		b, e := os.ReadFile(file)
		if e == nil && (active == nil || !bytes.Equal(previous, b)) {
			var dir string
			dir, e = materialize(work, b)
			if e == nil {
				if active != nil {
					active.stop()
					active = nil
				}
				active, e = startReader(program, dir)
				if e != nil {
					os.RemoveAll(dir)
					return e
				}
				previous = b
				slog.Info("started reconciler with complete snapshot")
			}
		}
		if e != nil {
			slog.Error("snapshot unavailable", "error", e)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
