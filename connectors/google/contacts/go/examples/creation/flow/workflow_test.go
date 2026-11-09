// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package creation

import (
	"testing"

	"github.com/stretchr/testify/require"
	contacts "github.com/superdurable/dex-connectors-library/connectors/google/contacts/go"
	"github.com/superdurable/dex-connectors-library/connectors/google/contacts/go/contactsmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestCreationFlowIdentityAndMapping(t *testing.T) {
	input := contacts.CreateContactInput{GivenName: "Example", FamilyName: "Contact", EmailAddress: "example@example.com"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(contacts.Connection{})))
	require.Equal(t, "StartCreateContact", dex.GetFinalStepType[contacts.CreateContactInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := contactsmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
