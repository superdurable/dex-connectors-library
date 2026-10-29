// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package supabase

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

func TestSelectRowsHTTPAndRejection(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{"happy", 200, `[{"id":1,"name":"Meridian"}]`, SelectRowsBranchSelected},
		{"rejected", 401, `{"message":"SECRET SENTINEL"}`, SelectRowsBranchProviderRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/rest/v1/projects", r.URL.Path)
				require.Equal(t, "test-token", r.Header.Get("apikey"))
				require.NotContains(t, r.URL.String(), "test-token")
				w.WriteHeader(test.status)
				_, err := fmt.Fprint(w, test.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			reference := sdkgo.ConnectionRef{Provider: "supabase", Name: "supabase"}
			client, err := New(Config{BaseURL: server.URL + "/rest/v1"}, sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString("test-token")}})
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(testContext{}, client.SelectRows(), reference, SelectRowsInput{Table: "projects", Limit: 10})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			if test.name == "rejected" {
				require.NotNil(t, result.Failure)
				require.NotContains(t, result.Failure.Message, "SENTINEL")
			}
		})
	}
}

func TestUpsertRowsHTTPAndRejection(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{"happy", 201, `[{"id":1,"name":"Meridian"}]`, UpsertRowsBranchUpserted},
		{"rejected", 401, `{"message":"SECRET SENTINEL"}`, UpsertRowsBranchProviderRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/rest/v1/projects", r.URL.Path)
				require.Equal(t, "test-token", r.Header.Get("apikey"))
				require.NotContains(t, r.URL.String(), "test-token")
				require.Equal(t, "resolution=merge-duplicates,return=representation", r.Header.Get("Prefer"))
				w.WriteHeader(test.status)
				_, err := fmt.Fprint(w, test.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			reference := sdkgo.ConnectionRef{Provider: "supabase", Name: "supabase"}
			client, err := New(Config{BaseURL: server.URL + "/rest/v1"}, sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString("test-token")}})
			require.NoError(t, err)
			result, err := sdkgo.RunMutation(testContext{}, client.UpsertRows(), reference, UpsertRowsInput{Table: "projects", OnConflict: "id", Rows: []map[string]any{{"id": 1, "name": "Meridian"}}})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			if test.name == "rejected" {
				require.NotNil(t, result.Failure)
				require.NotContains(t, result.Failure.Message, "SENTINEL")
			}
		})
	}
}

func TestUpsertRowsRequiresStableUniqueConflictValues(t *testing.T) {
	_, _, _, err := encodeUpsertRowsInput(UpsertRowsInput{Table: "projects", OnConflict: "id", Rows: []map[string]any{{"name": "Meridian"}}})
	require.ErrorContains(t, err, "onConflict")
	_, _, _, err = encodeUpsertRowsInput(UpsertRowsInput{Table: "projects", OnConflict: "id", Rows: []map[string]any{{"id": 1}, {"id": 1}}})
	require.ErrorContains(t, err, "duplicate")
}
