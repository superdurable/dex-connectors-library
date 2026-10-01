// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command customer-reply runs the Worker for the email customer-reply example Flow.
package main

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/superdurable/email"
	customerreply "github.com/superdurable/dex-connectors-library/connectors/superdurable/email/examples/customer-reply/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// rootCAFileEnvironmentVariable names a PEM bundle that replaces the system trust store, for a mail
// server with a private certificate authority or a local test server.
const rootCAFileEnvironmentVariable = "EMAIL_TLS_ROOT_CA_FILE"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("customer-reply stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	store, err := localconfig.LoadFromEnvironment()
	if err != nil {
		return err
	}
	options, err := connectionOptions()
	if err != nil {
		return err
	}
	connection, err := email.NewLocalConnection(store, customerreply.ConnectionName, options...)
	if err != nil {
		return err
	}
	registry, err := dex.NewRegistry([]dex.Flow{customerreply.NewFlow(connection)})
	if err != nil {
		return fmt.Errorf("register email customer-reply Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-email-customer-reply-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: environmentOr("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:8834"), FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS"),
	})
	if err != nil {
		return errors.Join(err, cache.Close())
	}
	slog.Info("email customer-reply worker starting", "connection", customerreply.ConnectionName)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	}
	return errors.Join(err, stopWorker(worker), cache.Close())
}

// connectionOptions trusts the PEM bundle named by EMAIL_TLS_ROOT_CA_FILE instead of the system store when it is set.
func connectionOptions() ([]email.Option, error) {
	path := os.Getenv(rootCAFileEnvironmentVariable)
	if path == "" {
		return nil, nil
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rootCAFileEnvironmentVariable, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(contents) {
		return nil, fmt.Errorf("%s holds no PEM certificate", rootCAFileEnvironmentVariable)
	}
	slog.Warn("mail server certificates are verified against a custom root CA file", "variable", rootCAFileEnvironmentVariable)
	return []email.Option{email.WithTLSRootCAs(pool)}, nil
}

func stopWorker(worker *dex.Worker) error {
	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return worker.Stop(stopCtx)
}

func environmentOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
