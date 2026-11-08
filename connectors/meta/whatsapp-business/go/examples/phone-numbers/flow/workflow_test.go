// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package phonenumbers

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/meta/whatsapp-business/go"
	"github.com/superdurable/dex-connectors-library/connectors/meta/whatsapp-business/go/whatsappbusinessmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestPhoneNumbersFlowIdentityAndMapping(t *testing.T) {
	input := whatsappbusiness.ListPhoneNumbersInput{BusinessAccountID: "123456789"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(whatsappbusiness.Connection{})))
	require.Equal(t, "StartListPhoneNumbers", dex.GetFinalStepType[whatsappbusiness.ListPhoneNumbersInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := whatsappbusinessmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
