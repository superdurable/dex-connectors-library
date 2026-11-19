// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package redis

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestKeyCommandsUseUpstashREST(t *testing.T) {
	for _, test := range []struct {
		name     string
		request  string
		response string
		invoke   func(*Client) (sdkgo.BranchID, error)
	}{
		{"set", `["SET","sample:key","value","EXAT",1800000000]`, `{"result":"OK"}`, func(client *Client) (sdkgo.BranchID, error) {
			result, err := sdkgo.RunMutation(queryContext{}, client.SetKey(), testConnection, SetKeyInput{Key: "sample:key", Value: "value", ExpireAtUnixSeconds: 1800000000})
			return result.Branch, err
		}},
		{"delete", `["DEL","sample:key"]`, `{"result":1}`, func(client *Client) (sdkgo.BranchID, error) {
			result, err := sdkgo.RunMutation(queryContext{}, client.DeleteKey(), testConnection, DeleteKeyInput{Key: "sample:key"})
			return result.Branch, err
		}},
		{"keys", `["KEYS","sample:*"]`, `{"result":["sample:key"]}`, func(client *Client) (sdkgo.BranchID, error) {
			result, err := sdkgo.RunQuery(queryContext{}, client.ListKeys(), testConnection, ListKeysInput{Pattern: "sample:*"})
			return result.Branch, err
		}},
		{"list length", `["LLEN","sample:list"]`, `{"result":3}`, func(client *Client) (sdkgo.BranchID, error) {
			result, err := sdkgo.RunQuery(queryContext{}, client.GetListLength(), testConnection, GetListLengthInput{Key: "sample:list"})
			return result.Branch, err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				require.Equal(t, http.MethodPost, request.Method)
				require.Equal(t, "Bearer sample-redis-token", request.Header.Get("Authorization"))
				body, err := io.ReadAll(request.Body)
				require.NoError(t, err)
				require.JSONEq(t, test.request, string(body))
				_, err = writer.Write([]byte(test.response))
				require.NoError(t, err)
			}))
			defer server.Close()
			branch, err := test.invoke(newTestClient(t, server.URL, 0))
			require.NoError(t, err)
			require.NotEmpty(t, branch)
		})
	}
}
