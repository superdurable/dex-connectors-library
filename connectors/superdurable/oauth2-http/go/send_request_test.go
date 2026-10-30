// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oauth2http_test

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	oauth2http "github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestSendRequestSendsABearerRequestAndReturnsTheAnswer(t *testing.T) {
	session := newTestSession(t, nil, time.Hour)
	session.server.api = func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json; charset=utf-8")
		response.Header().Set("X-Request-Id", "req-123")
		_, _ = io.WriteString(response, `{"sub":"user-1","name":"Ada"}`)
	}

	result, err := session.send(t, "get-user", oauth2http.SendRequestInput{
		Path: "/users/42", Query: map[string][]string{"fields": {"name", "email"}, "expand": {"groups"}},
		Headers: map[string]string{"Accept": "application/json"},
	})

	require.NoError(t, err)
	require.Equal(t, oauth2http.SendRequestBranchCompleted, result.Branch)
	require.Nil(t, result.Failure)
	require.Equal(t, oauth2http.SendRequestOutput{
		StatusCode: 200, ContentType: "application/json; charset=utf-8", Body: `{"sub":"user-1","name":"Ada"}`,
	}, result.Value)
	require.Equal(t, "oauth2-http", result.Receipt.Provider)
	require.Equal(t, "req-123", result.Receipt.ProviderRequestID)
	requests := session.server.requests()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodGet, requests[0].method)
	require.Equal(t, "/api/users/42", requests[0].path)
	require.Equal(t, url.Values{"fields": {"name", "email"}, "expand": {"groups"}}, requests[0].query)
	require.Equal(t, "Bearer access-token-1", requests[0].authorization)
	require.Equal(t, "application/json", requests[0].header.Get("Accept"))
	require.Contains(t, requests[0].header.Get("User-Agent"), "oauth2-http-connector")
	require.Empty(t, session.server.refreshRequests(), "a token with an hour left needs no refresh")
	requireSecretFree(t, result, "access-token-1", "refresh-token-1")
}

func TestSendRequestJoinsPathsToTheBaseURLAndAcceptsOnlySameOriginURLs(t *testing.T) {
	session := newTestSession(t, nil, time.Hour)
	otherOrigin := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the access token must never reach another origin")
	}))
	t.Cleanup(otherOrigin.Close)

	pathResult, err := session.send(t, "path", oauth2http.SendRequestInput{Path: "/files/a%2Fb/c"})
	require.NoError(t, err)
	require.Equal(t, oauth2http.SendRequestBranchCompleted, pathResult.Branch)
	urlResult, err := session.send(t, "url", oauth2http.SendRequestInput{
		URL: strings.ToUpper(session.server.URL[:len("http://")]) + session.server.URL[len("http://"):] + "/api/items?page=2", Query: map[string][]string{"size": {"10"}},
	})
	require.NoError(t, err)
	require.Equal(t, oauth2http.SendRequestBranchCompleted, urlResult.Branch)

	requests := session.server.requests()
	require.Len(t, requests, 2)
	require.Equal(t, "/api/files/a%2Fb/c", requests[0].path, "percent-encoding survives the join")
	require.Equal(t, "/api/items", requests[1].path)
	require.Equal(t, url.Values{"page": {"2"}, "size": {"10"}}, requests[1].query, "query parameters merge with those in the URL")
	for name, foreignURL := range map[string]string{
		"another host":   otherOrigin.URL + "/api/items",
		"another port":   "http://" + net.JoinHostPort(mustHostname(t, session.server.URL), "1") + "/api/items",
		"another scheme": "https://" + strings.TrimPrefix(session.server.URL, "http://") + "/api/items",
	} {
		result, err := session.send(t, "foreign-"+name, oauth2http.SendRequestInput{URL: foreignURL})
		require.NoError(t, err, name)
		require.Equal(t, oauth2http.SendRequestBranchDefect, result.Branch, name)
		require.Contains(t, result.Failure.Message, "apiBaseUrl", name)
	}
	require.Len(t, session.server.requests(), 2, "no foreign URL was requested")
}

func mustHostname(t *testing.T, rawURL string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)
	return parsed.Hostname()
}

func TestSendRequestSelectsDefectWithoutSendingInvalidInput(t *testing.T) {
	session := newTestSession(t, func(config *oauth2http.Config) { config.MaxBodyBytes = 16 }, time.Hour)
	for _, test := range []struct {
		name           string
		input          oauth2http.SendRequestInput
		expectedReason string
	}{
		{"neither path nor url", oauth2http.SendRequestInput{}, "exactly one of path and url"},
		{"both path and url", oauth2http.SendRequestInput{Path: "/a", URL: "http://example.test/a"}, "exactly one of path and url"},
		{"unknown method", oauth2http.SendRequestInput{Method: "TRACE", Path: "/a"}, "method must be"},
		{"path without a slash", oauth2http.SendRequestInput{Path: "users"}, "single slash"},
		{"protocol-relative path", oauth2http.SendRequestInput{Path: "//evil.example/a"}, "single slash"},
		{"path with a query", oauth2http.SendRequestInput{Path: "/a?b=c"}, "use query"},
		{"path with a fragment", oauth2http.SendRequestInput{Path: "/a#b"}, "use query"},
		{"path with a backslash", oauth2http.SendRequestInput{Path: "/a\\b"}, "use query"},
		{"path with a space", oauth2http.SendRequestInput{Path: "/a b"}, "printable ASCII"},
		{"path with a dot-dot segment", oauth2http.SendRequestInput{Path: "/a/../b"}, ".. segment"},
		{"path with an encoded dot-dot segment", oauth2http.SendRequestInput{Path: "/a/%2e%2e/b"}, ".. segment"},
		{"path with bad percent-encoding", oauth2http.SendRequestInput{Path: "/a%zz"}, "percent-encoding"},
		{"url with user information", oauth2http.SendRequestInput{URL: "http://user:password@example.test/a"}, "user information"},
		{"url with a fragment", oauth2http.SendRequestInput{URL: "http://example.test/a#b"}, "fragment"},
		{"relative url", oauth2http.SendRequestInput{URL: "/a"}, "absolute http or https URL"},
		{"empty query name", oauth2http.SendRequestInput{Path: "/a", Query: map[string][]string{"": {"x"}}}, "query parameter names"},
		{"Authorization header", oauth2http.SendRequestInput{Path: "/a", Headers: map[string]string{"authorization": "Bearer x"}}, "Authorization"},
		{"Host header", oauth2http.SendRequestInput{Path: "/a", Headers: map[string]string{"Host": "evil.example"}}, "transport owns"},
		{"Cookie header", oauth2http.SendRequestInput{Path: "/a", Headers: map[string]string{"Cookie": "a=b"}}, "transport owns"},
		{"Proxy header", oauth2http.SendRequestInput{Path: "/a", Headers: map[string]string{"Proxy-Foo": "x"}}, "transport owns"},
		{"invalid header name", oauth2http.SendRequestInput{Path: "/a", Headers: map[string]string{"Bad Name": "x"}}, "RFC 7230"},
		{"header value with a newline", oauth2http.SendRequestInput{Path: "/a", Headers: map[string]string{"X-A": "x\r\nX-B: y"}}, "control characters"},
		{"duplicate header names", oauth2http.SendRequestInput{Path: "/a", Headers: map[string]string{"X-A": "1", "x-a": "2"}}, "unique"},
		{"Authorization as the idempotency header", oauth2http.SendRequestInput{Method: "POST", Path: "/a", IdempotencyKeyHeader: "Authorization"}, "Authorization"},
		{"GET with a body", oauth2http.SendRequestInput{Path: "/a", Body: "{}"}, "not allowed with GET"},
		{"oversized body", oauth2http.SendRequestInput{Method: "POST", Path: "/a", Body: strings.Repeat("x", 17)}, "maxBodyBytes"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := session.send(t, "invalid", test.input)

			require.NoError(t, err)
			require.Equal(t, oauth2http.SendRequestBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, test.expectedReason)
			require.NotContains(t, result.Failure.Message, "password")
			requireSecretFree(t, result, "access-token-1")
		})
	}
	require.Empty(t, session.server.requests())
	require.Empty(t, session.server.refreshRequests())
}

func TestSendRequestSendsABodyWithTheIdempotencyKeyOfTheStepExecution(t *testing.T) {
	session := newTestSession(t, nil, time.Hour)
	input := oauth2http.SendRequestInput{
		Method: "post", Path: "/orders", Body: `{"item":"gear"}`, IdempotencyKeyHeader: "Idempotency-Key",
	}

	first, err := session.send(t, "create-order", input)
	require.NoError(t, err)
	repeated, err := session.send(t, "create-order", input)
	require.NoError(t, err)
	other, err := session.send(t, "create-another-order", input)
	require.NoError(t, err)
	textInput := input
	textInput.Headers, textInput.Body, textInput.IdempotencyKeyHeader = map[string]string{"content-type": "text/plain"}, "plain text", ""
	text, err := session.send(t, "create-text-order", textInput)
	require.NoError(t, err)

	for _, result := range []oauth2http.SendRequestResult{first, repeated, other, text} {
		require.Equal(t, oauth2http.SendRequestBranchCompleted, result.Branch)
	}
	requests := session.server.requests()
	require.Len(t, requests, 4)
	require.Equal(t, http.MethodPost, requests[0].method)
	require.Equal(t, `{"item":"gear"}`, requests[0].body)
	require.Equal(t, "application/json", requests[0].header.Get("Content-Type"), "JSON is the default content type")
	require.Equal(t, string(first.Receipt.IdempotencyKey), requests[0].header.Get("Idempotency-Key"))
	require.Equal(t, requests[0].header.Get("Idempotency-Key"), requests[1].header.Get("Idempotency-Key"), "a retry of one Step execution repeats the key")
	require.NotEqual(t, requests[0].header.Get("Idempotency-Key"), requests[2].header.Get("Idempotency-Key"))
	require.Equal(t, "text/plain", requests[3].header.Get("Content-Type"))
	require.Equal(t, "plain text", requests[3].body)
	require.Empty(t, requests[3].header.Get("Idempotency-Key"), "no header is sent unless one is named")
}

func TestSendRequestMapsAnswersToBranches(t *testing.T) {
	for _, test := range []struct {
		statusCode     int
		expectedBranch sdkgo.BranchID
		expectedKind   sdkgo.FailureKind
	}{
		{http.StatusOK, oauth2http.SendRequestBranchCompleted, ""},
		{http.StatusCreated, oauth2http.SendRequestBranchCompleted, ""},
		{http.StatusNoContent, oauth2http.SendRequestBranchCompleted, ""},
		{http.StatusFound, oauth2http.SendRequestBranchRejected, sdkgo.FailureProviderRejection},
		{http.StatusBadRequest, oauth2http.SendRequestBranchRejected, sdkgo.FailureValidation},
		{http.StatusForbidden, oauth2http.SendRequestBranchRejected, sdkgo.FailureAuthorization},
		{http.StatusNotFound, oauth2http.SendRequestBranchRejected, sdkgo.FailureNotFound},
		{http.StatusConflict, oauth2http.SendRequestBranchRejected, sdkgo.FailureConflict},
		{http.StatusUnprocessableEntity, oauth2http.SendRequestBranchRejected, sdkgo.FailureValidation},
		{http.StatusTeapot, oauth2http.SendRequestBranchRejected, sdkgo.FailureProviderRejection},
	} {
		t.Run(strconv.Itoa(test.statusCode), func(t *testing.T) {
			session := newTestSession(t, nil, time.Hour)
			session.server.api = func(response http.ResponseWriter, _ *http.Request) {
				response.Header().Set("Location", "http://127.0.0.1:1/redirected")
				response.WriteHeader(test.statusCode)
				_, _ = io.WriteString(response, "body of "+strconv.Itoa(test.statusCode))
			}

			result, err := session.send(t, "answer", oauth2http.SendRequestInput{Method: "POST", Path: "/things"})

			require.NoError(t, err)
			require.Equal(t, test.expectedBranch, result.Branch)
			require.Equal(t, test.statusCode, result.Value.StatusCode)
			if test.statusCode != http.StatusNoContent {
				require.Equal(t, "body of "+strconv.Itoa(test.statusCode), result.Value.Body)
			}
			require.Len(t, session.server.requests(), 1, "redirects are never followed")
			if test.expectedKind == "" {
				require.Nil(t, result.Failure)
			} else {
				require.Equal(t, test.expectedKind, result.Failure.Kind)
				require.Contains(t, result.Failure.Message, strconv.Itoa(test.statusCode))
				require.NotContains(t, result.Failure.Message, "body of", "a failure never repeats the answer's text")
			}
		})
	}
}

func TestSendRequestRetriesThrottledRequestsWithTheServersDelay(t *testing.T) {
	for _, statusCode := range []int{http.StatusRequestTimeout, http.StatusTooManyRequests} {
		for _, method := range []string{"GET", "POST"} {
			t.Run(method+strconv.Itoa(statusCode), func(t *testing.T) {
				session := newTestSession(t, nil, time.Hour)
				session.server.api = func(response http.ResponseWriter, _ *http.Request) {
					response.Header().Set("Retry-After", "7")
					response.WriteHeader(statusCode)
				}

				_, err := session.send(t, "throttled", oauth2http.SendRequestInput{Method: method, Path: "/things"})

				var retryAfter *dex.RetryAfterError
				require.ErrorAs(t, err, &retryAfter)
				require.Equal(t, 7*time.Second, retryAfter.After)
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, sdkgo.FailureRateLimit, retry.Failure.Kind)
			})
		}
	}
}

func TestSendRequestRepeatsASafeMethodAfterAServerErrorButNeverAnUnsafeOne(t *testing.T) {
	for _, statusCode := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable} {
		t.Run(strconv.Itoa(statusCode), func(t *testing.T) {
			session := newTestSession(t, nil, time.Hour)
			session.server.api = func(response http.ResponseWriter, _ *http.Request) {
				response.Header().Set("Retry-After", "3")
				response.WriteHeader(statusCode)
				_, _ = io.WriteString(response, "server trouble")
			}

			for _, method := range []string{"GET", "HEAD", "OPTIONS"} {
				_, err := session.send(t, "safe", oauth2http.SendRequestInput{Method: method, Path: "/things"})
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry, method)
				require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind, method)
			}
			for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
				result, err := session.send(t, "unsafe", oauth2http.SendRequestInput{Method: method, Path: "/things"})
				require.NoError(t, err, method)
				require.Equal(t, oauth2http.SendRequestBranchUncertain, result.Branch, method)
				require.Equal(t, sdkgo.FailureAvailability, result.Failure.Kind, method)
				require.Equal(t, statusCode, result.Value.StatusCode, method)
			}
		})
	}
}

func TestSendRequestRetriesAnUndispatchedRequestAndTreatsALostAnswerPerMethod(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	closedAddress := listener.Addr().String()
	require.NoError(t, listener.Close())
	unreachable := newTestSession(t, func(config *oauth2http.Config) { config.APIBaseURL = "http://" + closedAddress }, time.Hour)
	for _, method := range []string{"GET", "POST"} {
		_, err = unreachable.send(t, "refused", oauth2http.SendRequestInput{Method: method, Path: "/things"})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, "a refused connection never sent the %s request", method)
		require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	}

	cutOff := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		connection, _, hijackErr := response.(http.Hijacker).Hijack()
		if hijackErr == nil {
			_ = connection.Close()
		}
	}))
	t.Cleanup(cutOff.Close)
	lost := newTestSession(t, func(config *oauth2http.Config) { config.APIBaseURL = cutOff.URL }, time.Hour)

	_, err = lost.send(t, "lost-get", oauth2http.SendRequestInput{Path: "/things"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "a lost GET is repeated")
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
	result, err := lost.send(t, "lost-post", oauth2http.SendRequestInput{Method: "POST", Path: "/things", Body: "{}"})
	require.NoError(t, err)
	require.Equal(t, oauth2http.SendRequestBranchUncertain, result.Branch, "the server read the POST and may have processed it")
	require.Equal(t, sdkgo.FailureTransport, result.Failure.Kind)
	require.Zero(t, result.Value.StatusCode)
}

func TestSendRequestBoundsTheBodyItReturns(t *testing.T) {
	session := newTestSession(t, func(config *oauth2http.Config) { config.MaxBodyBytes = 10 }, time.Hour)
	for _, test := range []struct {
		name         string
		answer       string
		expectedBody string
		isBase64     bool
		isTruncated  bool
	}{
		{name: "at the limit", answer: "0123456789", expectedBody: "0123456789"},
		{name: "past the limit", answer: "0123456789ABCDEF", expectedBody: "0123456789", isTruncated: true},
		{name: "cut inside a character", answer: "012345678€", expectedBody: "012345678", isTruncated: true},
		{name: "binary", answer: "\xff\xfe\x00\x01", isBase64: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			session.server.api = func(response http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(response, test.answer) }

			result, err := session.send(t, "bounded", oauth2http.SendRequestInput{Path: "/document"})

			require.NoError(t, err)
			require.Equal(t, oauth2http.SendRequestBranchCompleted, result.Branch)
			require.Equal(t, test.isTruncated, result.Value.IsBodyTruncated)
			require.Equal(t, test.isBase64, result.Value.IsBodyBase64)
			if test.isBase64 {
				require.Equal(t, "//4AAQ==", result.Value.Body)
				return
			}
			require.Equal(t, test.expectedBody, result.Value.Body)
		})
	}
}

func TestSendRequestRedactsTheAccessTokenAnAPIEchoes(t *testing.T) {
	session := newTestSession(t, nil, time.Hour)
	session.server.api = func(response http.ResponseWriter, request *http.Request) {
		_, _ = io.WriteString(response, `{"echoed":"`+request.Header.Get("Authorization")+`"}`)
	}

	result, err := session.send(t, "echo", oauth2http.SendRequestInput{Path: "/headers"})

	require.NoError(t, err)
	require.Equal(t, `{"echoed":"Bearer [REDACTED]"}`, result.Value.Body)
	requireSecretFree(t, result, "access-token-1")
}

func TestSendRequestReturnsTheRequestTimeoutAsALostAnswer(t *testing.T) {
	hanging := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	t.Cleanup(hanging.Close)
	session := newTestSession(t, func(config *oauth2http.Config) {
		config.APIBaseURL, config.RequestTimeout = hanging.URL, time.Second
	}, time.Hour)

	result, err := session.send(t, "hanging", oauth2http.SendRequestInput{Method: "DELETE", Path: "/things/1"})

	require.NoError(t, err)
	require.Equal(t, oauth2http.SendRequestBranchUncertain, result.Branch)
	require.Equal(t, sdkgo.FailureTransport, result.Failure.Kind)
}

func TestSendRequestReadsAServerThatClosesAfterTheHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		connection, _, hijackErr := response.(http.Hijacker).Hijack()
		if hijackErr != nil {
			return
		}
		_, _ = io.WriteString(connection, "HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\npartial")
		_ = connection.Close()
	}))
	t.Cleanup(server.Close)
	session := newTestSession(t, func(config *oauth2http.Config) { config.APIBaseURL = server.URL }, time.Hour)

	_, getErr := session.send(t, "partial-get", oauth2http.SendRequestInput{Path: "/things"})
	postResult, postErr := session.send(t, "partial-post", oauth2http.SendRequestInput{Method: "POST", Path: "/things"})

	var retry *sdkgo.RetryError
	require.True(t, errors.As(getErr, &retry), "a GET cut off mid-body is repeated")
	require.NoError(t, postErr)
	require.Equal(t, oauth2http.SendRequestBranchUncertain, postResult.Branch, "a POST cut off mid-body may have taken effect")
	require.Equal(t, http.StatusOK, postResult.Value.StatusCode)
}
