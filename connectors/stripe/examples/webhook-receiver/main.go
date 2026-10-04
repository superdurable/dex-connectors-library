// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/stripe"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

const (
	connectionName = "stripe-payments"
	bindingName    = "registration-payments"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("Stripe webhook receiver stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return err
	}
	runtime, err := stripe.NewProjectCheckoutSessionWebhookRuntime(
		project,
		connectionName,
		[]stripe.ProjectCheckoutSessionUpdatedTriggerRoute{{BindingName: bindingName, Target: checkoutEventLogger{}}},
	)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("/webhooks/stripe", runtime)
	server := &http.Server{
		Addr: environmentOr("STRIPE_WEBHOOK_BIND_ADDRESS", "127.0.0.1:8080"), Handler: mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, 2)
	go func() { results <- runtime.Run(runContext) }()
	go func() { results <- server.ListenAndServe() }()
	select {
	case <-ctx.Done():
		err = nil
	case err = <-results:
		if errors.Is(err, http.ErrServerClosed) || errors.Is(err, context.Canceled) {
			err = nil
		}
	}
	cancel()
	shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	return errors.Join(err, server.Shutdown(shutdownContext))
}

type checkoutEventLogger struct{}

func (checkoutEventLogger) HandleTrigger(_ context.Context, event sdkgo.TriggerEvent[stripe.CheckoutSessionEvent]) error {
	slog.Info(
		"Stripe Checkout Session event received",
		"event_id", event.ID,
		"event_type", event.Payload.Type,
		"session_id", event.Payload.Session.ID,
		"client_reference_id", event.Payload.Session.ClientReferenceID,
	)
	return nil
}

func environmentOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
