// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqs_test

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestSendMessageSignsAndChecksAcceptedBody(t *testing.T) {
	const body = "Report ready"
	digest := md5.Sum([]byte(body))
	checksum := hex.EncodeToString(digest[:])
	server := newStaticServer(t, func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "AmazonSQS.SendMessage", request.Header.Get("X-Amz-Target"))
		require.Contains(t, request.Header.Get("Authorization"), "/eu-west-1/sqs/aws4_request")
		var sent map[string]string
		require.NoError(t, json.NewDecoder(request.Body).Decode(&sent))
		require.Equal(t, body, sent["MessageBody"])
		require.NotContains(t, sent, "MessageDeduplicationId")
		_, err := response.Write([]byte(`{"MessageId":"message-123","MD5OfMessageBody":"` + checksum + `"}`))
		require.NoError(t, err)
	})
	queueURL := server.URL + "/123456789012/orders"
	client, err := sqs.New(sqs.Config{Endpoint: server.URL, Region: testRegion, DefaultQueueURL: queueURL}, staticCredentials(""))
	require.NoError(t, err)
	result, err := runMutation(t, newDexContext("send"), client.SendMessage(), sqs.SendMessageInput{Body: body})
	require.NoError(t, err)
	require.Equal(t, sqs.SendMessageBranchSent, result.Branch)
	require.Equal(t, sqs.SentMessage{QueueURL: queueURL, MessageID: "message-123", MD5OfBody: checksum}, result.Value)
}

func TestSendMessageUsesStableFIFODeduplicationID(t *testing.T) {
	digest := md5.Sum([]byte("paid"))
	checksum := hex.EncodeToString(digest[:])
	server := newStaticServer(t, func(response http.ResponseWriter, request *http.Request) {
		var sent map[string]string
		require.NoError(t, json.NewDecoder(request.Body).Decode(&sent))
		require.Equal(t, "orders", sent["MessageGroupId"])
		require.NotEmpty(t, sent["MessageDeduplicationId"])
		_, err := response.Write([]byte(`{"MessageId":"message-123","MD5OfMessageBody":"` + checksum + `"}`))
		require.NoError(t, err)
	})
	client, err := sqs.New(sqs.Config{Endpoint: server.URL, Region: testRegion, DefaultQueueURL: server.URL + "/123456789012/orders.fifo"}, staticCredentials(""))
	require.NoError(t, err)
	result, err := runMutation(t, newDexContext("fifo-send"), client.SendMessage(), sqs.SendMessageInput{Body: "paid", MessageGroupID: "orders"})
	require.NoError(t, err)
	require.Equal(t, sqs.SendMessageBranchSent, result.Branch)
}

func TestSendMessageClassifiesRejectionAndUnknownResponse(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusInternalServerError, http.StatusOK} {
		server := newStaticServer(t, func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(status)
			if status == http.StatusOK {
				_, _ = response.Write([]byte(`{}`))
			}
		})
		client, err := sqs.New(sqs.Config{Endpoint: server.URL, Region: testRegion, DefaultQueueURL: server.URL + "/123456789012/orders"}, staticCredentials(""))
		require.NoError(t, err)
		result, err := runMutation(t, newDexContext("status-send"), client.SendMessage(), sqs.SendMessageInput{Body: "paid"})
		switch status {
		case http.StatusForbidden:
			require.NoError(t, err)
			require.Equal(t, sqs.SendMessageBranchProviderRejected, result.Branch)
		case http.StatusOK:
			require.NoError(t, err)
			require.Equal(t, sqs.SendMessageBranchUncertain, result.Branch)
		default:
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
		}
	}
}
