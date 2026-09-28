// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package authenticatedprofile

import (
	"testing"

	"github.com/stretchr/testify/require"
	linkedinconnector "github.com/superdurable/dex-connectors-library/connectors/linkedin"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestMapToGetAuthenticatedProfileInputReturnsTheEmptyProviderInput(t *testing.T) {
	require.Equal(t, linkedinconnector.GetAuthenticatedProfileInput{}, NewFlow(linkedinconnector.Connection{}).MapToGetAuthenticatedProfileInput(Input{RequestLabel: "signup"}))
}

func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(linkedinconnector.Connection{})))
	require.Equal(t, recordProfileRequestStepType, dex.GetFinalStepType[Input](recordProfileRequest{}))
	require.Equal(t, completeProfileStepType, dex.GetFinalStepType[linkedinconnector.GetAuthenticatedProfileResult](completeProfile{}))
	wait, err := recordProfileRequest{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	client, err := linkedinconnector.New(linkedinconnector.Config{}, sdkgo.StaticCredentialProvider[linkedinconnector.Credentials]{})
	require.NoError(t, err)
	connection, err := linkedinconnector.NewConnection(client, sdkgo.ConnectionRef{Provider: "linkedin", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)

	otherConnection, err := linkedinconnector.NewConnection(client, sdkgo.ConnectionRef{Provider: "linkedin", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(otherConnection)}) })
}
