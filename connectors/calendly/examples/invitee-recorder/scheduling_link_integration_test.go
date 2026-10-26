//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/calendly"
	"github.com/superdurable/dex-connectors-library/connectors/calendly/examples/invitee-recorder/linkflow"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestSchedulingLinkExampleRetries503WithRealDex(t *testing.T) {
	var mu sync.Mutex
	var requestBodies []string
	provider := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/scheduling_links" {
			http.NotFound(response, request)
			return
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		requestBodies = append(requestBodies, string(body))
		requestCount := len(requestBodies)
		mu.Unlock()
		response.Header().Set("Content-Type", "application/json")
		if requestCount == 1 {
			response.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		response.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(response, `{"resource":{"booking_url":"https://calendly.com/d/abcd-1234/discovery-call","owner":"https://api.calendly.com/event_types/TYPE0001"}}`)
	}))
	defer provider.Close()
	target, err := url.Parse(provider.URL)
	require.NoError(t, err)
	base := provider.Client().Transport
	connection := newExampleConnection(t, calendly.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		redirected := request.Clone(request.Context())
		redirected.URL.Scheme, redirected.URL.Host, redirected.Host = target.Scheme, target.Host, target.Host
		return base.RoundTrip(redirected)
	})}))
	flow := linkflow.NewSchedulingLinkFlow(connection)
	client := startWorkerAndClient(t, flow)
	input := calendly.CreateSchedulingLinkInput{EventTypeURI: bookedEventTypeURI}
	flowID := fmt.Sprintf("calendly-link-retry-%d", time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, err = client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var link calendly.SchedulingLink
	require.NoError(t, result.DecodeSingleOutput(&link))
	require.Equal(t, input.EventTypeURI, link.EventTypeURI)
	require.Equal(t, "https://calendly.com/d/abcd-1234/discovery-call", link.BookingURL)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, requestBodies, 2)
	require.Equal(t, requestBodies[0], requestBodies[1], "the same logical call is sent after the rejected attempt")
}
