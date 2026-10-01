// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zoom_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoom"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestNewRejectsInsecureEndpointsAndInvalidLimits(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[zoom.Credentials]{}
	_, err := zoom.New(zoom.Config{Endpoint: "http://api.zoom.example/v2"}, credentials)
	require.ErrorContains(t, err, "HTTPS")
	_, err = zoom.New(zoom.Config{Endpoint: "https://api.zoom.us/v2?token=secret"}, credentials)
	require.ErrorContains(t, err, "query")
	_, err = zoom.New(zoom.Config{MaxResponseBytes: -1}, credentials)
	require.Error(t, err)
	_, err = zoom.New(zoom.Config{}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = zoom.New(zoom.Config{}, credentials, nil)
	require.ErrorContains(t, err, "option is nil")
	client, err := zoom.New(zoom.Config{}, credentials)
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestRequestsUseTheBearerTokenAndNoIdempotencyHeader(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, meetingJSON(85746065, "Planning", "2036-02-24T17:00:00Z"))
	})
	client := newZoomClient(t, provider.URL+"/v2")

	result, err := sdkgo.RunQuery(newZoomDexContext("bearer"), client.GetMeeting(), zoomConnection, zoom.GetMeetingInput{MeetingID: 85746065})
	require.NoError(t, err)
	require.Equal(t, zoom.GetMeetingBranchFound, result.Branch)
	request := provider.request(t, 0)
	require.Equal(t, "Bearer "+testAccessToken, request.authorization)
	require.Equal(t, "application/json", request.headers.Get("Accept"))
	require.Empty(t, request.headers.Get("Idempotency-Key"))
	require.Equal(t, "/v2/meetings/85746065", request.path)
}

func TestUnauthorizedRequestRefreshesOnceAndRetriesWithReplacement(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if index == 0 {
			require.Equal(t, "Bearer rejected-token", request.Header.Get("Authorization"))
			writeJSON(t, response, http.StatusUnauthorized, `{"code":124,"message":"Access token has expired."}`)
			return
		}
		require.Equal(t, "Bearer replacement-token", request.Header.Get("Authorization"))
		writeJSON(t, response, http.StatusOK, meetingJSON(85746065, "Planning", "2036-02-24T17:00:00Z"))
	})
	credentials := &rejectionRefreshingCredentialProvider{}
	client, err := zoom.New(zoom.Config{Endpoint: provider.URL}, credentials)
	require.NoError(t, err)

	result, err := sdkgo.RunQuery(newZoomDexContext("refresh-once"), client.GetMeeting(), zoomConnection, zoom.GetMeetingInput{MeetingID: 85746065})
	require.NoError(t, err)
	require.Equal(t, zoom.GetMeetingBranchFound, result.Branch)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, 1, credentials.forcedRefreshes)
}

func TestSecondUnauthorizedResponseIsTerminalWithoutARefreshLoop(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusUnauthorized, `{"code":124,"message":"Invalid access token."}`)
	})
	credentials := &rejectionRefreshingCredentialProvider{}
	client, err := zoom.New(zoom.Config{Endpoint: provider.URL}, credentials)
	require.NoError(t, err)

	result, err := sdkgo.RunQuery(newZoomDexContext("no-refresh-loop"), client.GetMeeting(), zoomConnection, zoom.GetMeetingInput{MeetingID: 85746065})
	require.NoError(t, err)
	require.Equal(t, zoom.GetMeetingBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Equal(t, "Zoom rejected the request with HTTP 401 (code 124)", result.Failure.Message)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, 1, credentials.forcedRefreshes)
}

func TestFailuresKeepOnlyZoomsNumericErrorCode(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusBadRequest, `{"code":3161,"message":"SENTINEL host@example.com is not allowed to host"}`)
	})
	client := newZoomClient(t, provider.URL)

	result, err := sdkgo.RunQuery(newZoomDexContext("safe-failure"), client.GetMeeting(), zoomConnection, zoom.GetMeetingInput{MeetingID: 85746065})
	require.NoError(t, err)
	require.Equal(t, zoom.GetMeetingBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureProviderRejection, result.Failure.Kind)
	require.Equal(t, "Zoom rejected the request with HTTP 400 (code 3161)", result.Failure.Message)
	require.Equal(t, "3161", result.Receipt.Metadata["zoomErrorCode"])
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL")
	require.NotContains(t, string(encoded), testAccessToken)
}

func TestThrottlesRetryAfterZoomsDelayAndDailyLimitsAreRejected(t *testing.T) {
	for _, test := range []struct {
		name       string
		retryAfter string
		isRetried  bool
		delay      time.Duration
	}{
		{name: "per-second limit", retryAfter: "2", isRetried: true, delay: 2 * time.Second},
		{name: "no Retry-After", isRetried: true},
		{name: "daily limit", retryAfter: "3600", isRetried: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				if test.retryAfter != "" {
					response.Header().Set("Retry-After", test.retryAfter)
				}
				writeJSON(t, response, http.StatusTooManyRequests, `{"code":429,"message":"SENTINEL limit"}`)
			})
			client := newZoomClient(t, provider.URL)
			result, err := sdkgo.RunQuery(newZoomDexContext("throttle"), client.GetMeeting(), zoomConnection, zoom.GetMeetingInput{MeetingID: 85746065})
			if !test.isRetried {
				require.NoError(t, err)
				require.Equal(t, zoom.GetMeetingBranchProviderRejected, result.Branch)
				require.Equal(t, sdkgo.FailureRateLimit, result.Failure.Kind)
				return
			}
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, sdkgo.FailureRateLimit, retry.Failure.Kind)
			var retryAfter *dex.RetryAfterError
			if test.delay == 0 {
				require.False(t, errors.As(err, &retryAfter))
				return
			}
			require.ErrorAs(t, err, &retryAfter)
			require.Equal(t, test.delay, retryAfter.After)
		})
	}
}

func TestServerErrorsAndTransportFailuresRetryReads(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusServiceUnavailable, `{"code":503,"message":"SENTINEL"}`)
	})
	client := newZoomClient(t, provider.URL)
	_, err := sdkgo.RunQuery(newZoomDexContext("unavailable"), client.GetMeeting(), zoomConnection, zoom.GetMeetingInput{MeetingID: 85746065})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)

	provider.Close()
	_, err = sdkgo.RunQuery(newZoomDexContext("unreachable"), client.GetMeeting(), zoomConnection, zoom.GetMeetingInput{MeetingID: 85746065})
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
}

func TestOversizedRedirectedAndReflectingResponsesSelectInvalidResponse(t *testing.T) {
	for _, test := range []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
		kind    sdkgo.FailureKind
	}{
		{name: "oversized", kind: sdkgo.FailureResponseTooLarge, handler: func(response http.ResponseWriter, _ *http.Request) {
			writeJSON(t, response, http.StatusOK, `{"id":85746065,"topic":"`+strings.Repeat("x", 4096)+`"}`)
		}},
		{name: "redirect", kind: sdkgo.FailureProtocol, handler: func(response http.ResponseWriter, request *http.Request) {
			http.Redirect(response, request, "https://attacker.example/steal", http.StatusFound)
		}},
		{name: "credential reflection", kind: sdkgo.FailureProtocol, handler: func(response http.ResponseWriter, _ *http.Request) {
			writeJSON(t, response, http.StatusOK, meetingJSON(85746065, testAccessToken, "2036-02-24T17:00:00Z"))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				test.handler(response, request)
			})
			client, err := zoom.New(zoom.Config{Endpoint: provider.URL, MaxResponseBytes: 2048}, staticZoomCredentials())
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(newZoomDexContext("invalid-"+test.name), client.GetMeeting(), zoomConnection, zoom.GetMeetingInput{MeetingID: 85746065})
			require.NoError(t, err)
			require.Equal(t, zoom.GetMeetingBranchInvalidResponse, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Equal(t, 1, provider.requestCount(), "a redirect is never followed")
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), testAccessToken)
		})
	}
}

func TestCredentialRefreshOutagesRetryAndRevokedGrantsSelectDefect(t *testing.T) {
	provider := newRecordingProvider(t, func(http.ResponseWriter, *http.Request, int) {
		t.Fatal("a call without credentials must not reach Zoom")
	})
	outage, err := zoom.New(zoom.Config{Endpoint: provider.URL}, failingCredentialProvider{err: errors.New("Zoom token endpoint returned HTTP 503")})
	require.NoError(t, err)
	_, err = sdkgo.RunMutation(newZoomDexContext("refresh-outage"), outage.CreateMeeting(), zoomConnection, validCreateMeetingInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "nothing was sent, so even a create may retry")
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)

	revoked, err := zoom.New(zoom.Config{Endpoint: provider.URL}, failingCredentialProvider{
		err: sdkgo.NewReauthorizationRequiredError(errors.New("Zoom rejected credential refresh with invalid_grant")),
	})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newZoomDexContext("refresh-revoked"), revoked.GetMeeting(), zoomConnection, zoom.GetMeetingInput{MeetingID: 85746065})
	require.NoError(t, err)
	require.Equal(t, zoom.GetMeetingBranchDefect, result.Branch)
	require.Equal(t, "Zoom connection requires reauthorization", result.Failure.Message)
	require.Zero(t, provider.requestCount())
}

func TestUnavailableCredentialsSelectDefectWithoutProviderRequest(t *testing.T) {
	provider := newRecordingProvider(t, func(http.ResponseWriter, *http.Request, int) {
		t.Fatal("a call without credentials must not reach Zoom")
	})
	client, err := zoom.New(zoom.Config{Endpoint: provider.URL}, sdkgo.StaticCredentialProvider[zoom.Credentials]{
		zoomConnection: {AccessToken: sdkgo.NewSecretString("token with spaces")},
	})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newZoomDexContext("no-credentials"), client.GetMeeting(), zoomConnection, zoom.GetMeetingInput{MeetingID: 85746065})
	require.NoError(t, err)
	require.Equal(t, zoom.GetMeetingBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Zero(t, provider.requestCount())
}
