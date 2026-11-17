// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package todoist

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const closeTaskID = "6XGgmFVcrG5RRjVr"

func TestCloseTaskUsesStableSyncCommandUUIDAcrossUnknownResult(t *testing.T) {
	var sends atomic.Int32
	var advances atomic.Int32
	var commandUUID string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/api/v1/sync", request.URL.Path)
		require.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
		require.Equal(t, "Bearer "+testToken, request.Header.Get("Authorization"))
		require.NoError(t, request.ParseForm())
		var commands []struct {
			Type string `json:"type"`
			UUID string `json:"uuid"`
			Args struct {
				ID string `json:"id"`
			} `json:"args"`
		}
		require.NoError(t, json.Unmarshal([]byte(request.Form.Get("commands")), &commands))
		require.Len(t, commands, 1)
		require.Equal(t, "item_close", commands[0].Type)
		require.Equal(t, closeTaskID, commands[0].Args.ID)
		if commandUUID == "" {
			commandUUID = commands[0].UUID
			advances.Add(1)
		}
		require.Equal(t, commandUUID, commands[0].UUID)
		if sends.Add(1) == 1 {
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, err := fmt.Fprintf(writer, `{"sync_status":{%q:"ok"}}`, commandUUID)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/v1")
	_, err := sdkgo.RunMutation(providerContext{}, client.CloseTask(), testConnection, CloseTaskInput{TaskID: closeTaskID})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	result, err := sdkgo.RunMutation(providerContext{}, client.CloseTask(), testConnection, CloseTaskInput{TaskID: closeTaskID})
	require.NoError(t, err)
	require.Equal(t, CloseTaskBranchClosed, result.Branch)
	require.Equal(t, closeTaskID, result.Value.TaskID)
	require.Equal(t, int32(2), sends.Load())
	require.Equal(t, int32(1), advances.Load())
}

func TestCloseTaskClassifiesSyncCommandRejectionWithoutProviderText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.NoError(t, request.ParseForm())
		var commands []struct {
			UUID string `json:"uuid"`
		}
		require.NoError(t, json.Unmarshal([]byte(request.Form.Get("commands")), &commands))
		_, err := fmt.Fprintf(writer, `{"sync_status":{%q:{"http_code":400,"error":"ITEM_NOT_FOUND","error_extra":"secret provider detail"}}}`, commands[0].UUID)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(providerContext{}, newTestClient(t, server.URL+"/api/v1").CloseTask(), testConnection, CloseTaskInput{TaskID: closeTaskID})
	require.NoError(t, err)
	require.Equal(t, CloseTaskBranchNotFound, result.Branch)
	require.NotContains(t, result.Failure.Message, "secret provider detail")
}
