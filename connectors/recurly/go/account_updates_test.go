// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recurly

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestSetAccountTimeZoneUsesAbsoluteReplacement(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPut, request.Method)
		require.Equal(t, "/api/accounts/e28zov4fw0v2", request.URL.Path)
		var body struct {
			TimeZone string `json:"preferred_time_zone"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, "America/Los_Angeles", body.TimeZone)
		_, err := writer.Write([]byte(`{"id":"e28zov4fw0v2","code":"acme-corp","preferred_time_zone":"America/Los_Angeles","email":"private@example.com"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	input := SetAccountTimeZoneInput{AccountID: "e28zov4fw0v2", TimeZone: "America/Los_Angeles"}
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("recurly-account", "time-zone"), newTestClient(t, server.URL+"/api", 0).SetAccountTimeZone(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, SetAccountTimeZoneBranchUpdated, result.Branch)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private@example.com")
}

func TestReactivateAccountConfirmsAlreadyActive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPut {
			writer.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		require.Equal(t, "/api/accounts/e28zov4fw0v2", request.URL.Path)
		_, err := writer.Write([]byte(`{"id":"e28zov4fw0v2","code":"acme-corp","state":"active"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("recurly-account", "reactivate"), newTestClient(t, server.URL+"/api", 0).ReactivateAccount(), testConnection, ReactivateAccountInput{AccountID: "e28zov4fw0v2"})
	require.NoError(t, err)
	require.Equal(t, ReactivateAccountBranchReactivated, result.Branch)
}
