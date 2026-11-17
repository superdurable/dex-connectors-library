// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package datadog

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

var testCreateMonitorInput = CreateMonitorInput{Name: "Dex latency alert", Query: "avg(last_5m):avg:system.cpu.user{*} > 0.9", Message: "Investigate CPU usage"}

func TestCreateMonitorSendsMetricAlertAndReturnsID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/api/v1/monitor", request.URL.Path)
		require.Equal(t, "test-apikey", request.Header.Get("DD-API-KEY"))
		var body map[string]string
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, map[string]string{"name": testCreateMonitorInput.Name, "type": "metric alert", "query": testCreateMonitorInput.Query, "message": testCreateMonitorInput.Message}, body)
		_, err := writer.Write([]byte(`{"id":12345,"name":"Dex latency alert","type":"metric alert","overall_state":"OK"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("datadog", "create-monitor"), testClient(t, server.URL).CreateMonitor(), testConnection, testCreateMonitorInput)
	require.NoError(t, err)
	require.Equal(t, CreateMonitorBranchCreated, result.Branch)
	require.Equal(t, int64(12345), result.Value.ID)
}

func TestCreateMonitorStopsAfterTwoUnknownSends(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	ctx := singledispatchtest.NewContext("datadog", "create-monitor-unknown")
	client := testClient(t, server.URL)
	for attempt := 1; attempt <= 2; attempt++ {
		result, err := sdkgo.RunMutation(ctx, client.CreateMonitor(), testConnection, testCreateMonitorInput)
		if attempt == 1 {
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			ctx.AdvanceAttempt()
			continue
		}
		require.NoError(t, err)
		require.Equal(t, CreateMonitorBranchUncertain, result.Branch)
	}
	require.Equal(t, int32(2), requests.Load())
}

func TestCreateMonitorReadsBackKnownIDWithoutResending(t *testing.T) {
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodPost:
			posts.Add(1)
			_, err := writer.Write([]byte(`{"id":12345}`))
			require.NoError(t, err)
		case http.MethodGet:
			require.Equal(t, "/api/v1/monitor/12345", request.URL.Path)
			_, err := writer.Write([]byte(`{"id":12345,"name":"Dex latency alert","type":"metric alert","overall_state":"OK"}`))
			require.NoError(t, err)
		default:
			t.Fatalf("unexpected method %s", request.Method)
		}
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("datadog", "create-monitor-known-id"), testClient(t, server.URL).CreateMonitor(), testConnection, testCreateMonitorInput)
	require.NoError(t, err)
	require.Equal(t, CreateMonitorBranchCreated, result.Branch)
	require.Equal(t, int32(1), posts.Load())
}
