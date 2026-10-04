// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package messaging_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/twilio/messaging"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type sendMessageTarget struct {
	dex.StepDefaultsNoWaitFor[messaging.SendMessageResult]
}

func (sendMessageTarget) Execute(dex.Context, messaging.SendMessageResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

type getMessageTarget struct {
	dex.StepDefaultsNoWaitFor[messaging.GetMessageResult]
}

func (getMessageTarget) Execute(dex.Context, messaging.GetMessageResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func TestFactoriesRequireOnlyTheHappyPathBranch(t *testing.T) {
	connection := newTestConnection(t)
	annotations := sdkgo.StepAnnotations{GroupID: "twilio", GroupLabel: "Twilio", Explanation: "Send a text."}
	require.NotPanics(t, func() {
		messaging.NewSendMessageStep(messaging.SendMessageStepConfig[string]{
			StepType: "SendText", Annotations: annotations, Connection: connection, ConnectionName: twilioConnection.Name,
			MapToOperationInput: func(string) messaging.SendMessageInput { return validSendInput() },
			Accepted:            sdkgo.GoTo(sendMessageTarget{}),
		})
		messaging.NewSendMessageStep(messaging.SendMessageStepConfig[string]{
			StepType: "SendTextWithRecovery", Annotations: annotations, Connection: connection, ConnectionName: twilioConnection.Name,
			MapToOperationInput: func(string) messaging.SendMessageInput { return validSendInput() },
			Accepted:            sdkgo.GoTo(sendMessageTarget{}), ProviderRejected: sdkgo.GoTo(sendMessageTarget{}),
			Uncertain: sdkgo.GoTo(sendMessageTarget{}), Defect: sdkgo.GoTo(sendMessageTarget{}),
		})
		messaging.NewGetMessageStep(messaging.GetMessageStepConfig[string]{
			StepType: "ReadText", Annotations: annotations, Connection: connection, ConnectionName: twilioConnection.Name,
			MapToOperationInput: func(messageSID string) messaging.GetMessageInput {
				return messaging.GetMessageInput{MessageSID: messageSID}
			},
			Found: sdkgo.GoTo(getMessageTarget{}),
		})
	})
	require.Panics(t, func() {
		messaging.NewSendMessageStep(messaging.SendMessageStepConfig[string]{
			StepType: "SendText", Annotations: annotations, Connection: connection, ConnectionName: twilioConnection.Name,
			MapToOperationInput: func(string) messaging.SendMessageInput { return validSendInput() },
			Uncertain:           sdkgo.GoTo(sendMessageTarget{}),
		})
	}, "accepted is the required branch")
	require.Panics(t, func() {
		messaging.NewGetMessageStep(messaging.GetMessageStepConfig[string]{
			StepType: "ReadText", Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: func(string) messaging.GetMessageInput { return messaging.GetMessageInput{} },
			Found:               sdkgo.GoTo(getMessageTarget{}),
		})
	}, "the static ConnectionName must match the runtime connection")
}

func TestSendMessageDeclaresUncertaintyAndSyncDurability(t *testing.T) {
	branches := map[sdkgo.BranchID]bool{}
	for _, branch := range messaging.SendMessageDefinition.Branches {
		branches[branch.ID] = branch.Optional
	}
	require.Equal(t, map[sdkgo.BranchID]bool{
		messaging.SendMessageBranchAccepted: false, messaging.SendMessageBranchProviderRejected: true,
		messaging.SendMessageBranchUncertain: true, messaging.SendMessageBranchDefect: true,
	}, branches)
	require.Equal(t, dex.StepDurabilitySync, messaging.SendMessageDefinition.StepDefaults.ExecuteDurability,
		"an async fallback attempt would re-send a request that outlasts the local phase")
	require.Equal(t, dex.StepDurabilityAsync, messaging.GetMessageDefinition.StepDefaults.ExecuteDurability)
	require.Greater(t, messaging.SendMessageDefinition.StepDefaults.ExecuteMethodTimeout, 20*time.Second,
		"the connector's 20-second request bound must expire before the Execute timeout")
}

func TestConnectionAndCredentialsNeverRevealSecrets(t *testing.T) {
	connection := newTestConnection(t)
	_, err := json.Marshal(connection)
	require.Error(t, err)
	require.NotContains(t, err.Error(), testAuthToken)
	require.NotContains(t, fmt.Sprintf("%v %#v", connection, connection), testAuthToken)
	credentials := messaging.Credentials{
		AuthMethodID: messaging.AuthTokenAuthMethodID, AuthToken: sdkgo.NewSecretString(testAuthToken),
		APIKeySecret: sdkgo.NewSecretString(testAPIKeySecret),
	}
	rendered := fmt.Sprintf("%v %+v %#v", credentials, credentials, credentials)
	require.NotContains(t, rendered, testAuthToken)
	require.NotContains(t, rendered, testAPIKeySecret)
}

func newTestConnection(t *testing.T) messaging.Connection {
	t.Helper()
	client := newTwilioClient(t, "https://api.twilio.test/2010-04-01", messaging.Config{DefaultSender: testSender}, authTokenCredentials())
	connection, err := messaging.NewConnection(client, twilioConnection)
	require.NoError(t, err)
	return connection
}
