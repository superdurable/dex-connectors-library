// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	gmail "github.com/superdurable/dex-connectors-library/connectors/google/gmail"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type gmailTarget struct {
	dex.StepDefaultsNoWaitFor[gmail.SendMessageStepOutput[string]]
}

func (gmailTarget) Execute(dex.Context, gmail.SendMessageStepOutput[string]) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func TestSendFactoryRequiresEveryTypedBranch(t *testing.T) {
	client := newGmailClient(t, "http://127.0.0.1:1")
	connection, err := gmail.NewConnection(client, gmailConnection)
	require.NoError(t, err)
	require.Panics(t, func() {
		gmail.NewSendMessageStep(gmail.SendMessageStepConfig[string]{
			StepType: "Send", Presentation: sdkgo.StepPresentation{GroupID: "google", GroupLabel: "Google", Explanation: "Send a message."},
			Connection: connection, ConnectionName: "gmail-send",
			BuildInput: func(string) (gmail.SendMessageInput, error) { return gmail.SendMessageInput{}, nil },
			Sent:       sdkgo.GoTo(gmailTarget{}), Rejected: sdkgo.GoTo(gmailTarget{}), Uncertain: sdkgo.GoTo(gmailTarget{}),
		})
	})
}

func TestSendFactoryRejectsConnectionNameMismatch(t *testing.T) {
	client := newGmailClient(t, "http://127.0.0.1:1")
	connection, err := gmail.NewConnection(client, gmailConnection)
	require.NoError(t, err)
	require.Panics(t, func() {
		gmail.NewSendMessageStep(gmail.SendMessageStepConfig[string]{
			StepType: "Send", Presentation: sdkgo.StepPresentation{GroupID: "google", GroupLabel: "Google", Explanation: "Send a message."},
			Connection: connection, ConnectionName: "different",
			BuildInput: func(string) (gmail.SendMessageInput, error) { return gmail.SendMessageInput{}, nil },
		})
	})
}

func TestGmailConnectionCannotBeSerialized(t *testing.T) {
	client := newGmailClient(t, "http://127.0.0.1:1")
	connection, err := gmail.NewConnection(client, gmailConnection)
	require.NoError(t, err)
	_, err = json.Marshal(connection)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "gmail-token")
}
