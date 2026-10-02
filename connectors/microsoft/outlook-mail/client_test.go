// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookmail_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	outlookmail "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail/internal/graphtest"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[outlookmail.Credentials]{}
	for _, test := range []struct {
		name    string
		config  outlookmail.Config
		options []outlookmail.Option
	}{
		{"negative response limit", outlookmail.Config{MaxResponseBytes: -1}, nil},
		{"mailbox with a path", outlookmail.Config{Mailbox: "support@contoso.example/../admin"}, nil},
		{"mailbox with a display name", outlookmail.Config{Mailbox: "Support <support@contoso.example>"}, nil},
		{"non-loopback local provider", outlookmail.Config{}, []outlookmail.Option{outlookmail.WithLocalProviderURL("https://graph.example.com")}},
		{"local provider with a path", outlookmail.Config{}, []outlookmail.Option{outlookmail.WithLocalProviderURL("http://127.0.0.1:9/v1.0")}},
		{"nil option", outlookmail.Config{}, []outlookmail.Option{nil}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := outlookmail.New(test.config, credentials, test.options...)
			require.Error(t, err)
		})
	}
	_, err := outlookmail.New(outlookmail.Config{}, nil)
	require.Error(t, err)
	_, err = outlookmail.New(outlookmail.Config{Mailbox: "6b0e6a3c-1d2e-4f5a-9b8c-7d6e5f4a3b2c"}, credentials)
	require.NoError(t, err, "an Entra object ID names a mailbox")
}

func TestGraphAnswersMapToBranchesWithoutServerText(t *testing.T) {
	for _, test := range []struct {
		name       string
		fault      graphtest.Fault
		branch     sdkgo.BranchID
		kind       sdkgo.FailureKind
		isRetry    bool
		retryAfter time.Duration
	}{
		{name: "throttled", fault: graphtest.Fault{Status: http.StatusTooManyRequests, ErrorCode: "ApplicationThrottled", RetryAfter: "12"},
			kind: sdkgo.FailureRateLimit, isRetry: true, retryAfter: 12 * time.Second},
		{name: "unavailable", fault: graphtest.Fault{Status: http.StatusServiceUnavailable, RetryAfter: "3"}, kind: sdkgo.FailureAvailability, isRetry: true, retryAfter: 3 * time.Second},
		{name: "gateway timeout", fault: graphtest.Fault{Status: http.StatusGatewayTimeout}, kind: sdkgo.FailureAvailability, isRetry: true},
		{name: "dropped", fault: graphtest.Fault{ShouldDropConnection: true}, kind: sdkgo.FailureTransport, isRetry: true},
		{name: "access denied", fault: graphtest.Fault{Status: http.StatusForbidden, ErrorCode: "ErrorAccessDenied"},
			branch: outlookmail.GetMessageBranchProviderRejected, kind: sdkgo.FailureAuthorization},
		{name: "missing message", fault: graphtest.Fault{Status: http.StatusNotFound, ErrorCode: "ErrorItemNotFound"},
			branch: outlookmail.GetMessageBranchNotFound, kind: sdkgo.FailureNotFound},
		{name: "malformed id", fault: graphtest.Fault{Status: http.StatusBadRequest, ErrorCode: "ErrorInvalidIdMalformed"},
			branch: outlookmail.GetMessageBranchNotFound, kind: sdkgo.FailureNotFound},
		{name: "mailbox without REST", fault: graphtest.Fault{Status: http.StatusNotFound, ErrorCode: "MailboxNotEnabledForRESTAPI"},
			branch: outlookmail.GetMessageBranchProviderRejected, kind: sdkgo.FailureNotFound},
		{name: "mailbox full", fault: graphtest.Fault{Status: http.StatusInsufficientStorage, ErrorCode: "ErrorQuotaExceeded"},
			branch: outlookmail.GetMessageBranchProviderRejected, kind: sdkgo.FailureQuotaExhausted},
		{name: "not implemented", fault: graphtest.Fault{Status: http.StatusNotImplemented},
			branch: outlookmail.GetMessageBranchProviderRejected, kind: sdkgo.FailureProviderRejection},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newGraphFake(t)
			id := seedCustomerMessage(fake, "Refund", searchBase)
			test.fault.Endpoint = graphtest.EndpointGetMessage
			fake.InjectFault(test.fault)
			client, _ := delegatedClient(t, fake)
			result, err := sdkgo.RunQuery(newOutlookDexContext("map-"+test.name), client.GetMessage(), outlookConnection, outlookmail.GetMessageInput{MessageID: id})
			if test.isRetry {
				message, delay := requireRetry(t, err, test.kind)
				require.NotContains(t, message, graphtest.SentinelText)
				require.Equal(t, test.retryAfter, delay)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, test.fault.ErrorCode)
			require.Equal(t, "0d5e3b4f-1a2b-4c3d-8e9f-001122334455", result.Receipt.ProviderRequestID)
			requireNoSecretsOrServerText(t, result.Failure)
		})
	}
}

func TestRejectedAccessTokenIsRefreshedOnceAndTheRotatedTokenPersisted(t *testing.T) {
	fake := newGraphFake(t)
	id := seedCustomerMessage(fake, "Refund", searchBase)
	client, path := delegatedClient(t, fake)
	fake.ExpireAccessTokens()
	result, err := sdkgo.RunQuery(newOutlookDexContext("expired"), client.GetMessage(), outlookConnection, outlookmail.GetMessageInput{MessageID: id})
	require.NoError(t, err)
	require.Equal(t, outlookmail.GetMessageBranchFound, result.Branch, result.Failure)
	require.Equal(t, 2, fake.RequestCount(graphtest.EndpointGetMessage), "the 401 is answered by one refresh and one resend")
	require.Equal(t, 1, fake.RequestCount(graphtest.EndpointDelegatedToken))
	stored, status := readStoredCredentials(t, path)
	require.Empty(t, status)
	require.Equal(t, fake.CurrentRefreshToken(), stored["refresh_token"], "Microsoft's rotated refresh token replaces the old one")
	require.NotEqual(t, testRefreshToken, stored["refresh_token"])

	fake.ExpireAccessTokens()
	fake.InjectFault(graphtest.Fault{Endpoint: graphtest.EndpointGetMessage, Count: 2, Status: http.StatusUnauthorized, ErrorCode: "InvalidAuthenticationToken"})
	result, err = sdkgo.RunQuery(newOutlookDexContext("rejected-twice"), client.GetMessage(), outlookConnection, outlookmail.GetMessageInput{MessageID: id})
	require.NoError(t, err)
	require.Equal(t, outlookmail.GetMessageBranchProviderRejected, result.Branch, "a second 401 is terminal without a refresh loop")
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Equal(t, 2, fake.RequestCount(graphtest.EndpointDelegatedToken))
}

func TestAppOnlyConnectionRequestsATokenAndUsesTheConfiguredMailbox(t *testing.T) {
	fake := newGraphFake(t)
	seedCustomerMessage(fake, "Refund", searchBase)
	client, path := appOnlyClient(t, fake, testMailbox)
	result, err := sdkgo.RunQuery(newOutlookDexContext("app-only"), client.SearchMessages(), outlookConnection, outlookmail.SearchMessagesInput{})
	require.NoError(t, err)
	require.Equal(t, outlookmail.SearchMessagesBranchSearched, result.Branch, result.Failure)
	require.Len(t, result.Value.Messages, 1)
	require.Equal(t, "/v1.0/users/"+testMailbox+"/mailFolders/inbox/messages", fake.Requests(graphtest.EndpointListFolderMessages)[0].Path)
	tokenRequest := fake.Requests(graphtest.EndpointAppOnlyToken)[0]
	require.Equal(t, "/"+testTenantID+"/oauth2/v2.0/token", tokenRequest.Path)
	stored, _ := readStoredCredentials(t, path)
	require.NotEmpty(t, stored["access_token"], "the application stores the token it requested")
	require.Nil(t, stored["refresh_token"])

	_, err = sdkgo.RunQuery(newOutlookDexContext("app-only-again"), client.SearchMessages(), outlookConnection, outlookmail.SearchMessagesInput{})
	require.NoError(t, err)
	require.Equal(t, 1, fake.RequestCount(graphtest.EndpointAppOnlyToken), "a stored unexpired token is reused")
}

func TestAppOnlyConnectionCannotReachAnotherMailbox(t *testing.T) {
	fake := newGraphFake(t)
	client, _ := appOnlyClient(t, fake, "ceo@contoso.example")
	result, err := sdkgo.RunQuery(newOutlookDexContext("other-mailbox"), client.SearchMessages(), outlookConnection, outlookmail.SearchMessagesInput{})
	require.NoError(t, err)
	require.Equal(t, outlookmail.SearchMessagesBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, result.Failure.Kind)
}

func TestAppOnlyConnectionWithoutAMailboxIsADefectBeforeAnyRequest(t *testing.T) {
	fake := newGraphFake(t)
	client, _ := appOnlyClient(t, fake, "")
	result, err := sdkgo.RunQuery(newOutlookDexContext("no-mailbox"), client.SearchMessages(), outlookConnection, outlookmail.SearchMessagesInput{})
	require.NoError(t, err)
	require.Equal(t, outlookmail.SearchMessagesBranchDefect, result.Branch)
	require.Equal(t, "an app-only connection needs the mailbox configuration field", result.Failure.Message)
	require.Zero(t, fake.RequestCount(graphtest.EndpointListFolderMessages))
}

func TestRedirectsAreNotFollowedAndReflectedTokensAreRefused(t *testing.T) {
	var answer func(http.ResponseWriter, *http.Request)
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { answer(writer, request) }))
	t.Cleanup(provider.Close)
	accessToken := "delegated-access-token-reflected-0123"
	client, err := outlookmail.New(outlookmail.Config{}, sdkgo.StaticCredentialProvider[outlookmail.Credentials]{outlookConnection: {
		AuthMethodID: outlookmail.MicrosoftOAuthAuthMethodID, AccessToken: sdkgo.NewSecretString(accessToken),
	}}, outlookmail.WithLocalProviderURL(provider.URL))
	require.NoError(t, err)

	redirects := 0
	answer = func(writer http.ResponseWriter, request *http.Request) {
		redirects++
		http.Redirect(writer, request, "https://attacker.example/collect", http.StatusFound)
	}
	result, err := sdkgo.RunQuery(newOutlookDexContext("redirect"), client.GetMessage(), outlookConnection, outlookmail.GetMessageInput{MessageID: "AAMkMessage-1="})
	require.NoError(t, err)
	require.Equal(t, outlookmail.GetMessageBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
	require.Equal(t, 1, redirects)

	answer = func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":"AAMkMessage-1=","subject":"` + accessToken + `"}`))
	}
	result, err = sdkgo.RunQuery(newOutlookDexContext("reflected"), client.GetMessage(), outlookConnection, outlookmail.GetMessageInput{MessageID: "AAMkMessage-1="})
	require.NoError(t, err)
	require.Equal(t, outlookmail.GetMessageBranchInvalidResponse, result.Branch)
	require.NotContains(t, result.Failure.Message, accessToken)
}

func TestOversizedResponseSelectsInvalidResponse(t *testing.T) {
	fake := newGraphFake(t)
	for index := 0; index < 20; index++ {
		seedCustomerMessage(fake, "Refund", searchBase)
	}
	client, err := outlookmail.New(outlookmail.Config{MaxResponseBytes: 2048}, sdkgo.StaticCredentialProvider[outlookmail.Credentials]{outlookConnection: {
		AuthMethodID: outlookmail.MicrosoftOAuthAuthMethodID, AccessToken: sdkgo.NewSecretString(fake.IssueDelegatedAccessToken()),
	}}, outlookmail.WithLocalProviderURL(fake.URL))
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newOutlookDexContext("oversized"), client.SearchMessages(), outlookConnection, outlookmail.SearchMessagesInput{Limit: 20})
	require.NoError(t, err)
	require.Equal(t, outlookmail.SearchMessagesBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
}
