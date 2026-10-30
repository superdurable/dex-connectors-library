// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqs_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs/go"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs/go/internal/sqsfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// The fixtures are split or punctuated so that no literal has the shape of a real AWS credential.
	testAccessKeyID     = "AKIA" + "SQSCONNECTORTEST"
	testSecretAccessKey = "sqs-connector-test/secret+value"
	testSessionToken    = "sqs-connector-test-session-token"
	testRegion          = "eu-west-1"
	ordersQueue         = "orders"
	ordersFIFOQueue     = "orders.fifo"
)

var testConnection = sdkgo.ConnectionRef{Provider: "amazon-sqs", Name: "sqs-test"}

// newFakeQueues starts a fake that accepts the test credentials and has a standard and a FIFO queue.
func newFakeQueues(t *testing.T, configure ...func(*sqsfake.Config)) *sqsfake.Server {
	t.Helper()
	config := sqsfake.Config{
		AccessKeyID: testAccessKeyID, SecretAccessKey: testSecretAccessKey, Region: testRegion,
		Queues: []string{ordersQueue, ordersFIFOQueue},
	}
	for _, apply := range configure {
		apply(&config)
	}
	return sqsfake.NewServer(t, config)
}

// newFakeClient addresses the fake as a custom endpoint with the standard queue as the default queue.
func newFakeClient(t *testing.T, queues *sqsfake.Server, configure ...func(*sqs.Config)) *sqs.Client {
	t.Helper()
	config := sqs.Config{Endpoint: queues.URL, Region: testRegion, DefaultQueueURL: queues.QueueURL(ordersQueue)}
	for _, apply := range configure {
		apply(&config)
	}
	client, err := sqs.New(config, staticCredentials(""))
	require.NoError(t, err)
	return client
}

// newStaticServer answers with handler without verifying signatures, for malformed-response cases.
func newStaticServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func staticCredentials(sessionToken string) sdkgo.StaticCredentialProvider[sqs.Credentials] {
	return sdkgo.StaticCredentialProvider[sqs.Credentials]{testConnection: {
		AccessKeyID: sdkgo.NewSecretString(testAccessKeyID), SecretAccessKey: sdkgo.NewSecretString(testSecretAccessKey),
		SessionToken: sdkgo.NewSecretString(sessionToken),
	}}
}

func runMutation[IN, OUT any](t *testing.T, context *fakeDexContext, operation sdkgo.Mutation[IN, OUT], input IN) (sdkgo.MutationResult[OUT], error) {
	t.Helper()
	return sdkgo.RunMutation(context, operation, testConnection, input)
}

// receiveOne receives at most one message at once from the default queue in a new Step execution.
func receiveOne(t *testing.T, client *sqs.Client, step string) sqs.ReceiveMessagesResult {
	t.Helper()
	result, err := runMutation(t, newDexContext(step), client.ReceiveMessages(), sqs.ReceiveMessagesInput{MaxMessages: 1, WaitTimeSeconds: seconds(0)})
	require.NoError(t, err)
	return result
}

func seconds(value int) *int { return &value }

// requireNoSecrets proves a Result or error carries neither provider message text nor a credential.
func requireNoSecrets(t *testing.T, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	for _, forbidden := range []string{"SENTINEL", testAccessKeyID, testSecretAccessKey, testSessionToken} {
		require.NotContains(t, string(encoded), forbidden)
	}
}

func requireRetry(t *testing.T, err error, kind sdkgo.FailureKind) *sdkgo.RetryError {
	t.Helper()
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, kind, retry.Failure.Kind)
	requireNoSecrets(t, retry.Failure)
	return retry
}

// fakeDexContext is a Step-execution dex.Context without a Dex Server; every attempt shares the Step ID.
type fakeDexContext struct {
	context.Context
	step    string
	attempt int32
}

func newDexContext(step string) *fakeDexContext {
	return &fakeDexContext{Context: context.Background(), step: step, attempt: 1}
}

// nextAttempt is the following attempt of the same Step execution, which shares its Call ID.
func (context *fakeDexContext) nextAttempt() *fakeDexContext {
	return &fakeDexContext{Context: context.Context, step: context.step, attempt: context.attempt + 1}
}

func (*fakeDexContext) FlowID() string                                  { return "sqs-flow" }
func (*fakeDexContext) RunID() string                                   { return "run" }
func (*fakeDexContext) FlowStartedAt() time.Time                        { return time.Unix(1, 0) }
func (context *fakeDexContext) StepExecutionID() string                 { return context.step }
func (*fakeDexContext) FromStepExecutionID() string                     { return "" }
func (*fakeDexContext) RecoveryError() *dex.RecoveryErrorInfo           { return nil }
func (*fakeDexContext) FirstAttemptAt() time.Time                       { return time.Unix(1, 0) }
func (context *fakeDexContext) Attempt() int32                          { return context.attempt }
func (*fakeDexContext) HasTimerFired() bool                             { return false }
func (*fakeDexContext) HasTimerFiredByIndex(int) bool                   { return false }
func (*fakeDexContext) WaitForMethodFailed() bool                       { return false }
func (*fakeDexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*fakeDexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*fakeDexContext) RecordEvent(string, any) error                   { return nil }
func (*fakeDexContext) RecordHeartbeat(any) error                       { return nil }
func (*fakeDexContext) GetLastHeartbeatValue(any) (bool, error) {
	return false, errors.New("no heartbeat")
}

var _ dex.Context = (*fakeDexContext)(nil)
