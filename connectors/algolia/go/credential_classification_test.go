// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package algolia

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
			client, err := New(Config{BaseURL: server.URL + "/1"}, erroringCredentials{err: testCase.err})
			require.NoError(t, err)
			saved, saveErr := sdkgo.RunMutation(providerContext{}, client.SaveObject(), testConnection,
				SaveObjectInput{IndexName: "products", ObjectID: "product-1", Attributes: map[string]string{"name": "Meridian"}})
			found, searchErr := sdkgo.RunQuery(providerContext{}, client.Search(), testConnection, SearchInput{IndexName: "products", Query: "notebook"})
			if testCase.isTerminal {
				require.NoError(t, errors.Join(saveErr, searchErr))
				require.Equal(t, SaveObjectBranchDefect, saved.Branch)
				require.Equal(t, SearchBranchDefect, found.Branch)
			} else {
				for _, attemptErr := range []error{saveErr, searchErr} {
					var retry *sdkgo.RetryError
					require.ErrorAs(t, attemptErr, &retry)
					require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
				}
			}
			require.Zero(t, requests.Load())
		})
	}
}
