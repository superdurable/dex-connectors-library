// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gusto

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAccessToken = "synthetic-gusto-token"
const testEmployeeID = "90a68db5-cb3a-4f64-bb60-c0a934ee7600"

var testConnection = sdkgo.ConnectionRef{Provider: "gusto", Name: "gusto"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "gusto-provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	credentials := Credentials{
		OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
		AccessToken: sdkgo.NewSecretString(testAccessToken), RefreshToken: sdkgo.NewSecretString("refresh-token"),
	}
	client, err := New(Config{BaseURL: baseURL, RedirectURI: "https://dex.example/callback"}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetEmployeeProjectsProviderIdentityWithoutPayData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v1/employees/"+testEmployeeID, r.URL.Path)
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		require.Equal(t, "2026-06-15", r.Header.Get("X-Gusto-API-Version"))
		_, err := w.Write([]byte(`{"uuid":"90a68db5-cb3a-4f64-bb60-c0a934ee7600","first_name":"Ada","last_name":"Lovelace","email":"ada@example.com","company_uuid":"7d61f73e-a18c-46a5-88e2-2bfa894657d7","compensations":[{"rate":"99999"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetEmployee(), testConnection, GetEmployeeInput{EmployeeID: testEmployeeID})
	require.NoError(t, err)
	require.Equal(t, GetEmployeeBranchFound, result.Branch)
	require.Equal(t, Employee{EmployeeID: testEmployeeID, FirstName: "Ada", LastName: "Lovelace", Email: "ada@example.com", CompanyID: "7d61f73e-a18c-46a5-88e2-2bfa894657d7"}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "rate")
	require.NotContains(t, string(encoded), testAccessToken)
}

func TestGetEmployeeRejectsInvalidUUIDBeforeProviderCall(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetEmployee(), testConnection, GetEmployeeInput{EmployeeID: "invalid/id"})
	require.NoError(t, err)
	require.Equal(t, GetEmployeeBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}
