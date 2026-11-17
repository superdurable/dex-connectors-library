// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package discord_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/discord/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGetMessageReadsExactMessageAndClassifiesFailures(t *testing.T) {
	for name, testCase := range map[string]struct {
		status int
		body   string
		branch sdkgo.BranchID
	}{
		"found":         {http.StatusOK, messageJSON(testMessageID, "release"), discord.GetMessageBranchFound},
		"missing":       {http.StatusNotFound, `{"code":10008,"message":"SENTINEL"}`, discord.GetMessageBranchNotFound},
		"denied":        {http.StatusForbidden, `{"code":50013,"message":"SENTINEL"}`, discord.GetMessageBranchProviderRejected},
		"wrong message": {http.StatusOK, messageJSON("100000000000000001", "release"), discord.GetMessageBranchInvalidResponse},
		"malformed":     {http.StatusOK, `{`, discord.GetMessageBranchInvalidResponse},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingDiscord(t, func(response http.ResponseWriter, _ recordedRequest) {
				writeJSON(t, response, testCase.status, testCase.body)
			})
			client := newTestClient(t, provider.URL, discord.Config{})
			result, err := sdkgo.RunQuery(newStepDexContext("get-message-"+name), client.GetMessage(), discordConnection, discord.GetMessageInput{ChannelID: testChannelID, MessageID: testMessageID})
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			require.Equal(t, http.MethodGet, provider.recordedRequests()[0].method)
			require.Equal(t, "/channels/"+testChannelID+"/messages/"+testMessageID, provider.recordedRequests()[0].path)
			if result.Failure != nil {
				requireSecretSafeFailure(t, result.Failure)
			} else {
				require.Equal(t, testMessageID, result.Value.ID)
			}
		})
	}
}

func TestDeleteMessageReturns204AndClassifiesFailures(t *testing.T) {
	for name, testCase := range map[string]struct {
		status int
		body   string
		branch sdkgo.BranchID
	}{
		"deleted": {http.StatusNoContent, "", discord.DeleteMessageBranchDeleted},
		"missing": {http.StatusNotFound, `{"code":10008,"message":"SENTINEL"}`, discord.DeleteMessageBranchNotFound},
		"denied":  {http.StatusForbidden, `{"code":50013,"message":"SENTINEL"}`, discord.DeleteMessageBranchProviderRejected},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingDiscord(t, func(response http.ResponseWriter, _ recordedRequest) {
				writeJSON(t, response, testCase.status, testCase.body)
			})
			client := newTestClient(t, provider.URL, discord.Config{})
			result, err := sdkgo.RunMutation(newStepDexContext("delete-message-"+name), client.DeleteMessage(), discordConnection, discord.DeleteMessageInput{ChannelID: testChannelID, MessageID: testMessageID})
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			require.Equal(t, http.MethodDelete, provider.recordedRequests()[0].method)
			require.Equal(t, "/channels/"+testChannelID+"/messages/"+testMessageID, provider.recordedRequests()[0].path)
			if result.Failure != nil {
				requireSecretSafeFailure(t, result.Failure)
			} else {
				require.Equal(t, testMessageID, result.Value.MessageID)
			}
		})
	}
}

func TestMessageOperationsRetryRateLimitAndUnknownOutcome(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusInternalServerError} {
		provider := newRecordingDiscord(t, func(response http.ResponseWriter, _ recordedRequest) {
			writeJSON(t, response, status, `{"message":"SENTINEL"}`)
		})
		client := newTestClient(t, provider.URL, discord.Config{})
		_, err := sdkgo.RunQuery(newStepDexContext("get-message-retry"), client.GetMessage(), discordConnection, discord.GetMessageInput{ChannelID: testChannelID, MessageID: testMessageID})
		_, failure := requireRetry(t, err)
		requireSecretSafeFailure(t, &failure)
		_, err = sdkgo.RunMutation(newStepDexContext("delete-message-retry"), client.DeleteMessage(), discordConnection, discord.DeleteMessageInput{ChannelID: testChannelID, MessageID: testMessageID})
		_, failure = requireRetry(t, err)
		requireSecretSafeFailure(t, &failure)
	}
}
