//go:build pprof

package main

// Build with `-tags pprof` to expose Go's profiler over localhost. It is behind
// a build tag rather than a runtime flag so a release binary cannot be talked
// into opening a port: the handlers are not compiled in at all.
//
//	go build -tags pprof -o adbq-pprof .
//	go tool pprof -http=: 'http://127.0.0.1:6060/debug/pprof/profile?seconds=30'
//
// Bound to 127.0.0.1 on purpose — pprof exposes goroutine stacks and heap
// contents, which must not reach the network.

import (
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"time"
)

const pprofAddr = "127.0.0.1:6060"

func init() {
	go func() {
		srv := &http.Server{
			Addr:              pprofAddr,
			ReadHeaderTimeout: 5 * time.Second,
		}
		slog.Info("pprof listening", "addr", pprofAddr)
		if err := srv.ListenAndServe(); err != nil {
			slog.Warn("pprof server stopped", "err", err)
		}
	}()
}
