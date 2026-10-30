// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package netlifylookup

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/netlify/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestFlowUsesStaticConnectionAndStepIdentities(t *testing.T) {
	reference := sdkgo.ConnectionRef{Provider: "netlify", Name: ConnectionName}
	client, err := netlify.New(netlify.Config{}, sdkgo.StaticCredentialProvider[netlify.Credentials]{})
	require.NoError(t, err)
	connection, err := netlify.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	_, err = dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	require.Equal(t, "NetlifyLookup", dex.GetFinalFlowType(flow))
	require.Equal(t, "StartNetlifyLookup", dex.GetFinalStepType[LookupRequest](startLookup{}))
	require.Equal(t, "CompleteNetlifyLookup", dex.GetFinalStepType[netlify.ListSitesResult](completeLookup{}))
	wait, err := startLookup{}.WaitFor(nil, netlify.ListSitesInput{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}
