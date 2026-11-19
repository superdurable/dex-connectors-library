// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package elasticsearch

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
			client, err := New(Config{ClusterURL: server.URL}, erroringCredentials{err: testCase.err})
			require.NoError(t, err)
			indexed, indexErr := sdkgo.RunMutation(queryContext{}, client.IndexDocument(), testConnection,
				IndexDocumentInput{Index: "products", DocumentID: "sku-42", Source: []byte(`{"title":"Meridian"}`)})
			deleted, deleteErr := sdkgo.RunMutation(queryContext{}, client.DeleteDocument(), testConnection,
				DeleteDocumentInput{Index: "products", DocumentID: "sku-42"})
			found, searchErr := sdkgo.RunQuery(queryContext{}, client.SearchDocuments(), testConnection,
				SearchDocumentsInput{Index: "products", Query: "title:Meridian"})
			if testCase.isTerminal {
				require.NoError(t, errors.Join(indexErr, deleteErr, searchErr))
				require.Equal(t, IndexDocumentBranchDefect, indexed.Branch)
				require.Equal(t, DeleteDocumentBranchDefect, deleted.Branch)
				require.Equal(t, SearchDocumentsBranchDefect, found.Branch)
			} else {
				for _, attemptErr := range []error{indexErr, deleteErr, searchErr} {
					var retry *sdkgo.RetryError
					require.ErrorAs(t, attemptErr, &retry)
					require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
				}
			}
			require.Zero(t, requests.Load())
		})
	}
}
