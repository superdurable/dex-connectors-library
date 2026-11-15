// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command invoice-changed runs the Worker for the Xero invoiceChanged example Flow and polls the
// organisation's invoices, starting one Flow per invoice or bill created or changed.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/xero/go"
	invoicechanged "github.com/superdurable/dex-connectors-library/connectors/xero/go/examples/invoice-changed/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func main() {
	logger := newLogger(os.Stderr, os.Getenv("LOG_LEVEL"))
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("invoice-changed stopped", "error", err)
		os.Exit(1)
	}
}

// newLogger writes text records to output at LOG_LEVEL (debug, info, warn, or error; info by default).
func newLogger(output io.Writer, levelName string) *slog.Logger {
	level := slog.LevelInfo
	isInvalid := strings.TrimSpace(levelName) != "" && level.UnmarshalText([]byte(strings.TrimSpace(levelName))) != nil
	logger := slog.New(slog.NewTextHandler(output, &slog.HandlerOptions{Level: level}))
	if isInvalid {
		logger.Warn("LOG_LEVEL is not debug, info, warn, or error; using info", "log_level", levelName)
	}
	return logger
}

func run(ctx context.Context, logger *slog.Logger) error {
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return err
	}
	options := connectionOptions(logger)
	connection, err := xero.NewProjectConnection(project, invoicechanged.ConnectionName, options...)
	if err != nil {
		return err
	}
	flow := invoicechanged.NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	if err != nil {
		return fmt.Errorf("register Xero invoice change Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-xero-invoice-changed-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	address := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: address})
	if err != nil {
		return errors.Join(err, cache.Close())
	}
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: address})
	if err != nil {
		return errors.Join(err, stopWorker(worker), cache.Close())
	}
	// Each change is recorded in the binding's project Trigger inbox before the poll position moves past it.
	triggerRunner, err := xero.NewProjectInvoiceTriggerRunner(project, invoicechanged.ConnectionName, xero.ProjectInvoiceTriggerRunnerConfig{
		InvoiceChangedRoutes: []xero.ProjectInvoiceChangedTriggerRoute{{
			BindingName: invoicechanged.InvoiceChangedTriggerBinding,
			Target: sdkgo.NewDexFlowTriggerTarget(client, flow, invoicechanged.AcceptEveryInvoiceChange, invoicechanged.ResolveFlowID,
				invoicechanged.MapToFlowInput, sdkgo.WithTriggerLogger(logger)),
		}},
	}, options...)
	if err != nil {
		return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
	}
	logger.Info("Xero invoice change worker starting", "connection", invoicechanged.ConnectionName, "binding", invoicechanged.InvoiceChangedTriggerBinding)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	triggerResult := make(chan error, 1)
	triggerContext, cancelTrigger := context.WithCancel(ctx)
	defer cancelTrigger()
	go func() { triggerResult <- triggerRunner.Run(triggerContext) }()
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	case err = <-triggerResult:
		// Refill the channel so the shutdown read below does not block.
		triggerResult <- nil
	}
	cancelTrigger()
	if triggerErr := <-triggerResult; triggerErr != nil && !errors.Is(triggerErr, context.Canceled) {
		err = errors.Join(err, triggerErr)
	}
	return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
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

// localProviderURLEnvironmentVariable sends Xero requests to a local fake, for local verification only.
const localProviderURLEnvironmentVariable = "XERO_LOCAL_PROVIDER_URL"

func connectionOptions(logger *slog.Logger) []xero.Option {
	options := []xero.Option{xero.WithLogger(logger)}
	if providerURL := os.Getenv(localProviderURLEnvironmentVariable); providerURL != "" {
		logger.Warn("Xero API and token requests go to a local URL", "variable", localProviderURLEnvironmentVariable)
		options = append(options, xero.WithLocalProviderURL(providerURL))
	}
	return options
}
