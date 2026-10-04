// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command incident-acknowledgement runs the Worker for the Microsoft Teams incident acknowledgement example Flow.
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

	"github.com/superdurable/dex-connectors-library/connectors/microsoft/teams"
	incidentacknowledgement "github.com/superdurable/dex-connectors-library/connectors/microsoft/teams/examples/incident-acknowledgement/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("incident-acknowledgement stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return err
	}
	connection, err := teams.NewProjectConnection(project, incidentacknowledgement.ConnectionName)
	if err != nil {
		return err
	}
	channel, err := loadChannelSelection(project.Configuration)
	if err != nil {
		return err
	}
	escalation, err := loadEscalationChatSelection(project.Configuration)
	if err != nil {
		return err
	}
	policy := incidentacknowledgement.DefaultAcknowledgementPolicy()
	registry, err := dex.NewRegistry([]dex.Flow{incidentacknowledgement.NewFlow(connection, channel, escalation, &policy)})
	if err != nil {
		return fmt.Errorf("register Microsoft Teams incident acknowledgement Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-teams-incident-acknowledgement-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: environmentOr("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:8883"), FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS"),
	})
	if err != nil {
		return errors.Join(err, cache.Close())
	}
	slog.Info("Microsoft Teams incident acknowledgement worker starting",
		"connection", incidentacknowledgement.ConnectionName, "escalation_chat_selected", escalation.ChatID != "")
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	}
	return errors.Join(err, stopWorker(worker), cache.Close())
}

// loadChannelSelection reads the picked team and channel once at startup; the Worker needs both.
func loadChannelSelection(configuration projectconfig.Configuration) (incidentacknowledgement.ChannelSelection, error) {
	loaded, err := provider.LoadOperationConfiguration[incidentacknowledgement.ChannelSelection](configuration, incidentacknowledgement.ChannelSelectionConfigurationRef())
	if errors.Is(err, projectconfig.ErrObjectNotFound) {
		return incidentacknowledgement.ChannelSelection{}, errors.New("save the Incident team and Incident channel pickers on the PostIncidentUpdate Step in Dex Web, then start the Worker")
	}
	if err != nil {
		return incidentacknowledgement.ChannelSelection{}, err
	}
	if loaded.Value.TeamID == "" || loaded.Value.ChannelID == "" {
		return incidentacknowledgement.ChannelSelection{}, errors.New("the saved Incident team or Incident channel is blank; pick both in Dex Web")
	}
	return loaded.Value, nil
}

// loadEscalationChatSelection reads the optional escalation chat; an unsaved picker skips escalation.
func loadEscalationChatSelection(configuration projectconfig.Configuration) (incidentacknowledgement.EscalationChatSelection, error) {
	loaded, err := provider.LoadOperationConfiguration[incidentacknowledgement.EscalationChatSelection](configuration, incidentacknowledgement.EscalationChatSelectionConfigurationRef())
	if errors.Is(err, projectconfig.ErrObjectNotFound) {
		return incidentacknowledgement.EscalationChatSelection{}, nil
	}
	if err != nil {
		return incidentacknowledgement.EscalationChatSelection{}, err
	}
	return loaded.Value, nil
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
