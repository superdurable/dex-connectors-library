// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cloudflarelookup

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/cloudflare/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestFlowUsesStaticConnectionAndStepIdentities(t *testing.T) {
	reference := sdkgo.ConnectionRef{Provider: "cloudflare", Name: ConnectionName}
	client, err := cloudflare.New(cloudflare.Config{}, sdkgo.StaticCredentialProvider[cloudflare.Credentials]{})
	require.NoError(t, err)
	connection, err := cloudflare.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	_, err = dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	require.Equal(t, "CloudflareLookup", dex.GetFinalFlowType(flow))
	require.Equal(t, "StartCloudflareLookup", dex.GetFinalStepType[LookupRequest](startLookup{}))
	require.Equal(t, "CompleteCloudflareLookup", dex.GetFinalStepType[cloudflare.ListZonesResult](completeLookup{}))
	wait, err := startLookup{}.WaitFor(nil, cloudflare.ListZonesInput{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}
