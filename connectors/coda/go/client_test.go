// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package coda

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type testContext struct{ dex.Context }

func (testContext) FlowID() string              { return "provider-test" }
func (testContext) StepExecutionID() string     { return "provider-step" }
func (testContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (testContext) Done() <-chan struct{}       { return nil }
func (testContext) Err() error                  { return nil }
func (testContext) Value(key any) any           { return context.Background().Value(key) }

func TestListRowsHTTPAndRejection(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{"happy", 200, `{"items":[],"nextPageToken":""}`, ListRowsBranchListed},
		{"rejected", 401, `{"message":"SECRET SENTINEL"}`, ListRowsBranchProviderRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/apis/v1/docs/AbCDeFGH/tables/grid-pqRst-U/rows", r.URL.Path)
				require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
				require.NotContains(t, r.URL.String(), "test-token")
				w.WriteHeader(test.status)
				_, err := fmt.Fprint(w, test.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			reference := sdkgo.ConnectionRef{Provider: "coda", Name: "coda"}
			client, err := New(Config{BaseURL: server.URL + "/apis/v1"}, sdkgo.StaticCredentialProvider[Credentials]{reference: {APIToken: sdkgo.NewSecretString("test-token")}})
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(testContext{}, client.ListRows(), reference, ListRowsInput{DocID: "AbCDeFGH", TableID: "grid-pqRst-U", Limit: 10})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			if test.name == "rejected" {
				require.NotNil(t, result.Failure)
				require.NotContains(t, result.Failure.Message, "SENTINEL")
			}
		})
	}
}

func TestGetRowHTTPAndRejection(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{"happy", 200, `{"id":"i-Meridian","values":{"c-Name":"Meridian"}}`, GetRowBranchFound},
		{"rejected", 401, `{"message":"SECRET SENTINEL"}`, GetRowBranchProviderRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/apis/v1/docs/AbCDeFGH/tables/grid-pqRst-U/rows/i-Meridian", r.URL.Path)
				require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
				require.NotContains(t, r.URL.String(), "test-token")
				w.WriteHeader(test.status)
				_, err := fmt.Fprint(w, test.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			reference := sdkgo.ConnectionRef{Provider: "coda", Name: "coda"}
			client, err := New(Config{BaseURL: server.URL + "/apis/v1"}, sdkgo.StaticCredentialProvider[Credentials]{reference: {APIToken: sdkgo.NewSecretString("test-token")}})
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(testContext{}, client.GetRow(), reference, GetRowInput{DocID: "AbCDeFGH", TableID: "grid-pqRst-U", RowID: "i-Meridian"})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			if test.name == "rejected" {
				require.NotNil(t, result.Failure)
				require.NotContains(t, result.Failure.Message, "SENTINEL")
			}
		})
	}
}
