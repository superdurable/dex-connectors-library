// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pagerduty

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type providerTestContext struct{ dex.Context }

func (providerTestContext) FlowID() string              { return "provider-test" }
func (providerTestContext) StepExecutionID() string     { return "provider-step" }
func (providerTestContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (providerTestContext) Done() <-chan struct{}       { return nil }
func (providerTestContext) Err() error                  { return nil }
func (providerTestContext) Value(key any) any           { return context.Background().Value(key) }

var testConnection = sdkgo.ConnectionRef{Provider: "pagerduty", Name: "pagerduty"}

func TestListIncidentsHTTPAndFailure(t *testing.T) {
	reject := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "GET", request.Method)
		require.Equal(t, "/incidents?limit=25&offset=0", request.URL.RequestURI())
		require.Equal(t, "Token token=test-secret-token", request.Header.Get("Authorization"))
		require.Equal(t, "application/vnd.pagerduty+json;version=2", request.Header.Get("Accept"))
		require.NotContains(t, request.URL.String(), "test-secret-token")

		if reject {
			writer.WriteHeader(400)
			return
		}
		_, err := io.WriteString(writer, `{"incidents":[{"id":"P123456","title":"API latency","status":"triggered"}],"limit":25,"offset":0,"more":false}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString("test-secret-token")}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(providerTestContext{}, client.ListIncidents(), testConnection, ListIncidentsInput{Limit: 25})
	require.NoError(t, err)
	require.Equal(t, ListIncidentsBranchListed, result.Branch)
	require.Nil(t, result.Failure)
	reject = true
	rejected, err := sdkgo.RunQuery(providerTestContext{}, client.ListIncidents(), testConnection, ListIncidentsInput{Limit: 25})
	require.NoError(t, err)
	require.Equal(t, ListIncidentsBranchProviderRejected, rejected.Branch)
	require.NotNil(t, rejected.Failure)
}

func TestGetIncidentHTTPAndFailure(t *testing.T) {
	reject := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "GET", request.Method)
		require.Equal(t, "/incidents/P123456", request.URL.RequestURI())
		require.Equal(t, "Token token=test-secret-token", request.Header.Get("Authorization"))
		require.Equal(t, "application/vnd.pagerduty+json;version=2", request.Header.Get("Accept"))
		require.NotContains(t, request.URL.String(), "test-secret-token")

		if reject {
			writer.WriteHeader(404)
			return
		}
		_, err := io.WriteString(writer, `{"incident":{"id":"P123456","title":"API latency","status":"triggered"}}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString("test-secret-token")}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(providerTestContext{}, client.GetIncident(), testConnection, GetIncidentInput{ID: "P123456"})
	require.NoError(t, err)
	require.Equal(t, GetIncidentBranchFound, result.Branch)
	require.Nil(t, result.Failure)
	reject = true
	rejected, err := sdkgo.RunQuery(providerTestContext{}, client.GetIncident(), testConnection, GetIncidentInput{ID: "P123456"})
	require.NoError(t, err)
	require.Equal(t, GetIncidentBranchNotFound, rejected.Branch)
	require.NotNil(t, rejected.Failure)
}
