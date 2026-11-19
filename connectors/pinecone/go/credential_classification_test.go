// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pinecone

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// erroringCredentials fails every Resolve with one fixed error.
type erroringCredentials struct{ err error }

func (credentials erroringCredentials) Resolve(sdkgo.Call) (Credentials, error) {
	return Credentials{}, credentials.err
}

func TestCredentialFailuresSelectDefectOnlyWhenTerminal(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		err        error
		isTerminal bool
	}{
		{"reauthorization required", sdkgo.NewReauthorizationRequiredError(nil), true},
		{"project reauthorization required", fmt.Errorf("refresh: %w", projectconfig.ErrReauthorizationRequired), true},
		{"connection object not found", projectconfig.ErrObjectNotFound, true},
		{"connection not selected", projectconfig.ErrConnectionNotSelected, true},
		{"credential broker rejected", projectconfig.ErrCredentialBrokerRejected, true},
		{"credential broker unavailable", projectconfig.ErrCredentialBrokerUnavailable, false},
		{"static connection not configured", errors.New("connection is not configured"), false},
		{"deadline exceeded", context.DeadlineExceeded, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			client, err := New(Config{BaseURL: server.URL}, erroringCredentials{err: testCase.err})
			require.NoError(t, err)
			reference := sdkgo.ConnectionRef{Provider: "pinecone", Name: "pinecone"}
			result, err := sdkgo.RunMutation(testContext{}, client.UpsertVectors(), reference, UpsertVectorsInput{Namespace: "example", Vectors: []Vector{{ID: "vec-meridian", Values: []float32{0.1, 0.2}}}})
			if testCase.isTerminal {
				require.NoError(t, err)
				require.Equal(t, UpsertVectorsBranchDefect, result.Branch)
			} else {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
			}
			require.Zero(t, requests.Load())
		})
	}
}
