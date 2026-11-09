// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textmessage

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/meta/whatsapp-business/go"
	"github.com/superdurable/dex-connectors-library/connectors/meta/whatsapp-business/go/whatsappbusinessmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestSendTextMessageFlowIdentityAndMapping(t *testing.T) {
	input := whatsappbusiness.SendTextMessageInput{PhoneNumberID: "1906385232743451", Recipient: "16315551234", Text: "Hello"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(whatsappbusiness.Connection{})))
	require.Equal(t, "StartSendTextMessage", dex.GetFinalStepType[whatsappbusiness.SendTextMessageInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := whatsappbusinessmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
