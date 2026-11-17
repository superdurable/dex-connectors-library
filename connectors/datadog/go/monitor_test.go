// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package datadog

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestMonitorOperationsUseTypedRoutes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/api/v1/monitor/12345", request.URL.Path)
		require.Equal(t, "test-apikey", request.Header.Get("DD-API-KEY"))
		require.Equal(t, "test-applicationkey", request.Header.Get("DD-APPLICATION-KEY"))
		switch request.Method {
		case http.MethodGet:
			_, err := writer.Write([]byte(`{"id":12345,"name":"Meridian API latency","type":"metric alert","overall_state":"OK","query":"secret excluded"}`))
			require.NoError(t, err)
		case http.MethodPut:
			require.Equal(t, "application/json", request.Header.Get("Content-Type"))
			var body map[string]string
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			require.Equal(t, map[string]string{"name": "Updated monitor"}, body)
			_, err := writer.Write([]byte(`{"id":12345,"name":"Updated monitor","type":"metric alert","overall_state":"OK"}`))
			require.NoError(t, err)
		case http.MethodDelete:
			_, err := writer.Write([]byte(`{"deleted_monitor_id":12345}`))
			require.NoError(t, err)
		default:
			t.Fatalf("unexpected monitor method %s", request.Method)
		}
	}))
	defer server.Close()
	client := testClient(t, server.URL)
	monitor, err := sdkgo.RunQuery(testContext{}, client.GetMonitor(), testConnection, GetMonitorInput{MonitorID: 12345})
	require.NoError(t, err)
	require.Equal(t, GetMonitorBranchFound, monitor.Branch)
	require.NotContains(t, monitor.Value.Name, "secret")
	name := "Updated monitor"
	updated, err := sdkgo.RunMutation(testContext{}, client.UpdateMonitor(), testConnection, UpdateMonitorInput{MonitorID: 12345, Name: &name})
	require.NoError(t, err)
	require.Equal(t, UpdateMonitorBranchUpdated, updated.Branch)
	require.Equal(t, name, updated.Value.Name)
	deleted, err := sdkgo.RunMutation(testContext{}, client.DeleteMonitor(), testConnection, DeleteMonitorInput{MonitorID: 12345})
	require.NoError(t, err)
	require.Equal(t, DeleteMonitorBranchDeleted, deleted.Branch)
	require.Equal(t, int64(12345), deleted.Value.MonitorID)
}

func TestMonitorRejectionDoesNotExposeProviderBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusForbidden)
		_, err := writer.Write([]byte(`{"errors":["secret incident detail"]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(testContext{}, testClient(t, server.URL).DeleteMonitor(), testConnection, DeleteMonitorInput{MonitorID: 12345})
	require.NoError(t, err)
	require.Equal(t, DeleteMonitorBranchProviderRejected, result.Branch)
	require.NotNil(t, result.Failure)
	require.False(t, strings.Contains(result.Failure.Message, "secret incident detail"))
}
