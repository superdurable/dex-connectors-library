// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contacts

import (
	"testing"

	"github.com/stretchr/testify/require"
	contacts "github.com/superdurable/dex-connectors-library/connectors/google/contacts/go"
	"github.com/superdurable/dex-connectors-library/connectors/google/contacts/go/contactsmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestContactsFlowIdentityAndMapping(t *testing.T) {
	input := contacts.ListContactsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(contacts.Connection{})))
	require.Equal(t, "StartListContacts", dex.GetFinalStepType[contacts.ListContactsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := contactsmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}

func TestContactUpdateFlowIdentityAndRegistration(t *testing.T) {
	input := contacts.UpdateContactInput{ResourceName: "people/c123", Etag: "etag-1", GivenName: "Ada"}
	require.Equal(t, input, mapUpdateInput(input))
	mock := contactsmock.New(t, ConnectionName)
	flow := NewUpdateFlow(mock.Connection())
	require.Equal(t, UpdateFlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, "StartUpdateContact", dex.GetFinalStepType[contacts.UpdateContactInput](startContactUpdate{}))
	_, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
}
