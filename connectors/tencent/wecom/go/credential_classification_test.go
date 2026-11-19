// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wecom

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestCredentialErrorClassification(t *testing.T) {
	for _, testCase := range []struct {
		err        error
		isTerminal bool
	}{
		{sdkgo.NewReauthorizationRequiredError(errors.New("revoked")), true},
		{fmt.Errorf("resolve: %w", projectconfig.ErrReauthorizationRequired), true},
		{fmt.Errorf("resolve: %w", projectconfig.ErrObjectNotFound), true},
		{fmt.Errorf("resolve: %w", projectconfig.ErrConnectionNotSelected), true},
		{fmt.Errorf("resolve: %w", projectconfig.ErrCredentialBrokerRejected), true},
		{fmt.Errorf("resolve: %w", projectconfig.ErrCredentialBrokerUnavailable), false},
		{projectconfig.ErrRejectedCredentialNotRefreshed, false},
		{errors.New("connection is not configured"), false},
		{errors.New("unlisted"), false},
	} {
		require.Equal(t, testCase.isTerminal, isTerminalCredentialError(testCase.err), testCase.err.Error())
	}
}

func TestSendGroupRobotMessageSelectsDefectForProjectReauthorization(t *testing.T) {
	fake := newFakeWeCom(t)
	client, err := New(Config{BaseURL: fake.server.URL}, failingCredentials{err: projectconfig.ErrReauthorizationRequired})
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("project-reauthorization-flow", "post-step")
	result, err := sdkgo.RunMutation(ctx, client.SendGroupRobotMessage(), testRobotConnection, testRobotText)
	require.NoError(t, err)
	require.Equal(t, SendGroupRobotMessageBranchDefect, result.Branch)
	require.Zero(t, fake.sendCount())
}
