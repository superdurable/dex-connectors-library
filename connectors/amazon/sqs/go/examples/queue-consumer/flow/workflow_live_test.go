//go:build live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package queueconsumer

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs/go"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs/go/internal/sqslive"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TestQueueConsumerExampleRunsAgainstALiveQueueWithRealDex uses the SQS API that SQS_CONNECTOR_TEST_* names.
func TestQueueConsumerExampleRunsAgainstALiveQueueWithRealDex(t *testing.T) {
	server := sqslive.RequireServer(t)
	queueURL := server.CreateQueue(t, "dex-sqs-example", false, 60)
	providerClient, err := sqs.New(sqs.Config{Endpoint: server.Endpoint, Region: server.Region, DefaultQueueURL: queueURL, ReceiveWaitSeconds: 2},
		sdkgo.StaticCredentialProvider[sqs.Credentials]{exampleConnection: {
			AccessKeyID: sdkgo.NewSecretString(server.AccessKeyID), SecretAccessKey: sdkgo.NewSecretString(server.SecretAccessKey),
			SessionToken: sdkgo.NewSecretString(server.SessionToken),
		}})
	require.NoError(t, err)
	flow, harness := newQueueConsumerHarness(t, providerClient)
	ctx := integrationContext(t)
	runID := strconv.FormatInt(time.Now().UnixNano(), 10)

	validID := server.SendMessage(t, queueURL, `{"orderId":"live-1042","event":"paid"}`, "")
	processed := runQueueConsumer(t, ctx, harness.client, flow, "live-processed-"+runID, Input{})
	require.Equal(t, dex.FlowCompleted, processed.status)
	require.Equal(t, StatusProcessed, processed.outcome.Status)
	require.Equal(t, validID, processed.outcome.Message.MessageID)
	require.True(t, processed.outcome.IsAcknowledgementConfirmed)

	invalidID := server.SendMessage(t, queueURL, "not an order event", "")
	rejected := runQueueConsumer(t, ctx, harness.client, flow, "live-rejected-"+runID, Input{})
	require.Equal(t, StatusRejected, rejected.outcome.Status)
	require.True(t, rejected.outcome.IsAcknowledgementConfirmed)
	redelivered := runQueueConsumer(t, ctx, harness.client, flow, "live-redelivered-"+runID, Input{})
	require.Equal(t, StatusRejected, redelivered.outcome.Status)
	require.Equal(t, invalidID, redelivered.outcome.Message.MessageID, "the released message came back, not the acknowledged one")
	require.Equal(t, 2, redelivered.outcome.Message.ReceiveCount)
	t.Logf("live consumer: processed=%s rejected=%s receiveCount=%d", validID, invalidID, redelivered.outcome.Message.ReceiveCount)
}
