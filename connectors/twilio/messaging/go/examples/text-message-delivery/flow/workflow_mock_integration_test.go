//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textmessagedelivery

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/twilio/messaging/go"
	"github.com/superdurable/dex-connectors-library/connectors/twilio/messaging/go/messagingmock"
)

func TestTextMessageDeliveryUsesMockDefaultsWithRealDex(t *testing.T) {
	mock := messagingmock.New(t, ConnectionName)
	flow, harness := newTwilioIntegrationHarnessForConnection(t, mock.Connection())
	ctx := integrationContext(t)
	input := Input{To: "+14155550123", Body: "Your table is ready."}
	flowID := startTextMessageFlow(t, ctx, harness, flow, "mock-default", input)
	delivery := waitForFlowOutput(t, ctx, harness, flowID)

	require.Equal(t, PhaseFinished, delivery.Phase)
	require.Equal(t, input, delivery.Request)
	require.Equal(t, 1, delivery.SendAttempts)
	require.Equal(t, 1, delivery.StatusChecks)
	require.Nil(t, delivery.UncertainSend)

	sends := mock.SendMessage().Calls()
	require.Len(t, sends, 1)
	require.Equal(t, messaging.SendMessageInput{To: "+14155550123", Body: "Your table is ready."}, sends[0].Input)
	reads := mock.GetMessage().Calls()
	require.Len(t, reads, 1)
	require.Equal(t, messaging.GetMessageInput{MessageSID: "SMfedcba9876543210fedcba9876543210"}, reads[0].Input)
}

func TestMessageListUsesMockWithRealDex(t *testing.T) {
	mock := messagingmock.New(t, ConnectionName)
	_, harness := newTwilioIntegrationHarnessForConnection(t, mock.Connection())
	flow := NewMessageListFlow(mock.Connection())
	ctx := integrationContext(t)
	flowID := "twilio-list-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	input := MessageListRequest(messaging.ListMessagesInput{PageSize: 50})
	_, err := harness.client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	var result dex.FlowResult
	for {
		result, err = harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
		var timeout *dex.LongPollTimeoutError
		if errors.As(err, &timeout) {
			continue
		}
		require.NoError(t, err)
		break
	}
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var page messaging.MessagePage
	require.NoError(t, result.DecodeSingleOutput(&page))
	require.Len(t, page.Messages, 1)
	require.Equal(t, "SMfedcba9876543210fedcba9876543210", page.Messages[0].SID)
	calls := mock.ListMessages().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, messaging.ListMessagesInput(input), calls[0].Input)
}
