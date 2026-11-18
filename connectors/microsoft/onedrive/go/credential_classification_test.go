// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onedrive_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// failingCredentialProvider returns one credential error for every call.
type failingCredentialProvider struct{ err error }

func (provider failingCredentialProvider) Resolve(sdkgo.Call) (onedrive.Credentials, error) {
	return onedrive.Credentials{}, provider.err
}

func (provider failingCredentialProvider) ResolveWithRefresh(context.Context, sdkgo.Call, sdkgo.CredentialRefreshDriver[onedrive.Credentials]) (onedrive.Credentials, error) {
	return onedrive.Credentials{}, provider.err
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
			graph := newGraph(t)
			client, err := onedrive.New(onedrive.Config{Endpoint: graph.URL}, failingCredentialProvider{err: testCase.err})
			require.NoError(t, err)

			created, err := sdkgo.RunMutation(newDexContext("credential-mutation"), client.CreateFolder(), graphConnection, onedrive.CreateFolderInput{DriveID: teamDriveID, Name: "Dex copies"})
			if testCase.isTerminal {
				require.NoError(t, err)
				require.Equal(t, onedrive.CreateFolderBranchDefect, created.Branch)
			} else {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
			}

			read, err := sdkgo.RunQuery(newDexContext("credential-query"), client.GetFile(), graphConnection, onedrive.GetFileInput{DriveID: teamDriveID, ItemID: "01BYE5RZ6QN3ZWBTUFOFD3GSPGOHDJD36K"})
			if testCase.isTerminal {
				require.NoError(t, err)
				require.Equal(t, onedrive.GetFileBranchDefect, read.Branch)
			} else {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
			}
			require.Empty(t, graph.Requests(), "a credential failure never reaches Graph")
		})
	}
}
