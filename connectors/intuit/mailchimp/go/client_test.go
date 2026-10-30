// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp_test

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// recordingTransport answers every request locally and records where it was sent, so no network is used.
type recordingTransport struct {
	mutex    sync.Mutex
	requests []*http.Request
	reply    func(*http.Request) *http.Response
}

func (transport *recordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.mutex.Lock()
	transport.requests = append(transport.requests, request)
	transport.mutex.Unlock()
	return transport.reply(request), nil
}

func localResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request,
	}
}

func TestNewValidatesOptionsAndLimits(t *testing.T) {
	credentials := testCredentialProvider(testAPIKey)
	_, err := mailchimp.New(mailchimp.Config{MaxResponseBytes: -1}, credentials)
	require.Error(t, err)
	_, err = mailchimp.New(mailchimp.Config{}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = mailchimp.New(mailchimp.Config{}, credentials, nil)
	require.ErrorContains(t, err, "option is nil")
	for _, baseURL := range []string{"http://us6.api.mailchimp.example.com/3.0", "https://user:secret@us6.api.mailchimp.com/3.0", "https://us6.api.mailchimp.com/3.0?x=1"} {
		_, err = mailchimp.New(mailchimp.Config{}, credentials, mailchimp.WithAPIBaseURL(baseURL))
		require.Error(t, err, baseURL)
		require.NotContains(t, err.Error(), "secret")
	}
	client, err := mailchimp.New(mailchimp.Config{}, credentials)
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestRequestsGoToTheDataCenterNamedByTheAPIKeyWithABearerToken(t *testing.T) {
	for _, test := range []struct {
		apiKey string
		host   string
	}{
		{apiKey: testAPIKey, host: "us6.api.mailchimp.com"},
		{apiKey: "fedcba9876543210fedcba9876543210" + "-us21", host: "us21.api.mailchimp.com"},
		{apiKey: "0123456789abcdef0123456789abcde-us19", host: "us19.api.mailchimp.com"},
	} {
		transport := &recordingTransport{reply: func(request *http.Request) *http.Response {
			return localResponse(request, http.StatusOK, memberBody(testEmailAddress, "subscribed"))
		}}
		client, err := mailchimp.New(mailchimp.Config{}, testCredentialProvider(test.apiKey), mailchimp.WithHTTPClient(&http.Client{Transport: transport}))
		require.NoError(t, err)
		result, err := sdkgo.RunQuery(newMailchimpDexContext("derived-host"), client.GetMember(), mailchimpConnection,
			mailchimp.GetMemberInput{ListID: testListID, EmailAddress: testEmailAddress})
		require.NoError(t, err)
		require.Equal(t, mailchimp.GetMemberBranchFound, result.Branch, test.apiKey)
		require.Len(t, transport.requests, 1)
		request := transport.requests[0]
		require.Equal(t, "https://"+test.host+"/3.0/lists/"+testListID+"/members/"+testSubscriberHash, request.URL.String())
		require.Equal(t, "Bearer "+test.apiKey, request.Header.Get("Authorization"))
		require.Equal(t, "application/json", request.Header.Get("Accept"))
	}
}

func TestAPIKeysWithoutADataCenterSuffixSelectDefectWithoutARequest(t *testing.T) {
	for _, apiKey := range []string{
		"0123456789abcdef0123456789abcdef", "0123456789abcdef0123456789abcdef-", "-us21", "0123456789abcdef0123456789abcdef-US6",
		"0123456789abcdef0123456789abcdef" + "-us6.example.com", "0123456789abcdef0123456789abcdef" + "-us6/evil", "0123 456789abcdef-us6",
		"0123456789abcdef0123456789abcdef-us", "short-us6",
	} {
		transport := &recordingTransport{reply: func(request *http.Request) *http.Response {
			t.Fatalf("no request is expected for %q", apiKey)
			return nil
		}}
		client, err := mailchimp.New(mailchimp.Config{}, testCredentialProvider(apiKey), mailchimp.WithHTTPClient(&http.Client{Transport: transport}))
		require.NoError(t, err)
		result, err := sdkgo.RunQuery(newMailchimpDexContext("invalid-key"), client.GetMember(), mailchimpConnection,
			mailchimp.GetMemberInput{ListID: testListID, EmailAddress: testEmailAddress})
		require.NoError(t, err)
		require.Equal(t, mailchimp.GetMemberBranchDefect, result.Branch, apiKey)
		require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
		require.NotContains(t, result.Failure.Message, apiKey)
	}
}

func TestFailureStatusesSelectBranchesWithOnlyTitleAndFieldTokens(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		body    string
		branch  sdkgo.BranchID
		kind    sdkgo.FailureKind
		message string
	}{
		{
			name: "invalid resource", status: http.StatusBadRequest,
			body: `{"title":"Invalid Resource","status":400,"detail":"SENTINEL jane@example.com looks fake","errors":[` +
				`{"field":"merge_fields.FNAME","message":"SENTINEL"},{"field":"jane@example.com","message":"SENTINEL"},{"field":"email_address","message":"SENTINEL"}]}`,
			branch: mailchimp.GetMemberBranchProviderRejected, kind: sdkgo.FailureValidation,
			message: "Mailchimp rejected the request (HTTP 400) [Invalid Resource; fields: email_address, merge_fields.FNAME]",
		},
		{
			name: "invalid key", status: http.StatusUnauthorized, body: `{"title":"API Key Invalid","status":401,"detail":"SENTINEL Your API key may be invalid."}`,
			branch: mailchimp.GetMemberBranchProviderRejected, kind: sdkgo.FailureAuthentication,
			message: "Mailchimp rejected the request (HTTP 401) [API Key Invalid]",
		},
		{
			name: "forbidden role", status: http.StatusForbidden, body: `{"title":"Forbidden","status":403,"detail":"SENTINEL role"}`,
			branch: mailchimp.GetMemberBranchProviderRejected, kind: sdkgo.FailureAuthorization,
			message: "Mailchimp rejected the request (HTTP 403) [Forbidden]",
		},
		{
			name: "missing contact", status: http.StatusNotFound, body: `{"title":"Resource Not Found","status":404,"detail":"SENTINEL"}`,
			branch: mailchimp.GetMemberBranchNotFound, kind: sdkgo.FailureNotFound,
			message: "Mailchimp found no such resource (HTTP 404) [Resource Not Found]",
		},
		{
			name: "method not allowed", status: http.StatusMethodNotAllowed, body: `{"title":"Method Not Allowed","status":405,"detail":"SENTINEL"}`,
			branch: mailchimp.GetMemberBranchProviderRejected, kind: sdkgo.FailureProviderRejection,
			message: "Mailchimp rejected the request (HTTP 405) [Method Not Allowed]",
		},
		{
			name: "title with text", status: http.StatusBadRequest, body: `{"title":"SENTINEL: jane@example.com 99","status":400}`,
			branch: mailchimp.GetMemberBranchProviderRejected, kind: sdkgo.FailureValidation, message: "Mailchimp rejected the request (HTTP 400)",
		},
		{
			name: "reflected key", status: http.StatusBadRequest, body: `{"title":"Bad Request","status":400,"errors":[{"field":"` + testAPIKey + `"}]}`,
			branch: mailchimp.GetMemberBranchProviderRejected, kind: sdkgo.FailureValidation, message: "Mailchimp rejected the request (HTTP 400) [Bad Request]",
		},
		{
			name: "redirect", status: http.StatusFound, body: ``,
			branch: mailchimp.GetMemberBranchProviderRejected, kind: sdkgo.FailureProtocol,
			message: "Mailchimp redirected the request (HTTP 302); redirects are never followed",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingMailchimp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				if test.status == http.StatusFound {
					response.Header().Set("Location", "https://evil.example.com/steal")
				}
				writeJSON(t, response, test.status, test.body)
			})
			result, err := sdkgo.RunQuery(newMailchimpDexContext("failure-"+test.name), newMailchimpClient(t, provider.URL).GetMember(), mailchimpConnection,
				mailchimp.GetMemberInput{ListID: testListID, EmailAddress: testEmailAddress})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Equal(t, test.message, result.Failure.Message)
			require.Equal(t, 1, provider.requestCount(), "a redirect is never followed and a rejection is not retried")
			requireNoSecretOrProviderText(t, result)
		})
	}
}

func TestThrottlingAndOutagesAreRetriedAfterRetryAfter(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		body       string
		retryAfter string
		kind       sdkgo.FailureKind
		delay      time.Duration
	}{
		{name: "too many connections", status: http.StatusTooManyRequests, body: `{"title":"Too Many Requests","status":429,"detail":"SENTINEL"}`, retryAfter: "7", kind: sdkgo.FailureRateLimit, delay: 7 * time.Second},
		{name: "throttling 403 without a document", status: http.StatusForbidden, body: ``, kind: sdkgo.FailureRateLimit},
		{name: "internal error", status: http.StatusInternalServerError, body: `{"title":"Internal Server Error","status":500}`, kind: sdkgo.FailureAvailability},
		{name: "CDN timeout with HTML", status: http.StatusBadGateway, body: `<html>SENTINEL</html>`, kind: sdkgo.FailureAvailability},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingMailchimp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				if test.retryAfter != "" {
					response.Header().Set("Retry-After", test.retryAfter)
				}
				writeJSON(t, response, test.status, test.body)
			})
			_, err := sdkgo.RunQuery(newMailchimpDexContext("retry-"+test.name), newMailchimpClient(t, provider.URL).GetMember(), mailchimpConnection,
				mailchimp.GetMemberInput{ListID: testListID, EmailAddress: testEmailAddress})
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, test.kind, retry.Failure.Kind)
			require.NotContains(t, retry.Failure.Message, "SENTINEL")
			if test.delay > 0 {
				var retryAfter *dex.RetryAfterError
				require.ErrorAs(t, err, &retryAfter)
				require.Equal(t, test.delay, retryAfter.After)
			}
		})
	}
}

func TestReadsRejectOversizedMalformedAndCredentialReflectingResponses(t *testing.T) {
	for name, body := range map[string]string{
		"oversized":        `{"id":"` + strings.Repeat("a", 4096) + `"}`,
		"malformed":        `{"id":`,
		"another contact":  memberBody("someone.else@example.com", "subscribed"),
		"missing status":   `{"id":"` + testSubscriberHash + `","email_address":"x@example.com"}`,
		"invalid time":     strings.Replace(memberBody(testEmailAddress, "subscribed"), "2026-01-28T08:45:00+00:00", "yesterday", 1),
		"reflected secret": strings.Replace(memberBody(testEmailAddress, "subscribed"), "Urist McVankab", testAPIKey, 1),
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingMailchimp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusOK, body)
			})
			client, err := mailchimp.New(mailchimp.Config{MaxResponseBytes: 2048}, testCredentialProvider(testAPIKey), mailchimp.WithAPIBaseURL(provider.URL+"/3.0"))
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(newMailchimpDexContext("invalid-"+name), client.GetMember(), mailchimpConnection,
				mailchimp.GetMemberInput{ListID: testListID, EmailAddress: testEmailAddress})
			require.NoError(t, err)
			require.Equal(t, mailchimp.GetMemberBranchInvalidResponse, result.Branch)
			requireNoSecretOrProviderText(t, result)
		})
	}
}

func TestRefusedConnectionIsRetriedAndCredentialsAreReadBeforeEveryCall(t *testing.T) {
	_, err := sdkgo.RunQuery(newMailchimpDexContext("refused"), newMailchimpClient(t, closedLoopbackURL(t)).GetMember(), mailchimpConnection,
		mailchimp.GetMemberInput{ListID: testListID, EmailAddress: testEmailAddress})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)

	client, err := mailchimp.New(mailchimp.Config{}, sdkgo.StaticCredentialProvider[mailchimp.Credentials]{})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newMailchimpDexContext("unknown-connection"), client.GetMember(), mailchimpConnection,
		mailchimp.GetMemberInput{ListID: testListID, EmailAddress: testEmailAddress})
	require.NoError(t, err)
	require.Equal(t, mailchimp.GetMemberBranchDefect, result.Branch)
	require.Equal(t, "connection credentials are unavailable", result.Failure.Message)
}

func TestSubscriberHashIsTheMD5OfTheLowercasedAddress(t *testing.T) {
	require.Equal(t, testSubscriberHash, mailchimp.SubscriberHash(testEmailAddress))
	require.Equal(t, testSubscriberHash, mailchimp.SubscriberHash("urist.mcvankab@example.com"))
	require.Equal(t, "d41d8cd98f00b204e9800998ecf8427e", mailchimp.SubscriberHash(""))
}
