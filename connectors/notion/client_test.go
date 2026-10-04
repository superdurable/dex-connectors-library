// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/notion"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestNewRejectsUnsafeConfiguration(t *testing.T) {
	credentials := staticNotionCredentials()
	for _, test := range []struct {
		name   string
		config notion.Config
	}{
		{"plain HTTP to a remote host", notion.Config{Endpoint: "http://api.notion.com"}},
		{"endpoint with a query", notion.Config{Endpoint: "https://api.notion.com?token=x"}},
		{"negative response limit", notion.Config{MaxResponseBytes: -1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := notion.New(test.config, credentials)
			require.Error(t, err)
		})
	}
	_, err := notion.New(notion.Config{}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = notion.New(notion.Config{}, credentials, nil)
	require.ErrorContains(t, err, "option is nil")
	client, err := notion.New(notion.Config{}, credentials)
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestEveryRequestSendsTheBearerTokenAndTheNotionVersion(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada"))
	})
	client := newNotionClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newNotionDexContext("headers"), client.GetPage(), notionConnection,
		notion.GetPageInput{PageID: testPageID, ShouldSkipContent: true})
	require.NoError(t, err)
	require.Equal(t, notion.GetPageBranchFound, result.Branch)
	request := provider.request(0)
	require.Equal(t, "Bearer "+testAPIToken, request.authorization)
	require.Equal(t, "2026-03-11", request.version)
	require.Equal(t, notion.NotionVersion, request.version)
	require.Equal(t, "request-1", result.Receipt.ProviderRequestID)
	require.Equal(t, testPageID, result.Receipt.ProviderObjectID)
}

func TestRedirectsAreNeverFollowedWithTheToken(t *testing.T) {
	elsewhere := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada"))
	})
	provider := newRecordingNotion(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		http.Redirect(response, request, elsewhere.URL+request.URL.Path, http.StatusFound)
	})
	client := newNotionClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newNotionDexContext("redirect"), client.GetPage(), notionConnection,
		notion.GetPageInput{PageID: testPageID, ShouldSkipContent: true})
	require.NoError(t, err)
	require.Equal(t, notion.GetPageBranchInvalidResponse, result.Branch)
	require.Zero(t, elsewhere.requestCount())
}

func TestAResponseReflectingTheTokenIsNeverReturned(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, pageJSON(testPageID, testAPIToken))
	})
	client := newNotionClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newNotionDexContext("reflect"), client.GetPage(), notionConnection,
		notion.GetPageInput{PageID: testPageID, ShouldSkipContent: true})
	require.NoError(t, err)
	require.Equal(t, notion.GetPageBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
	requireNoSentinel(t, result)
}

func TestUnavailableCredentialsSelectDefectWithoutARequest(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada"))
	})
	client, err := notion.New(notion.Config{Endpoint: provider.URL}, failingCredentials{})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newNotionDexContext("credentials"), client.GetPage(), notionConnection, notion.GetPageInput{PageID: testPageID})
	require.NoError(t, err)
	require.Equal(t, notion.GetPageBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Zero(t, provider.requestCount())

	spaced, err := notion.New(notion.Config{Endpoint: provider.URL}, sdkgo.StaticCredentialProvider[notion.Credentials]{
		notionConnection: {APIToken: sdkgo.NewSecretString("ntn token with spaces")},
	})
	require.NoError(t, err)
	result, err = sdkgo.RunQuery(newNotionDexContext("spaced"), spaced.GetPage(), notionConnection, notion.GetPageInput{PageID: testPageID})
	require.NoError(t, err)
	require.Equal(t, notion.GetPageBranchDefect, result.Branch)
	require.Zero(t, provider.requestCount())
}

func TestRejectionsNameTheStatusAndCodeButNeverTheProviderMessage(t *testing.T) {
	for _, test := range []struct {
		status int
		code   string
		kind   sdkgo.FailureKind
		hint   string
	}{
		{http.StatusBadRequest, "validation_error", sdkgo.FailureValidation, "property type"},
		{http.StatusUnauthorized, "unauthorized", sdkgo.FailureAuthentication, "invalid, expired, or revoked"},
		{http.StatusForbidden, "restricted_resource", sdkgo.FailureAuthorization, "capability"},
	} {
		t.Run(test.code, func(t *testing.T) {
			provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, notionError(test.status, test.code))
			})
			client := newNotionClient(t, provider.URL)
			result, err := sdkgo.RunQuery(newNotionDexContext("reject"), client.GetPage(), notionConnection, notion.GetPageInput{PageID: testPageID})
			require.NoError(t, err)
			require.Equal(t, notion.GetPageBranchProviderRejected, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, test.code)
			require.Contains(t, result.Failure.Message, test.hint)
			requireNoSentinel(t, result)
			require.Equal(t, "request-1", result.Receipt.ProviderRequestID)
		})
	}
}

type failingCredentials struct{}

func (failingCredentials) Resolve(sdkgo.Call) (notion.Credentials, error) {
	return notion.Credentials{}, errors.New("project connection credential is unreadable")
}
