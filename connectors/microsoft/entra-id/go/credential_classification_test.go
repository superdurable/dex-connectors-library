// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package entraid_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	entraid "github.com/superdurable/dex-connectors-library/connectors/microsoft/entra-id/go"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/entra-id/go/internal/graphfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// failingCredentialProvider returns one credential error for every call.
type failingCredentialProvider struct{ err error }

func (provider failingCredentialProvider) Resolve(sdkgo.Call) (entraid.Credentials, error) {
	return entraid.Credentials{}, provider.err
}

func (provider failingCredentialProvider) ResolveWithRefresh(context.Context, sdkgo.Call, sdkgo.CredentialRefreshDriver[entraid.Credentials]) (entraid.Credentials, error) {
	return entraid.Credentials{}, provider.err
}

func TestEveryOperationClassifiesCredentialFailuresTheSameWay(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		isTerminal bool
	}{
		{name: "sdk reauthorization", err: sdkgo.NewReauthorizationRequiredError(errors.New("revoked")), isTerminal: true},
		{name: "project reauthorization", err: projectconfig.ErrReauthorizationRequired, isTerminal: true},
		{name: "object not found", err: projectconfig.ErrObjectNotFound, isTerminal: true},
		{name: "connection not selected", err: projectconfig.ErrConnectionNotSelected, isTerminal: true},
		{name: "broker rejected", err: projectconfig.ErrCredentialBrokerRejected, isTerminal: true},
		{name: "broker unavailable", err: projectconfig.ErrCredentialBrokerUnavailable},
		{name: "rejected credential not refreshed", err: projectconfig.ErrRejectedCredentialNotRefreshed},
		{name: "static connection not configured", err: errors.New("connection is not configured")},
		{name: "deadline", err: context.DeadlineExceeded},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			graph := graphfake.New(t.Cleanup)
			client, err := entraid.New(entraid.Config{}, failingCredentialProvider{err: testCase.err}, entraid.WithLocalProviderURL(graph.URL))
			require.NoError(t, err)
			input := entraid.AddUserToGroupInput{GroupID: "02bd9fd6-8f93-4758-87c3-1fb73740a315", UserID: "6e7b768e-07e2-4810-8459-485f84f8f204"}

			added, err := sdkgo.RunMutation(newTestDexContext("credential-mutation"), client.AddUserToGroup(), testConnection, input)
			if testCase.isTerminal {
				require.NoError(t, err)
				require.Equal(t, entraid.AddUserToGroupBranchDefect, added.Branch)
			} else {
				require.Equal(t, sdkgo.FailureAvailability, requireRetry(t, err).Kind)
			}

			read, err := sdkgo.RunQuery(newTestDexContext("credential-query"), client.GetUser(), testConnection, entraid.GetUserInput{UserKey: input.UserID})
			if testCase.isTerminal {
				require.NoError(t, err)
				require.Equal(t, entraid.GetUserBranchDefect, read.Branch)
			} else {
				require.Equal(t, sdkgo.FailureAvailability, requireRetry(t, err).Kind)
			}
			require.Empty(t, graph.Requests(), "a credential failure never reaches Graph")
		})
	}
}
