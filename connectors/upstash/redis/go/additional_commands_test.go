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

func TestAdditionalRedisCommands(t *testing.T) {
	for _, test := range []struct {
		name     string
		body     string
		response string
		invoke   func(*Client) (sdkgo.BranchID, error)
	}{
		{"info", `["INFO"]`, `{"result":"# Server\nredis_version:7.2"}`, func(client *Client) (sdkgo.BranchID, error) {
			result, err := sdkgo.RunQuery(queryContext{}, client.GetInfo(), testConnection, GetInfoInput{})
			return result.Branch, err
		}},
		{"push", `["LPUSH","sample:list","item"]`, `{"result":3}`, func(client *Client) (sdkgo.BranchID, error) {
			result, err := sdkgo.RunMutation(&redisTestContext{}, client.PushListItem(), testConnection, PushListItemInput{Key: "sample:list", Value: "item", Direction: "left"})
			return result.Branch, err
		}},
		{"publish", `["PUBLISH","sample:channel","message"]`, `{"result":1}`, func(client *Client) (sdkgo.BranchID, error) {
			result, err := sdkgo.RunMutation(&redisTestContext{}, client.PublishMessage(), testConnection, PublishMessageInput{Channel: "sample:channel", Message: "message"})
			return result.Branch, err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				body, err := io.ReadAll(request.Body)
				require.NoError(t, err)
				require.JSONEq(t, test.body, string(body))
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
