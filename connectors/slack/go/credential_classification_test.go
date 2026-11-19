// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/slack/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

type credentialClassificationCase struct {
	name       string
	err        error
	isTerminal bool
}

func credentialClassificationCases() []credentialClassificationCase {
	return []credentialClassificationCase{
		{name: "sdk reauthorization", err: sdkgo.NewReauthorizationRequiredError(errors.New("revoked")), isTerminal: true},
		{name: "project reauthorization", err: fmt.Errorf("resolve: %w", projectconfig.ErrReauthorizationRequired), isTerminal: true},
		{name: "object not found", err: fmt.Errorf("resolve: %w", projectconfig.ErrObjectNotFound), isTerminal: true},
		{name: "connection not selected", err: fmt.Errorf("resolve: %w", projectconfig.ErrConnectionNotSelected), isTerminal: true},
		{name: "broker rejected", err: fmt.Errorf("resolve: %w", projectconfig.ErrCredentialBrokerRejected), isTerminal: true},
		{name: "broker unavailable", err: fmt.Errorf("resolve: %w", projectconfig.ErrCredentialBrokerUnavailable)},
		{name: "rejected credential not refreshed", err: projectconfig.ErrRejectedCredentialNotRefreshed},
		{name: "static not configured", err: errors.New("connection is not configured")},
		{name: "unlisted", err: errors.New("credential store hiccup")},
	}
}

func TestCredentialClassificationBeforeAnySend(t *testing.T) {
	for _, testCase := range credentialClassificationCases() {
		t.Run(testCase.name, func(t *testing.T) {
			provider := &sequencedSlackCredentials{resolutions: []slackCredentialResolution{{err: testCase.err}}}
			posts := 0
			client, err := slack.New(slack.Config{}, provider, slack.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				posts++
				return slackTestResponse(http.StatusOK, `{"ok":true,"channel":"C123","ts":"1.0"}`), nil
			})}))
			require.NoError(t, err)
			ctx := singledispatchtest.NewContext("credential-classification-first-"+testCase.name, "post")
			result, err := sdkgo.RunMutation(ctx, client.PostChannelMessage(), slackConnection, slack.PostChannelMessageInput{ChannelID: "C123", Text: "hello"})
			require.Zero(t, posts)
			if testCase.isTerminal {
				require.NoError(t, err)
				require.Equal(t, slack.PostChannelMessageBranchDefect, result.Branch)
				return
			}
			var retryError *sdkgo.RetryError
			require.ErrorAs(t, err, &retryError)
			require.Equal(t, sdkgo.FailureAvailability, retryError.Failure.Kind)
		})
	}
}

func TestCredentialClassificationAfterPossibleSend(t *testing.T) {
	validCredentials := slack.Credentials{BotToken: sdkgo.NewSecretString("bot-token"), UserToken: sdkgo.NewSecretString("user-token")}
	for _, testCase := range credentialClassificationCases() {
		t.Run(testCase.name, func(t *testing.T) {
			provider := &sequencedSlackCredentials{resolutions: []slackCredentialResolution{{credentials: validCredentials}, {err: testCase.err}}}
			posts := 0
			client, err := slack.New(slack.Config{}, provider, slack.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				posts++
				return slackTestResponse(http.StatusInternalServerError, `{"ok":false,"error":"fatal_error"}`), nil
			})}))
			require.NoError(t, err)
			ctx := singledispatchtest.NewContext("credential-classification-after-"+testCase.name, "post")
			input := slack.PostChannelMessageInput{ChannelID: "C123", Text: "hello"}
			_, err = sdkgo.RunMutation(ctx, client.PostChannelMessage(), slackConnection, input)
			var retryError *sdkgo.RetryError
			require.ErrorAs(t, err, &retryError)
			ctx.AdvanceAttempt()
			result, err := sdkgo.RunMutation(ctx, client.PostChannelMessage(), slackConnection, input)
			require.Equal(t, 1, posts)
			if testCase.isTerminal {
				require.NoError(t, err)
				require.Equal(t, slack.PostChannelMessageBranchUncertain, result.Branch)
				return
			}
			require.ErrorAs(t, err, &retryError)
			heartbeats := ctx.Heartbeats()
			require.NotContains(t, string(heartbeats[len(heartbeats)-1]), `"state":"notSent"`)
		})
	}
}

func TestCredentialClassificationForQueries(t *testing.T) {
	for _, testCase := range credentialClassificationCases() {
		t.Run(testCase.name, func(t *testing.T) {
			provider := &sequencedSlackCredentials{resolutions: []slackCredentialResolution{{err: testCase.err}}}
			client, err := slack.New(slack.Config{}, provider)
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(newSlackDexContext("credential-classification-query"), client.ListChannels(), slackConnection, slack.ListChannelsInput{})
			if testCase.isTerminal {
				require.NoError(t, err)
				require.Equal(t, slack.ListChannelsBranchDefect, result.Branch)
				return
			}
			var retryError *sdkgo.RetryError
			require.ErrorAs(t, err, &retryError)
		})
	}
}
