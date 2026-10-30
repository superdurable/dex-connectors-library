// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gitlablookup

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/gitlab/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestFlowUsesStaticConnectionAndStepIdentities(t *testing.T) {
	reference := sdkgo.ConnectionRef{Provider: "gitlab", Name: ConnectionName}
	client, err := gitlab.New(gitlab.Config{}, sdkgo.StaticCredentialProvider[gitlab.Credentials]{})
	require.NoError(t, err)
	connection, err := gitlab.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	_, err = dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	require.Equal(t, "GitLabLookup", dex.GetFinalFlowType(flow))
	require.Equal(t, "StartGitLabLookup", dex.GetFinalStepType[LookupRequest](startLookup{}))
	require.Equal(t, "CompleteGitLabLookup", dex.GetFinalStepType[gitlab.GetProjectResult](completeLookup{}))
	wait, err := startLookup{}.WaitFor(nil, gitlab.GetProjectInput{ProjectID: "meridian/web"})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}
