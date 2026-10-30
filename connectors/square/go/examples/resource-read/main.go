// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

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

	"github.com/superdurable/dex-connectors-library/connectors/square"
	resourceread "github.com/superdurable/dex-connectors-library/connectors/square/examples/resource-read/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("square worker stopped", "error", err)
		os.Exit(1)
	}
}
func run(ctx context.Context) error {
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return err
	}
	connection, err := square.NewProjectConnection(project, resourceread.ConnectionName)
	if err != nil {
		return err
	}
	registry, err := dex.NewRegistry([]dex.Flow{resourceread.NewFlow(connection)})
	if err != nil {
		return fmt.Errorf("register square Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(os.TempDir(), "dex-square-blobs"), MaxBytes: 1 << 30})
	if err != nil {
		return err
	}
	workerOptions := dex.WorkerOptions{FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS")}
	worker, err := dex.NewWorker(registry, cache, workerOptions)
	if err != nil {
		return errors.Join(err, cache.Close())
	}
	result := make(chan error, 1)
	go func() { result <- worker.Start() }()
	select {
	case err = <-result:
	case <-ctx.Done():
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return errors.Join(err, worker.Stop(stopCtx), cache.Close())
}
