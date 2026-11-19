// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package eventbrite

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

var validCreateInput = CreateEventInput{
	OrganizationID: "12345", Name: "Example event", StartUTC: "2026-12-01T18:00:00Z",
	EndUTC: "2026-12-01T19:00:00Z", Timezone: "America/Los_Angeles", Currency: "USD",
}

func TestCreateEventUnknownClientErrorsReachTheSendCap(t *testing.T) {
	for _, firstStatus := range []int{http.StatusConflict, http.StatusMethodNotAllowed, http.StatusBadRequest} {
		t.Run(http.StatusText(firstStatus), func(t *testing.T) {
			var requests atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				attempt := requests.Add(1)
				status := firstStatus
				body := ""
				if attempt == 2 {
					status = http.StatusBadRequest
					body = `{"error":"BAD_REQUEST","error_description":"Invalid event","status_code":400,"id":"123"}`
				}
				if attempt == 3 {
					status = http.StatusBadRequest
					body = `{"error":"BAD_REQUEST","error_description":"Invalid event","status_code":400}`
				}
				response.WriteHeader(status)
				_, err := response.Write([]byte(body))
				require.NoError(t, err)
			}))
			defer provider.Close()
			client := newTestClient(t, provider.URL)
			ctx := singledispatchtest.NewContext("eventbrite-client-error", "create-event")
			for attempt := 1; attempt <= 2; attempt++ {
				_, err := sdkgo.RunMutation(ctx, client.CreateEvent(), testConnection, validCreateInput)
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				ctx.AdvanceAttempt()
			}
			result, err := sdkgo.RunMutation(ctx, client.CreateEvent(), testConnection, validCreateInput)
			require.NoError(t, err)
			require.Equal(t, CreateEventBranchUncertain, result.Branch)
			require.EqualValues(t, 3, requests.Load())
		})
	}
}

func TestCreateEventFailClosedDoesNotSend(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("FailClosed must not dispatch")
	}))
	defer provider.Close()
	ctx := singledispatchtest.NewContext("eventbrite-bad-checkpoint", "create-event")
	ctx.SetReadError(errors.New("checkpoint unavailable"))
	_, err := sdkgo.RunMutation(ctx, newTestClient(t, provider.URL).CreateEvent(), testConnection, validCreateInput)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}
