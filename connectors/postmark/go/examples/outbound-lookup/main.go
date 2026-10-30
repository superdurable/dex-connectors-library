// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command outbound-lookup runs the Postmark example Worker.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/postmark/go"
	example "github.com/superdurable/dex-connectors-library/connectors/postmark/go/examples/outbound-lookup/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("outbound-lookup stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return err
	}
	connection, err := postmark.NewProjectConnection(project, example.ConnectionName)
	if err != nil {
		return err
	}
	registry, err := dex.NewRegistry([]dex.Flow{example.NewFlow(connection)})
	if err != nil {
		return fmt.Errorf("register example Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(os.TempDir(), "dex-postmark-outbound-lookup-blobs"), MaxBytes: 1 << 30})
	if err != nil {
		return err
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS")})
	if err != nil {
		return errors.Join(err, cache.Close())
	}
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	}
	stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return errors.Join(err, worker.Stop(stopContext), cache.Close())
}
