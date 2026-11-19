// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openai_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	openai "github.com/superdurable/dex-connectors-library/connectors/openai/go"
	"github.com/superdurable/dex-connectors-library/connectors/openai/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestDeleteResponseClassifiesProviderOutcomes(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		body       string
		wantBranch sdkgo.BranchID
	}{
		{"deleted", 200, `{"id":"resp_123","object":"response.deleted","deleted":true}`, openai.DeleteResponseBranchDeleted},
		{"not found", 404, `{"error":{"message":"response was not found"}}`, openai.DeleteResponseBranchNotFound},
		{"rejected", 403, `{"error":{"message":"request was refused"}}`, openai.DeleteResponseBranchProviderRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				require.Equal(t, http.MethodDelete, request.Method)
				require.Equal(t, "/responses/resp_123", request.URL.Path)
				require.Equal(t, "Bearer test-key", request.Header.Get("Authorization"))
				response.WriteHeader(test.status)
				_, _ = response.Write([]byte(test.body))
			}))
			defer server.Close()
			result, err := sdkgo.RunMutation(testsupport.NewDexContext("delete-flow", test.name), newClient(t, server.URL).DeleteResponse(), openAIConnection, openai.DeleteResponseRequest{ResponseID: "resp_123"})
			require.NoError(t, err)
			require.Equal(t, test.wantBranch, result.Branch)
		})
	}
}

func TestDeleteResponseRetriesUnknownOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		status int
		body string
	}{
		{429, ""}, {503, ""}, {500, ""}, {409, `{"error":{"message":"conflict"}}`},
		{405, `{"error":{"message":"method not allowed"}}`}, {403, ""},
		{403, `{"id":"resp_123","error":{"message":"contradictory"}}`},
		{200, `{"id":"resp_other","deleted":true}`},
	} {
		t.Run(fmt.Sprintf("%d-%s", testCase.status, testCase.body), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(testCase.status)
				_, _ = response.Write([]byte(testCase.body))
			}))
			defer server.Close()
			_, err := sdkgo.RunMutation(testsupport.NewDexContext("delete-flow", fmt.Sprint(testCase.status)), newClient(t, server.URL).DeleteResponse(), openAIConnection, openai.DeleteResponseRequest{ResponseID: "resp_123"})
			require.Error(t, err)
		})
	}
}

func TestDeleteResponseRejectsInvalidIDWithoutSending(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("invalid ID reached provider") }))
	defer server.Close()
	result, err := sdkgo.RunMutation(testsupport.NewDexContext("delete-flow", "invalid"), newClient(t, server.URL).DeleteResponse(), openAIConnection, openai.DeleteResponseRequest{ResponseID: "not/an/id"})
	require.NoError(t, err)
	require.Equal(t, openai.DeleteResponseBranchDefect, result.Branch)
}
