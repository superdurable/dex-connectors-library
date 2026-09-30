// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webhooktrigger_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

type testTriggerRunner struct {
	run func(context.Context) error
}

func (runner testTriggerRunner) Run(ctx context.Context) error { return runner.run(ctx) }
func (testTriggerRunner) Definition() sdkgo.TriggerDefinition  { return sdkgo.TriggerDefinition{} }
func (testTriggerRunner) Binding() sdkgo.TriggerBindingRef     { return sdkgo.TriggerBindingRef{} }

func waitForCancellation(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestEndpointRunnerStopsEveryRunnerWhenOneFails(t *testing.T) {
	stopped := make(chan struct{})
	failing := testTriggerRunner{run: func(context.Context) error { return errors.New("binding failed") }}
	waiting := testTriggerRunner{run: func(ctx context.Context) error {
		defer close(stopped)
		return waitForCancellation(ctx)
	}}
	endpointRunner, err := webhooktrigger.NewEndpointRunner(http.NotFoundHandler(), failing, waiting)
	require.NoError(t, err)
	require.EqualError(t, endpointRunner.Run(context.Background()), "binding failed")
	<-stopped
}

func TestEndpointRunnerReturnsTheContextErrorAfterCancellation(t *testing.T) {
	endpointRunner, err := webhooktrigger.NewEndpointRunner(http.NotFoundHandler(),
		testTriggerRunner{run: waitForCancellation}, testTriggerRunner{run: waitForCancellation})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, endpointRunner.Run(ctx), context.Canceled)
}

func TestEndpointRunnerServesItsHandler(t *testing.T) {
	endpointRunner, err := webhooktrigger.NewEndpointRunner(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusAccepted)
	}), testTriggerRunner{run: waitForCancellation})
	require.NoError(t, err)
	response := httptest.NewRecorder()
	endpointRunner.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/webhooks", nil))
	require.Equal(t, http.StatusAccepted, response.Code)
}

func TestNewEndpointRunnerRequiresAHandlerAndRunners(t *testing.T) {
	_, err := webhooktrigger.NewEndpointRunner(nil, testTriggerRunner{run: waitForCancellation})
	require.Error(t, err)
	_, err = webhooktrigger.NewEndpointRunner(http.NotFoundHandler())
	require.Error(t, err)
	_, err = webhooktrigger.NewEndpointRunner(http.NotFoundHandler(), nil)
	require.Error(t, err)
}
