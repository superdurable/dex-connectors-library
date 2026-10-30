// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package vercellookup

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/vercel/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestFlowUsesStaticConnectionAndStepIdentities(t *testing.T) {
	reference := sdkgo.ConnectionRef{Provider: "vercel", Name: ConnectionName}
	client, err := vercel.New(vercel.Config{}, sdkgo.StaticCredentialProvider[vercel.Credentials]{})
	require.NoError(t, err)
	connection, err := vercel.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	_, err = dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	require.Equal(t, "VercelLookup", dex.GetFinalFlowType(flow))
	require.Equal(t, "StartVercelLookup", dex.GetFinalStepType[LookupRequest](startLookup{}))
	require.Equal(t, "CompleteVercelLookup", dex.GetFinalStepType[vercel.ListDeploymentsResult](completeLookup{}))
	wait, err := startLookup{}.WaitFor(nil, vercel.ListDeploymentsInput{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}
