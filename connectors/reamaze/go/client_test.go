// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reamaze_test

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/reamaze"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestNewValidatesTheBrandSubdomainBeforeBuildingAnyURL(t *testing.T) {
	for _, brand := range []string{"", "Acme", "acme.reamaze.io", "https://acme", "acme/../evil", "-acme", "acme-", "acme_corp", "ac me", strings.Repeat("a", 64)} {
		_, err := reamaze.New(reamaze.Config{Brand: brand}, testCredentialProvider())
		require.Error(t, err, "brand %q", brand)
	}
	for _, brand := range []string{"acme", "acme-support", "a", "brand2"} {
		_, err := reamaze.New(reamaze.Config{Brand: brand}, testCredentialProvider())
		require.NoError(t, err, "brand %q", brand)
	}
	_, err := reamaze.New(reamaze.Config{Brand: "acme"}, nil)
	require.Error(t, err)
	_, err = reamaze.New(reamaze.Config{Brand: "acme", MaxResponseBytes: -1}, testCredentialProvider())
	require.Error(t, err)
	_, err = reamaze.New(reamaze.Config{Brand: "acme"}, testCredentialProvider(), reamaze.WithAPIBaseURL("http://example.com/api/v1"))
	require.Error(t, err, "a non-loopback override must use HTTPS")
}

func TestRequestsUseHTTPBasicEmailAndTokenWithJSONAccept(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, pageJSON("conversations", []any{}, 0))
	})
	result, err := sdkgo.RunQuery(newReamazeDexContext("auth"), newReamazeClient(t, provider.URL).SearchConversations(), reamazeConnection, reamaze.SearchConversationsInput{})
	require.NoError(t, err)
	require.Equal(t, reamaze.SearchConversationsBranchSearched, result.Branch)
	request := provider.request(0)
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte(testEmail+":"+testAPIToken))
	require.Equal(t, expected, request.header.Get("Authorization"))
	require.Equal(t, "application/json", request.header.Get("Accept"))
	require.Empty(t, request.header.Get("Content-Type"), "a read sends no body")
	require.Equal(t, "/api/v1/conversations", request.path)
	require.Equal(t, "request-0001", result.Receipt.ProviderRequestID)
	require.Equal(t, "reamaze", result.Receipt.Provider)
}

func TestInvalidCredentialsSelectDefectWithoutARequest(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, pageJSON("conversations", []any{}, 0))
	})
	for name, credentials := range map[string]reamaze.Credentials{
		"display address": {Email: "Agent <agent@acme.example.com>", APIToken: sdkgo.NewSecretString(testAPIToken)},
		"colon in email":  {Email: "agent:x@acme.example.com", APIToken: sdkgo.NewSecretString(testAPIToken)},
		"space in token":  {Email: testEmail, APIToken: sdkgo.NewSecretString("token with space")},
		"missing token":   {Email: testEmail},
	} {
		client, err := reamaze.New(reamaze.Config{Brand: testBrand},
			sdkgo.StaticCredentialProvider[reamaze.Credentials]{reamazeConnection: credentials}, reamaze.WithAPIBaseURL(provider.URL+"/api/v1"))
		require.NoError(t, err)
		result, err := sdkgo.RunQuery(newReamazeDexContext("credentials"), client.SearchConversations(), reamazeConnection, reamaze.SearchConversationsInput{})
		require.NoError(t, err, name)
		require.Equal(t, reamaze.SearchConversationsBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind, name)
		require.NotContains(t, result.Failure.Message, testAPIToken)
	}
	require.Zero(t, provider.requestCount())
}

func TestFailureStatusesMapToBranchesWithoutProviderText(t *testing.T) {
	for _, test := range []struct {
		status int
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
	}{
		{status: http.StatusUnauthorized, branch: reamaze.GetConversationBranchProviderRejected, kind: sdkgo.FailureAuthentication},
		{status: http.StatusForbidden, branch: reamaze.GetConversationBranchProviderRejected, kind: sdkgo.FailureAuthorization},
		{status: http.StatusNotFound, branch: reamaze.GetConversationBranchNotFound, kind: sdkgo.FailureNotFound},
		{status: http.StatusUnprocessableEntity, branch: reamaze.GetConversationBranchProviderRejected, kind: sdkgo.FailureValidation},
		{status: http.StatusFound, branch: reamaze.GetConversationBranchProviderRejected, kind: sdkgo.FailureProtocol},
	} {
		provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
			if test.status == http.StatusFound {
				response.Header().Set("Location", "https://evil.example.com/collect")
			}
			writeJSON(t, response, test.status, `{"error":"SENTINEL `+testAPIToken+`","errors":{"status":["SENTINEL"]}}`)
		})
		result, err := sdkgo.RunQuery(newReamazeDexContext("failure"), newReamazeClient(t, provider.URL).GetConversation(), reamazeConnection,
			reamaze.GetConversationInput{ConversationID: "double-charge"})
		require.NoError(t, err)
		require.Equal(t, test.branch, result.Branch, "HTTP %d", test.status)
		require.Equal(t, test.kind, result.Failure.Kind, "HTTP %d", test.status)
		require.NotContains(t, result.Failure.Message, "SENTINEL")
		require.NotContains(t, result.Failure.Message, testAPIToken)
		require.Equal(t, 1, provider.requestCount(), "redirects are never followed")
	}
}

func TestRetryableStatusesReturnRetryWithRetryAfter(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusRequestTimeout, http.StatusInternalServerError, http.StatusServiceUnavailable} {
		provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
			response.Header().Set("Retry-After", "7")
			writeJSON(t, response, status, `{"error":"SENTINEL"}`)
		})
		_, err := sdkgo.RunQuery(newReamazeDexContext("retry"), newReamazeClient(t, provider.URL).SearchConversations(), reamazeConnection, reamaze.SearchConversationsInput{})
		require.Error(t, err, "HTTP %d", status)
		require.NotContains(t, err.Error(), "SENTINEL")
		var retryAfter *dex.RetryAfterError
		require.ErrorAs(t, err, &retryAfter, "HTTP %d honors Retry-After", status)
		require.Equal(t, 7*time.Second, retryAfter.After)
	}
	client, err := reamaze.New(reamaze.Config{Brand: testBrand}, testCredentialProvider(), reamaze.WithAPIBaseURL(closedLoopbackURL(t)),
		reamaze.WithHTTPClient(&http.Client{Timeout: 2 * time.Second}))
	require.NoError(t, err)
	_, err = sdkgo.RunQuery(newReamazeDexContext("refused"), client.SearchConversations(), reamazeConnection, reamaze.SearchConversationsInput{})
	require.Error(t, err, "a refused connection is retried")
}

func TestRateLimitWithoutRetryAfterWaitsOneMinute(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusTooManyRequests, `{"error":"SENTINEL"}`)
	})
	_, err := sdkgo.RunQuery(newReamazeDexContext("rate-limit"), newReamazeClient(t, provider.URL).SearchConversations(), reamazeConnection, reamaze.SearchConversationsInput{})
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter, "Re:amaze documents a per-minute limit")
	require.Equal(t, time.Minute, retryAfter.After)
}

func TestCredentialReadFailuresRetryAndUnusableCredentialsSelectDefect(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, pageJSON("conversations", []any{}, 0))
	})
	client, credentials := newSwitchingReamazeClient(t, provider.URL)
	credentials.resolveErr = errors.New("project object read failed")
	_, err := sdkgo.RunQuery(newReamazeDexContext("credential-outage"), client.SearchConversations(), reamazeConnection, reamaze.SearchConversationsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "a credential store outage is retried")
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	_, err = sdkgo.RunMutation(newReamazeDexContext("credential-outage-update"), client.UpdateConversation(), reamazeConnection,
		reamaze.UpdateConversationInput{ConversationID: "double-charge", AddTags: []string{"vip"}})
	require.ErrorAs(t, err, &retry, "nothing was sent, so an update is retried too")

	credentials.resolveErr = sdkgo.ErrReauthorizationRequired
	result, err := sdkgo.RunQuery(newReamazeDexContext("credential-revoked"), client.SearchConversations(), reamazeConnection, reamaze.SearchConversationsInput{})
	require.NoError(t, err)
	require.Equal(t, reamaze.SearchConversationsBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Zero(t, provider.requestCount())
}

func TestOversizedOrCredentialReflectingResponsesAreInvalid(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusOK, `{"page_count":1,"total_count":1,"conversations":[],"padding":"`+strings.Repeat("x", 2048)+`"}`)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"page_count":1,"total_count":1,"conversations":[],"echo":"`+testAPIToken+`"}`)
	})
	client, err := reamaze.New(reamaze.Config{Brand: testBrand, MaxResponseBytes: 1024}, testCredentialProvider(), reamaze.WithAPIBaseURL(provider.URL+"/api/v1"))
	require.NoError(t, err)
	for _, kind := range []sdkgo.FailureKind{sdkgo.FailureResponseTooLarge, sdkgo.FailureProtocol} {
		result, err := sdkgo.RunQuery(newReamazeDexContext("invalid"), client.SearchConversations(), reamazeConnection, reamaze.SearchConversationsInput{})
		require.NoError(t, err)
		require.Equal(t, reamaze.SearchConversationsBranchInvalidResponse, result.Branch)
		require.Equal(t, kind, result.Failure.Kind)
		require.NotContains(t, result.Failure.Message, testAPIToken)
	}
}
