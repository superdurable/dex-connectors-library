// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command approval-decision runs the Worker for the Microsoft Excel approval-decision example Flow.
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

	"github.com/superdurable/dex-connectors-library/connectors/microsoft/excel"
	approvaldecision "github.com/superdurable/dex-connectors-library/connectors/microsoft/excel/examples/approval-decision/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const localProviderURLEnvironmentVariable = "MICROSOFT_EXCEL_LOCAL_PROVIDER_URL"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("approval-decision stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return err
	}
	connection, err := excel.NewProjectConnection(project, approvaldecision.ConnectionName, connectionOptions()...)
	if err != nil {
		return err
	}
	policyTable, err := loadOperationConfiguration[approvaldecision.TableConfiguration](project.Configuration, approvaldecision.PolicyTableConfigurationRef())
	if err != nil {
		return err
	}
	decisionTable, err := loadOperationConfiguration[approvaldecision.TableConfiguration](project.Configuration, approvaldecision.DecisionTableConfigurationRef())
	if err != nil {
		return err
	}
	summaryWorksheet, err := loadOperationConfiguration[approvaldecision.WorksheetConfiguration](project.Configuration, approvaldecision.SummaryWorksheetConfigurationRef())
	if err != nil {
		return err
	}
	registry, err := dex.NewRegistry([]dex.Flow{approvaldecision.NewFlow(connection, policyTable, decisionTable, summaryWorksheet)})
	if err != nil {
		return fmt.Errorf("register Microsoft Excel approval-decision Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-microsoft-excel-approval-decision-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: environmentOr("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:8863"), FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS"),
	})
	if err != nil {
		return errors.Join(err, cache.Close())
	}
	slog.Info("Microsoft Excel approval-decision worker starting", "connection", approvaldecision.ConnectionName,
		"policy_table", policyTable.Value.TableName, "decision_table", decisionTable.Value.TableName,
		"summary_worksheet", summaryWorksheet.Value.WorksheetName)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	}
	return errors.Join(err, stopWorker(worker), cache.Close())
}

// connectionOptions redirects Graph and token requests only when the local-verification variable is set.
func connectionOptions() []excel.Option {
	if providerURL := os.Getenv(localProviderURLEnvironmentVariable); providerURL != "" {
		slog.Warn("Microsoft Graph and token requests go to a local URL", "variable", localProviderURLEnvironmentVariable)
		return []excel.Option{excel.WithLocalProviderURL(providerURL)}
	}
	return nil
}

// loadOperationConfiguration treats a Step without saved picks as blank, which the first Step rejects.
func loadOperationConfiguration[T any](
	configuration projectconfig.Configuration,
	reference sdkgo.ConnectorConfigurationRef,
) (sdkgo.ConnectorLoadedConfiguration[T], error) {
	loaded, err := provider.LoadOperationConfiguration[T](configuration, reference)
	if errors.Is(err, projectconfig.ErrObjectNotFound) {
		return sdkgo.ConnectorLoadedConfiguration[T]{Reference: reference}, nil
	}
	return loaded, err
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
