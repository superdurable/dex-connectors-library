// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookmail_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	outlookmail "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// failingCredentialProvider returns one credential error for every call.
type failingCredentialProvider struct{ err error }

func (provider failingCredentialProvider) Resolve(sdkgo.Call) (outlookmail.Credentials, error) {
	return outlookmail.Credentials{}, provider.err
}

func (provider failingCredentialProvider) ResolveWithRefresh(context.Context, sdkgo.Call, sdkgo.CredentialRefreshDriver[outlookmail.Credentials]) (outlookmail.Credentials, error) {
	return outlookmail.Credentials{}, provider.err
}

var credentialClassificationCases = []struct {
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

func newFailingCredentialClient(t *testing.T, err error) *outlookmail.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("no request is expected") }))
	t.Cleanup(server.Close)
	client, newErr := outlookmail.New(outlookmail.Config{}, failingCredentialProvider{err: err}, outlookmail.WithLocalProviderURL(server.URL))
	require.NoError(t, newErr)
	return client
}

func TestSendMessageClassifiesEveryCredentialFailure(t *testing.T) {
	for _, testCase := range credentialClassificationCases {
		t.Run(testCase.name, func(t *testing.T) {
			client := newFailingCredentialClient(t, testCase.err)
			result, err := sdkgo.RunMutation(newOutlookDexContext("credential-first"), client.SendMessage(), outlookConnection, validSendMessageInput())
			if !testCase.isTerminal {
				requireRetry(t, err, sdkgo.FailureAvailability)
			} else {
				require.NoError(t, err)
				require.Equal(t, outlookmail.SendMessageBranchProviderRejected, result.Branch)
			}

			afterPossibleSend := newOutlookDexContext("credential-after-send")
			afterPossibleSend.attempt = 2
			afterPossibleSend.previousHeartbeat = json.RawMessage(`{"outlookMailSendPhase":"sendDispatched","outlookMailDraftMessageId":"AAMkAD-draft","outlookMailSendCount":1}`)
			result, err = sdkgo.RunMutation(afterPossibleSend, client.SendMessage(), outlookConnection, validSendMessageInput())
			if !testCase.isTerminal {
				requireRetry(t, err, sdkgo.FailureAvailability)
				return
			}
			require.NoError(t, err)
			require.Equal(t, outlookmail.SendMessageBranchUncertain, result.Branch)
		})
	}
}

func TestMoveMessageClassifiesEveryCredentialFailure(t *testing.T) {
	for _, testCase := range credentialClassificationCases {
		t.Run(testCase.name, func(t *testing.T) {
			client := newFailingCredentialClient(t, testCase.err)
			result, err := sdkgo.RunMutation(newOutlookDexContext("move-credential"), client.MoveMessage(), outlookConnection,
				outlookmail.MoveMessageInput{MessageID: "AAMkAD-message", DestinationFolder: "Archive"})
			if !testCase.isTerminal {
				requireRetry(t, err, sdkgo.FailureAvailability)
				return
			}
			require.NoError(t, err)
			require.Equal(t, outlookmail.MoveMessageBranchProviderRejected, result.Branch)
		})
	}
}
