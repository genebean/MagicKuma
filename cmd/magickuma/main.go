package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/genebean/magickuma/internal/magic"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const usage = `magickuma - Distributed, declarative monitoring for Uptime Kuma

  serve          -config FILE
  publish        -url HTTPS_ORIGIN -token-file FILE -manifest FILE
  validate       -manifest FILE
  status         -url HTTPS_ORIGIN -token-file FILE
  token generate -token-file NEW_FILE -hash-file NEW_FILE
  inspect        -config FILE -client ID                      (server stopped)
  decommission   -config FILE -client ID                      (server stopped)
  ack-retired    -config FILE -client ID -monitor ID          (server stopped)
  run-autokuma   -snapshot FILE -program AUTOKUMA -work-dir DIR
  version

Exit 0 indicates success, 1 an operational or validation error, 2 a usage error.
`

type usageError string

func (e usageError) Error() string { return string(e) }
func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, err)
		var u usageError
		if errors.As(err, &u) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageError(usage)
	}
	cmd := args[0]
	args = args[1:]
	if cmd == "help" || cmd == "--help" || cmd == "-h" {
		fmt.Print(usage)
		return nil
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	var conf, origin, token, manifest, hash, client, monitor, snapshot, program, work string
	opt := func(v *string, key, desc string) { fs.StringVar(v, key, "", desc) }
	switch cmd {
	case "version":
		fmt.Println(magic.Version)
		return nil
	case "serve", "inspect", "decommission", "ack-retired":
		opt(&conf, "config", "native server JSON")
		if cmd != "serve" {
			opt(&client, "client", "authenticated namespace")
		}
		if cmd == "ack-retired" {
			opt(&monitor, "monitor", "local ID already paused or deleted in Kuma")
		}
	case "publish", "status":
		opt(&origin, "url", "HTTPS origin")
		opt(&token, "token-file", "raw bearer token file")
		if cmd == "publish" {
			opt(&manifest, "manifest", "desired-state JSON file")
		}
	case "validate":
		opt(&manifest, "manifest", "desired-state JSON file")
	case "token":
		if len(args) == 0 || args[0] != "generate" {
			return usageError(usage)
		}
		args = args[1:]
		opt(&token, "token-file", "new private token file")
		opt(&hash, "hash-file", "new server hash file")
	case "run-autokuma":
		opt(&snapshot, "snapshot", "path to generated snapshot")
		opt(&program, "program", "AutoKuma executable")
		opt(&work, "work-dir", "private directory for immutable process inputs")
	default:
		return usageError(usage)
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return usageError(err.Error())
	}
	if fs.NArg() != 0 {
		return usageError(usage)
	}
	missing := ""
	fs.VisitAll(func(f *flag.Flag) {
		if f.Value.String() == "" {
			missing = f.Name
		}
	})
	if missing != "" {
		return usageError("missing -" + missing)
	}
	switch cmd {
	case "token":
		return generate(token, hash)
	case "run-autokuma":
		return magic.RunAutoKuma(ctx, snapshot, program, work)
	case "validate", "publish", "status":
		var data []byte
		var err error
		if cmd != "status" {
			f, e := os.Open(manifest)
			if e != nil {
				return e
			}
			defer f.Close()
			data, err = io.ReadAll(io.LimitReader(f, magic.MaxBody+1))
			if err != nil {
				return err
			}
			if err = magic.ValidateOffline(data); err != nil {
				return err
			}
		}
		if cmd == "validate" {
			return nil
		}
		return request(ctx, cmd, origin, token, data)
	}
	c, err := magic.Load(conf)
	if err != nil {
		return err
	}
	s, err := magic.Open(c)
	if err != nil {
		return err
	}
	defer s.Close()
	if cmd == "inspect" {
		return json.NewEncoder(os.Stdout).Encode(s.Status(client, time.Now().UTC()))
	}
	if cmd != "serve" {
		return s.Admin(cmd, client, monitor)
	}
	a := magic.API{Config: c, Store: s}
	server := http.Server{Addr: c.Listen, Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			return server.Shutdown(shutdown)
		case err := <-done:
			return err
		case <-ticker.C:
			a.Retry()
		}
	}
}
func request(ctx context.Context, cmd, origin, tokenFile string, data []byte) error {
	u, e := url.Parse(origin)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("expected HTTPS origin")
	}
	method := "PUT"
	u.Path = "/api/v1/monitors"
	if cmd == "status" {
		method = "GET"
		u.Path = "/api/v1/status"
	}
	token, e := os.ReadFile(tokenFile)
	if e != nil {
		return e
	}
	req, e := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(data))
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	req.Header.Set("Content-Type", "application/json")
	h := http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := h.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var problem struct{ Error, Message, Path string }
		_ = json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&problem)
		return fmt.Errorf("HTTP %d %s %s %s", resp.StatusCode, problem.Error, problem.Path, problem.Message)
	}
	_, e = io.Copy(os.Stdout, io.LimitReader(resp.Body, 1<<20))
	return e
}
func newFile(path, value string) error {
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	_, e = fmt.Fprintln(f, value)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		os.Remove(path)
	}
	return e
}
func generate(token, hash string) error {
	t, h, e := magic.NewToken()
	if e != nil {
		return e
	}
	if e = newFile(token, t); e != nil {
		return e
	}
	if e = newFile(hash, h); e != nil {
		os.Remove(token)
	}
	return e
}
