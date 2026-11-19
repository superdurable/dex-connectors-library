// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package rabbitmq

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestPublishMessageSendsOneUTF8Message(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPut, request.Method)
		require.Equal(t, "/api/exchanges/%2F/amq.direct/publish", request.URL.EscapedPath())
		username, password, hasAuth := request.BasicAuth()
		require.True(t, hasAuth)
		require.Equal(t, "observer", username)
		require.Equal(t, "sample-password", password)
		var body struct {
			Properties map[string]any `json:"properties"`
			RoutingKey string         `json:"routing_key"`
			Payload    string         `json:"payload"`
			Encoding   string         `json:"payload_encoding"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, "work", body.RoutingKey)
		require.Equal(t, "hello", body.Payload)
		require.Equal(t, "string", body.Encoding)
		_, err := writer.Write([]byte(`{"routed":true}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	input := PublishMessageInput{VirtualHost: "/", Exchange: "amq.direct", RoutingKey: "work", Payload: "hello"}
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("rabbitmq-publish", "publish-message"), newTestClient(t, server.URL, 0).PublishMessage(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, PublishMessageBranchPublished, result.Branch)
	require.True(t, result.Value.Routed)
}

func TestPublishMessageUnroutableAndRejected(t *testing.T) {
	for _, testCase := range []struct {
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{200, `{"routed":false}`, PublishMessageBranchUnroutable},
		{403, `{"error":"forbidden"}`, PublishMessageBranchProviderRejected},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(testCase.status)
			_, err := writer.Write([]byte(testCase.body))
			require.NoError(t, err)
		}))
		input := PublishMessageInput{VirtualHost: "/", Exchange: "amq.direct", RoutingKey: "work", Payload: "hello"}
		result, err := sdkgo.RunMutation(singledispatchtest.NewContext("rabbitmq-publish", "publish-message"), newTestClient(t, server.URL, 0).PublishMessage(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, testCase.branch, result.Branch)
		server.Close()
	}
}
