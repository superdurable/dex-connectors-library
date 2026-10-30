// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package datadoglookup

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/datadog"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestFlowUsesStaticConnectionAndStepIdentities(t *testing.T) {
	reference := sdkgo.ConnectionRef{Provider: "datadog", Name: ConnectionName}
	client, err := datadog.New(datadog.Config{}, sdkgo.StaticCredentialProvider[datadog.Credentials]{})
	require.NoError(t, err)
	connection, err := datadog.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	_, err = dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	require.Equal(t, "DatadogLookup", dex.GetFinalFlowType(flow))
	require.Equal(t, "StartDatadogLookup", dex.GetFinalStepType[LookupRequest](startLookup{}))
	require.Equal(t, "CompleteDatadogLookup", dex.GetFinalStepType[datadog.QueryMetricsResult](completeLookup{}))
	wait, err := startLookup{}.WaitFor(nil, datadog.QueryMetricsInput{From: 1761436800, To: 1761440400, Query: "system.cpu.user{*}"})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}
