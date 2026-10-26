//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textmessagedelivery

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/twilio/messaging"
	"github.com/superdurable/dex-connectors-library/connectors/twilio/messaging/messagingmock"
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
