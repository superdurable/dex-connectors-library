// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/atlassian/bitbucket/go"
	repositoryevent "github.com/superdurable/dex-connectors-library/connectors/atlassian/bitbucket/go/examples/repository-event/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("Bitbucket repository event worker stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return err
	}
	flow := repositoryevent.NewFlow()
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	if err != nil {
		return fmt.Errorf("register Bitbucket repository event Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(os.TempDir(), "dex-bitbucket-repository-events"), MaxBytes: 1 << 30})
	if err != nil {
		return err
	}
	address := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: address})
	if err != nil {
		return errors.Join(err, cache.Close())
	}
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: address})
	if err != nil {
		return errors.Join(err, cache.Close())
	}
	target := sdkgo.NewDexFlowTriggerTarget(client, flow, repositoryevent.AcceptEveryEvent, repositoryevent.ResolveFlowID, repositoryevent.MapToFlowInput)
	runner, err := bitbucket.NewProjectRepositoryEventReceivedEndpointRunner(project, repositoryevent.ConnectionName, []bitbucket.ProjectRepositoryEventReceivedTriggerRoute{{BindingName: repositoryevent.TriggerBindingName, Target: target}}, bitbucket.WithWebhookSecret(sdkgo.NewSecretString(os.Getenv("BITBUCKET_WEBHOOK_SECRET"))))
	if err != nil {
		return errors.Join(err, client.Close(), cache.Close())
	}
	listenAddress := os.Getenv("BITBUCKET_WEBHOOK_LISTEN_ADDRESS")
	if listenAddress == "" {
		listenAddress = "127.0.0.1:9212"
	}
	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		return errors.Join(err, client.Close(), cache.Close())
	}
	server := &http.Server{Handler: runner, ReadHeaderTimeout: 10 * time.Second}
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	triggerContext, cancelTrigger := context.WithCancel(ctx)
	defer cancelTrigger()
	triggerResult := make(chan error, 1)
	go func() { triggerResult <- runner.Run(triggerContext) }()
	serverResult := make(chan error, 1)
	go func() { serverResult <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
	case err = <-workerResult:
		workerResult <- nil
	case err = <-triggerResult:
		triggerResult <- nil
	case err = <-serverResult:
		serverResult <- nil
	}
	cancelTrigger()
	stopContext, cancelStop := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelStop()
	serverError := server.Shutdown(stopContext)
	workerError := worker.Stop(stopContext)
	triggerError := <-triggerResult
	serverServeError := <-serverResult
	workerRunError := <-workerResult
	if errors.Is(triggerError, context.Canceled) {
		triggerError = nil
	}
	if errors.Is(serverServeError, http.ErrServerClosed) {
		serverServeError = nil
	}
	return errors.Join(err, serverError, workerError, triggerError, serverServeError, workerRunError, client.Close(), cache.Close())
}
