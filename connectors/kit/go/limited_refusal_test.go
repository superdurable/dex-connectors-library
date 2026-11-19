// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kit

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestDefiniteLimitedRefusalRequiresEvidence(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"explicit", http.StatusForbidden, `{"error":"access denied"}`, true},
		{"conflict", http.StatusConflict, `{"error":"conflict"}`, false},
		{"other 4xx", http.StatusMethodNotAllowed, `{"error":"method not allowed"}`, false},
		{"empty", http.StatusBadRequest, "", false},
		{"empty object", http.StatusBadRequest, `{}`, false},
		{"contradictory", http.StatusBadRequest, `{"error":"invalid","accepted":true}`, false},
		{"success", http.StatusOK, `{"error":"invalid"}`, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := definiteLimitedRefusal(testCase.status, []byte(testCase.body)); got != testCase.want {
				t.Fatalf("definiteLimitedRefusal(%d, %q) = %v, want %v", testCase.status, testCase.body, got, testCase.want)
			}
		})
	}
}

func TestLimitedWriteFailClosedRetriesWithoutDispatch(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	ctx := singledispatchtest.NewContext("kit-fail-closed", "limited-write")
	ctx.SetReadError(errors.New("heartbeat unavailable"))
	_, err := sdkgo.RunMutation(ctx, newTestClient(t, server.URL).AddSubscriberToSequence(), testConnection, AddSubscriberToSequenceInput{SequenceID: 1, SubscriberID: 2})
	var retry *sdkgo.RetryError
	if !errors.As(err, &retry) {
		t.Fatalf("expected retry on FailClosed, got %v", err)
	}
	if requests != 0 {
		t.Fatalf("FailClosed dispatched %d requests", requests)
	}
}
