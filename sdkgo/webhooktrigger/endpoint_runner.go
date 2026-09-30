// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webhooktrigger

import (
	"context"
	"errors"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// EndpointRunner serves one webhook endpoint while running the Trigger runners of its bindings. An
// application mounts it as an http.Handler and runs it for the life of the process.
type EndpointRunner struct {
	handler http.Handler
	runners []sdkgo.TriggerRunner
}

// NewEndpointRunner returns an EndpointRunner for handler, usually an Endpoint, and at least one runner
// whose sources that handler feeds.
func NewEndpointRunner(handler http.Handler, runners ...sdkgo.TriggerRunner) (*EndpointRunner, error) {
	if handler == nil || len(runners) == 0 {
		return nil, errors.New("webhook endpoint runner requires a handler and at least one Trigger runner")
	}
	for _, runner := range runners {
		if runner == nil {
			return nil, errors.New("webhook endpoint runner Trigger runners cannot be nil")
		}
	}
	return &EndpointRunner{handler: handler, runners: runners}, nil
}

// ServeHTTP receives the shared webhook endpoint.
func (endpointRunner *EndpointRunner) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	endpointRunner.handler.ServeHTTP(response, request)
}

// Run runs every Trigger runner until ctx ends or one runner fails, then stops the others. It returns
// ctx's error after cancellation and otherwise the first runner's failure.
func (endpointRunner *EndpointRunner) Run(ctx context.Context) error {
	if endpointRunner == nil || len(endpointRunner.runners) == 0 {
		return errors.New("webhook endpoint runner is not configured")
	}
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, len(endpointRunner.runners))
	for _, runner := range endpointRunner.runners {
		go func(active sdkgo.TriggerRunner) { results <- active.Run(runContext) }(runner)
	}
	first := <-results
	cancel()
	for range len(endpointRunner.runners) - 1 {
		<-results
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(first, context.Canceled) {
		return nil
	}
	return first
}
