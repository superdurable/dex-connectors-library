// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package sqslive reads the SQS_CONNECTOR_TEST_* configuration of a live SQS API, such as Amazon SQS or a
// local ElasticMQ server, and sends the signed CreateQueue, SendMessage, and DeleteQueue requests that live
// tests need for setup and cleanup, which the connector itself does not offer.
package sqslive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/stretchr/testify/require"
)

// Server is one live SQS API.
type Server struct {
	// Endpoint is the custom endpoint; blank means the Amazon SQS Regional endpoint.
	Endpoint string
	// Region is the signing Region.
	Region string
	// AccessKeyID, SecretAccessKey, and SessionToken sign every request.
	AccessKeyID, SecretAccessKey, SessionToken string
}

// RequireServer reads the live configuration, skipping the test when SQS_CONNECTOR_TEST_ACCESS_KEY_ID is unset.
func RequireServer(t *testing.T) Server {
	t.Helper()
	server := Server{
		Endpoint: os.Getenv("SQS_CONNECTOR_TEST_ENDPOINT"), Region: os.Getenv("SQS_CONNECTOR_TEST_REGION"),
		AccessKeyID: os.Getenv("SQS_CONNECTOR_TEST_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("SQS_CONNECTOR_TEST_SECRET_ACCESS_KEY"),
		SessionToken: os.Getenv("SQS_CONNECTOR_TEST_SESSION_TOKEN"),
	}
	if server.AccessKeyID == "" {
		t.Skip("SQS_CONNECTOR_TEST_ACCESS_KEY_ID is not configured")
	}
	require.NotEmpty(t, server.Region, "SQS_CONNECTOR_TEST_REGION is the signing Region")
	return server
}

// CreateQueue creates a disposable queue with a unique name and deletes it when the test ends. A name ending
// in .fifo creates a FIFO queue with content-based deduplication.
func (server Server) CreateQueue(t *testing.T, prefix string, isFIFO bool, visibilityTimeoutSeconds int) string {
	t.Helper()
	name := fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	attributes := map[string]string{"VisibilityTimeout": fmt.Sprint(visibilityTimeoutSeconds)}
	if isFIFO {
		name += ".fifo"
		attributes["FifoQueue"], attributes["ContentBasedDeduplication"] = "true", "true"
	}
	var created struct {
		QueueURL string `json:"QueueUrl"`
	}
	server.send(t, "CreateQueue", map[string]any{"QueueName": name, "Attributes": attributes}, &created)
	require.NotEmpty(t, created.QueueURL)
	t.Cleanup(func() { server.send(t, "DeleteQueue", map[string]any{"QueueUrl": created.QueueURL}, nil) })
	return created.QueueURL
}

// SendMessage sends one message with a String attribute and returns its message ID; groupID is required for FIFO.
func (server Server) SendMessage(t *testing.T, queueURL string, body string, groupID string) string {
	t.Helper()
	request := map[string]any{
		"QueueUrl": queueURL, "MessageBody": body,
		"MessageAttributes": map[string]any{"tenant": map[string]string{"DataType": "String", "StringValue": "acme"}},
	}
	if groupID != "" {
		request["MessageGroupId"] = groupID
	}
	var sent struct {
		MessageID string `json:"MessageId"`
	}
	server.send(t, "SendMessage", request, &sent)
	require.NotEmpty(t, sent.MessageID)
	return sent.MessageID
}

// send signs one JSON-protocol action and decodes its response into result when result is not nil.
func (server Server) send(t *testing.T, action string, request any, result any) {
	t.Helper()
	endpoint := server.Endpoint
	if endpoint == "" {
		endpoint = "https://sqs." + server.Region + ".amazonaws.com"
	}
	body, err := json.Marshal(request)
	require.NoError(t, err)
	httpRequest, err := http.NewRequestWithContext(context.Background(), http.MethodPost, strings.TrimSuffix(endpoint, "/")+"/", bytes.NewReader(body))
	require.NoError(t, err)
	httpRequest.Header.Set("Content-Type", "application/x-amz-json-1.0")
	httpRequest.Header.Set("X-Amz-Target", "AmazonSQS."+action)
	digest := sha256.Sum256(body)
	credentials := aws.Credentials{AccessKeyID: server.AccessKeyID, SecretAccessKey: server.SecretAccessKey, SessionToken: server.SessionToken}
	require.NoError(t, v4.NewSigner().SignHTTP(context.Background(), credentials, httpRequest, hex.EncodeToString(digest[:]), "sqs", server.Region, time.Now()))
	response, err := http.DefaultClient.Do(httpRequest)
	require.NoError(t, err)
	defer response.Body.Close()
	contents, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode, "%s: %s", action, contents)
	if result != nil {
		require.NoError(t, json.Unmarshal(contents, result), "%s", action)
	}
}
