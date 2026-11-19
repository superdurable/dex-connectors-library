// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sns

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var exampleMessage = PublishMessageInput{TopicARN: exampleTopicARN, Message: "Report ready", Subject: "Operations"}

func TestPublishMessageSignsPOSTAndReturnsMessageID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Empty(t, request.URL.RawQuery)
		require.Contains(t, request.Header.Get("Authorization"), "/eu-west-1/sns/aws4_request")
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.Contains(t, string(body), "Action=Publish")
		require.Contains(t, string(body), "Message=Report+ready")
		_, err = response.Write([]byte(`<PublishResponse xmlns="https://sns.amazonaws.com/doc/2010-03-31/"><PublishResult><MessageId>message-123</MessageId></PublishResult></PublishResponse>`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(&publishTestContext{attempt: 1}, newTestClient(t, server.URL, 0).PublishMessage(), testConnection, exampleMessage)
	require.NoError(t, err)
	require.Equal(t, PublishMessageBranchPublished, result.Branch)
	require.Equal(t, "message-123", result.Value.MessageID)
}

func TestPublishMessageClassifiesDefiniteAndUnknownOutcomes(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusInternalServerError} {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(status) }))
		result, err := sdkgo.RunMutation(&publishTestContext{attempt: 1}, newTestClient(t, server.URL, 0).PublishMessage(), testConnection, exampleMessage)
		if status == http.StatusForbidden {
			require.NoError(t, err)
			require.Equal(t, PublishMessageBranchProviderRejected, result.Branch)
		} else {
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
		}
		server.Close()
	}
}

type publishTestContext struct {
	queryContext
	attempt   int32
	heartbeat json.RawMessage
}

func (context *publishTestContext) Attempt() int32 { return context.attempt }
func (context *publishTestContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err == nil {
		context.heartbeat = encoded
	}
	return err
}
func (context *publishTestContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, target)
}
