// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zoom_test

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoom"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func validCreateMeetingInput() zoom.CreateMeetingInput {
	isWaitingRoomOn, isJoinBeforeHostOn := true, false
	return zoom.CreateMeetingInput{
		Topic: "Consultation with Priya", StartTime: "2036-02-24T09:00:00-08:00", TimeZone: "America/Los_Angeles",
		DurationMinutes: 30, Agenda: "Booking BK-77",
		Settings: &zoom.MeetingSettingsInput{WaitingRoom: &isWaitingRoomOn, JoinBeforeHost: &isJoinBeforeHostOn, AutoRecording: zoom.AutoRecordingCloud},
	}
}

func TestCreateMeetingSendsTheInstantInUTCWithItsTimeZone(t *testing.T) {
	context := newZoomDexContext("create")
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		requireCheckpointState(t, context.lastHeartbeat(), singledispatch.StateClaimed)
		writeJSON(t, response, http.StatusCreated, meetingJSON(92674392836, "Consultation with Priya", "2036-02-24T17:00:00Z"))
	})
	client := newZoomClient(t, provider.URL)

	result, err := sdkgo.RunMutation(context, client.CreateMeeting(), zoomConnection, validCreateMeetingInput())
	require.NoError(t, err)
	require.Equal(t, zoom.CreateMeetingBranchCreated, result.Branch)
	require.Nil(t, result.Failure)
	request := provider.request(t, 0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/users/me/meetings", request.path)
	require.Equal(t, "application/json", request.headers.Get("Content-Type"))
	require.Empty(t, request.headers.Get("Idempotency-Key"))
	require.Equal(t, map[string]any{
		"topic": "Consultation with Priya", "type": float64(2), "start_time": "2036-02-24T17:00:00Z",
		"duration": float64(30), "timezone": "America/Los_Angeles", "agenda": "Booking BK-77",
		"settings": map[string]any{"waiting_room": true, "join_before_host": false, "auto_recording": "cloud"},
	}, decodeRequestBody(t, request))

	meeting := result.Value
	require.Equal(t, int64(92674392836), meeting.ID)
	require.Equal(t, "https://us05web.zoom.us/j/92674392836?pwd=join", meeting.JoinURL)
	require.True(t, meeting.StartTime.Equal(futureStart))
	require.Equal(t, "America/Los_Angeles", meeting.TimeZone)
	require.Equal(t, zoom.MeetingSettings{HostVideo: true, MuteUponEntry: true, WaitingRoom: true, AutoRecording: zoom.AutoRecordingNone}, meeting.Settings)
	require.Equal(t, "92674392836", result.Receipt.ProviderObjectID)
	require.Equal(t, sdkgo.IdempotencyKey(result.Receipt.CallID), result.Receipt.IdempotencyKey)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL", "start URLs, passcodes, and alternative hosts never cross the boundary")
}

func TestCreateMeetingRejectsAmbiguousTimesAndInvalidInputWithoutARequest(t *testing.T) {
	provider := newRecordingProvider(t, func(http.ResponseWriter, *http.Request, int) {
		t.Fatal("invalid input must not reach Zoom")
	})
	client := newZoomClient(t, provider.URL)
	for _, test := range []struct {
		name   string
		mutate func(*zoom.CreateMeetingInput)
		reason string
	}{
		{name: "naive start", mutate: func(input *zoom.CreateMeetingInput) { input.StartTime = "2036-02-24T09:00:00" }, reason: "explicit offset"},
		{name: "date only", mutate: func(input *zoom.CreateMeetingInput) { input.StartTime = "2036-02-24" }, reason: "explicit offset"},
		{name: "fractional second", mutate: func(input *zoom.CreateMeetingInput) { input.StartTime = "2036-02-24T09:00:00.5-08:00" }, reason: "whole second"},
		{name: "past start", mutate: func(input *zoom.CreateMeetingInput) { input.StartTime = "2020-02-24T09:00:00-08:00" }, reason: "not in the future"},
		{name: "missing time zone", mutate: func(input *zoom.CreateMeetingInput) { input.TimeZone = "" }, reason: "timeZone is required"},
		{name: "abbreviation", mutate: func(input *zoom.CreateMeetingInput) { input.TimeZone = "PST" }, reason: "not a known IANA"},
		{name: "local zone", mutate: func(input *zoom.CreateMeetingInput) { input.TimeZone = "Local" }, reason: "IANA time zone name"},
		{name: "blank topic", mutate: func(input *zoom.CreateMeetingInput) { input.Topic = "  " }, reason: "topic is required"},
		{name: "long topic", mutate: func(input *zoom.CreateMeetingInput) { input.Topic = strings.Repeat("t", 201) }, reason: "at most 200"},
		{name: "long agenda", mutate: func(input *zoom.CreateMeetingInput) { input.Agenda = strings.Repeat("a", 2001) }, reason: "at most 2000"},
		{name: "zero duration", mutate: func(input *zoom.CreateMeetingInput) { input.DurationMinutes = 0 }, reason: "between 1 and 1440"},
		{name: "day-long duration", mutate: func(input *zoom.CreateMeetingInput) { input.DurationMinutes = 1441 }, reason: "between 1 and 1440"},
		{name: "recording", mutate: func(input *zoom.CreateMeetingInput) { input.Settings.AutoRecording = "always" }, reason: "none, local, or cloud"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := validCreateMeetingInput()
			test.mutate(&input)
			result, err := sdkgo.RunMutation(newZoomDexContext("invalid-"+test.name), client.CreateMeeting(), zoomConnection, input)
			require.NoError(t, err)
			require.Equal(t, zoom.CreateMeetingBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, test.reason)
		})
	}
	require.Zero(t, provider.requestCount())
}

func TestCreateMeetingRetriesOnlyWhenZoomProvablyCreatedNothing(t *testing.T) {
	t.Run("per-second throttle", func(t *testing.T) {
		context := newZoomDexContext("throttled-create")
		provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
			response.Header().Set("Retry-After", "1")
			writeJSON(t, response, http.StatusTooManyRequests, `{"code":4001,"message":"SENTINEL"}`)
		})
		_, err := sdkgo.RunMutation(context, newZoomClient(t, provider.URL).CreateMeeting(), zoomConnection, validCreateMeetingInput())
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.Equal(t, sdkgo.FailureRateLimit, retry.Failure.Kind)
		requireCheckpointState(t, context.lastHeartbeat(), singledispatch.StateCanResend)
	})
	t.Run("connection refused", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		endpoint := "http://" + listener.Addr().String()
		require.NoError(t, listener.Close())
		context := newZoomDexContext("unreachable-create")
		_, err = sdkgo.RunMutation(context, newZoomClient(t, endpoint).CreateMeeting(), zoomConnection, validCreateMeetingInput())
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
		requireCheckpointState(t, context.lastHeartbeat(), singledispatch.StateCanResend)
	})
}

func TestCreateMeetingReportsEveryAmbiguousOutcomeAsUncertain(t *testing.T) {
	for _, test := range []struct {
		name          string
		status        int
		body          string
		kind          sdkgo.FailureKind
		isStall       bool
		isInterrupted bool
	}{
		{name: "server error", status: http.StatusInternalServerError, body: `{"code":500,"message":"SENTINEL"}`, kind: sdkgo.FailureAvailability},
		{name: "request timeout", status: http.StatusRequestTimeout, body: `{}`, kind: sdkgo.FailureAvailability},
		{name: "conflict without a lookup key", status: http.StatusConflict, body: `{"code":409}`, kind: sdkgo.FailureConflict},
		{name: "redirect", status: http.StatusFound, body: ``, kind: sdkgo.FailureProtocol},
		{name: "unusable created body", status: http.StatusCreated, body: `{"id":"not-a-number"}`, kind: sdkgo.FailureProtocol},
		{name: "created body without ID", status: http.StatusCreated, body: `{}`, kind: sdkgo.FailureProtocol},
		{name: "interrupted created body", status: http.StatusCreated, kind: sdkgo.FailureTransport, isInterrupted: true},
		{name: "oversized created body", status: http.StatusCreated, body: `{"agenda":"` + strings.Repeat("x", 4096) + `"}`, kind: sdkgo.FailureResponseTooLarge},
		{name: "no response before the request bound", isStall: true, kind: sdkgo.FailureTransport},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				if test.isStall {
					<-request.Context().Done()
					return
				}
				if test.isInterrupted {
					response.Header().Set("Content-Length", "100")
					response.WriteHeader(http.StatusCreated)
					if _, err := response.Write([]byte(`{"id":926`)); err != nil {
						t.Errorf("write partial accepted Zoom response: %v", err)
						return
					}
					response.(http.Flusher).Flush()
					connection, _, err := response.(http.Hijacker).Hijack()
					if err != nil {
						t.Errorf("interrupt accepted Zoom response: %v", err)
						return
					}
					_ = connection.Close() // The response is intentionally interrupted.
					return
				}
				if test.status == http.StatusFound {
					response.Header().Set("Location", "https://attacker.example/")
					response.WriteHeader(test.status)
					return
				}
				writeJSON(t, response, test.status, test.body)
			})
			httpClient := &http.Client{}
			if test.isStall {
				httpClient.Timeout = 200 * time.Millisecond
			}
			client, err := zoom.New(zoom.Config{Endpoint: provider.URL, MaxResponseBytes: 2048}, staticZoomCredentials(), zoom.WithHTTPClient(httpClient))
			require.NoError(t, err)
			context := newZoomDexContext("uncertain-" + test.name)
			result, err := sdkgo.RunMutation(context, client.CreateMeeting(), zoomConnection, validCreateMeetingInput())
			require.NoError(t, err)
			require.Equal(t, zoom.CreateMeetingBranchUncertain, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Zero(t, result.Value.ID)
			require.Equal(t, "Consultation with Priya", result.Value.Topic, "the requested meeting is echoed for reconciliation")
			require.True(t, result.Value.StartTime.Equal(futureStart))
			require.Equal(t, 1, provider.requestCount())
			requireCheckpointState(t, context.lastHeartbeat(), singledispatch.StateClaimed)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "SENTINEL")
		})
	}
}

func TestCreateMeetingNeverResendsAfterAnEarlierAttemptDispatched(t *testing.T) {
	provider := newRecordingProvider(t, func(http.ResponseWriter, *http.Request, int) {
		t.Fatal("a later attempt must not send the create again")
	})
	context := newZoomDexContext("replayed-create")
	require.NoError(t, context.RecordHeartbeat(map[string]bool{"isDispatched": true}))

	result, err := sdkgo.RunMutation(context, newZoomClient(t, provider.URL).CreateMeeting(), zoomConnection, validCreateMeetingInput())
	require.NoError(t, err)
	require.Equal(t, zoom.CreateMeetingBranchUncertain, result.Branch)
	require.Equal(t, "the Zoom create checkpoint could not be trusted", result.Failure.Message)
	require.Zero(t, provider.requestCount())
}

func TestCreateMeetingConclusiveRejectionsCreateNothing(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		retryAfter string
		body       string
		kind       sdkgo.FailureKind
		code       string
	}{
		{name: "hosting not allowed", status: http.StatusBadRequest, body: `{"code":3161,"message":"SENTINEL"}`, kind: sdkgo.FailureProviderRejection, code: "3161"},
		{name: "missing scope", status: http.StatusForbidden, body: `{"code":4711,"message":"SENTINEL"}`, kind: sdkgo.FailureAuthorization, code: "4711"},
		{name: "unknown user", status: http.StatusNotFound, body: `{"code":1001,"message":"SENTINEL"}`, kind: sdkgo.FailureNotFound, code: "1001"},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				if test.retryAfter != "" {
					response.Header().Set("Retry-After", test.retryAfter)
				}
				writeJSON(t, response, test.status, test.body)
			})
			result, err := sdkgo.RunMutation(newZoomDexContext("rejected-"+test.name), newZoomClient(t, provider.URL).CreateMeeting(), zoomConnection, validCreateMeetingInput())
			require.NoError(t, err)
			require.Equal(t, zoom.CreateMeetingBranchProviderRejected, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Equal(t, test.code, result.Receipt.Metadata["zoomErrorCode"])
			require.Equal(t, "Consultation with Priya", result.Value.Topic)
			require.Equal(t, 1, provider.requestCount())
		})
	}
}

func requireCheckpointState(t *testing.T, checkpoint json.RawMessage, state singledispatch.State) {
	t.Helper()
	var marker singledispatch.Marker
	require.NoError(t, json.Unmarshal(checkpoint, &marker))
	require.NotEmpty(t, marker.CallID)
	require.Equal(t, int32(1), marker.Attempt)
	require.Equal(t, state, marker.State)
}

func TestCreateMeetingFenceRetriesOnlyAfterDefiniteRejection(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, index int) {
				if index == 0 {
					writeJSON(t, response, status, `{"code":429}`)
					return
				}
				writeJSON(t, response, http.StatusCreated, meetingJSON(92674392836, "Consultation with Priya", "2036-02-24T17:00:00Z"))
			})
			context := singledispatchtest.NewContext("zoom-flow", "create-meeting")
			client := newZoomClient(t, provider.URL)
			_, err := sdkgo.RunMutation(context, client.CreateMeeting(), zoomConnection, validCreateMeetingInput())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			context.AdvanceAttempt()
			result, err := sdkgo.RunMutation(context, client.CreateMeeting(), zoomConnection, validCreateMeetingInput())
			require.NoError(t, err)
			require.Equal(t, zoom.CreateMeetingBranchCreated, result.Branch)
			require.Equal(t, 2, provider.requestCount())
			require.Equal(t, provider.request(t, 0).body, provider.request(t, 1).body)
			frames := context.Heartbeats()
			require.GreaterOrEqual(t, len(frames), 4)
			var first, second singledispatch.Marker
			require.NoError(t, json.Unmarshal(frames[0], &first))
			require.NoError(t, json.Unmarshal(frames[2], &second))
			require.Equal(t, first.CallID, second.CallID)
			require.Equal(t, int32(1), first.Attempt)
			require.Equal(t, int32(2), second.Attempt)
		})
	}
}

func TestCreateMeetingFenceFailsClosedWithoutCurrentResendMarker(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusServiceUnavailable, `{}`)
	})
	context := singledispatchtest.NewContext("zoom-flow", "create-meeting")
	context.SetLocalPhase(true)
	context.DropLocalHeartbeats()
	_, err := sdkgo.RunMutation(context, newZoomClient(t, provider.URL).CreateMeeting(), zoomConnection, validCreateMeetingInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	context.AdvanceAttempt()
	result, err := sdkgo.RunMutation(context, newZoomClient(t, provider.URL).CreateMeeting(), zoomConnection, validCreateMeetingInput())
	require.NoError(t, err)
	require.Equal(t, zoom.CreateMeetingBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreateMeetingFenceRecordsCredentialFailureAsNotSent(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, meetingJSON(92674392836, "Consultation with Priya", "2036-02-24T17:00:00Z"))
	})
	context := singledispatchtest.NewContext("zoom-flow", "create-meeting")
	client, err := zoom.New(zoom.Config{Endpoint: provider.URL}, failingCredentialProvider{err: errors.New("refresh unavailable")})
	require.NoError(t, err)
	_, err = sdkgo.RunMutation(context, client.CreateMeeting(), zoomConnection, validCreateMeetingInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Zero(t, provider.requestCount())
	context.AdvanceAttempt()
	result, err := sdkgo.RunMutation(context, newZoomClient(t, provider.URL).CreateMeeting(), zoomConnection, validCreateMeetingInput())
	require.NoError(t, err)
	require.Equal(t, zoom.CreateMeetingBranchCreated, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreateMeetingFenceReadsBackAcceptedIDWithoutResending(t *testing.T) {
	for _, readStatus := range []int{http.StatusOK, http.StatusServiceUnavailable, http.StatusNotFound} {
		t.Run(http.StatusText(readStatus), func(t *testing.T) {
			provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				if request.Method == http.MethodGet {
					if readStatus != http.StatusOK {
						writeJSON(t, response, readStatus, `{}`)
						return
					}
					writeJSON(t, response, http.StatusOK, meetingJSON(92674392836, "Consultation with Priya", "2036-02-24T17:00:00Z"))
					return
				}
				writeJSON(t, response, http.StatusCreated, meetingJSON(92674392836, "Consultation with Priya", "2036-02-24T17:00:00Z"))
			})
			context := singledispatchtest.NewContext("zoom-flow", "create-meeting")
			client := newZoomClient(t, provider.URL)
			first, err := sdkgo.RunMutation(context, client.CreateMeeting(), zoomConnection, validCreateMeetingInput())
			require.NoError(t, err)
			require.Equal(t, zoom.CreateMeetingBranchCreated, first.Branch)
			context.AdvanceAttempt()
			second, err := sdkgo.RunMutation(context, client.CreateMeeting(), zoomConnection, validCreateMeetingInput())
			switch readStatus {
			case http.StatusOK:
				require.NoError(t, err)
				require.Equal(t, zoom.CreateMeetingBranchCreated, second.Branch)
			case http.StatusServiceUnavailable:
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
			case http.StatusNotFound:
				require.NoError(t, err)
				require.Equal(t, zoom.CreateMeetingBranchUncertain, second.Branch)
			}
			require.Equal(t, 2, provider.requestCount())
			require.Equal(t, http.MethodPost, provider.request(t, 0).method)
			require.Equal(t, http.MethodGet, provider.request(t, 1).method)
		})
	}
}

func TestCreateMeetingFenceCutsOffExpiredAttemptBeforeSending(t *testing.T) {
	provider := newRecordingProvider(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("expired attempt sent") })
	context := singledispatchtest.NewContext("zoom-flow", "create-meeting")
	cancel := context.SetDeadline(time.Now().Add(time.Nanosecond))
	defer cancel()
	_, err := sdkgo.RunMutation(context, newZoomClient(t, provider.URL).CreateMeeting(), zoomConnection, validCreateMeetingInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Zero(t, provider.requestCount())
}

func TestCreateMeetingFenceCutsOffDispatchedRequestBeforeAttemptDeadline(t *testing.T) {
	provider := newRecordingProvider(t, func(_ http.ResponseWriter, request *http.Request, _ int) {
		<-request.Context().Done()
	})
	context := singledispatchtest.NewContext("zoom-flow", "create-meeting")
	cancel := context.SetDeadline(time.Now().Add(500 * time.Millisecond))
	defer cancel()
	result, err := sdkgo.RunMutation(context, newZoomClient(t, provider.URL).CreateMeeting(), zoomConnection, validCreateMeetingInput())
	require.NoError(t, err)
	require.Equal(t, zoom.CreateMeetingBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreateMeetingRefreshesRejectedCredentialBeforeRetrying(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusUnauthorized, `{"code":124}`)
			return
		}
		writeJSON(t, response, http.StatusCreated, meetingJSON(92674392836, "Consultation with Priya", "2036-02-24T17:00:00Z"))
	})
	credentials := &rejectionRefreshingCredentialProvider{}
	client, err := zoom.New(zoom.Config{Endpoint: provider.URL}, credentials)
	require.NoError(t, err)
	context := singledispatchtest.NewContext("zoom-flow", "rejected-create-credential")
	_, err = sdkgo.RunMutation(context, client.CreateMeeting(), zoomConnection, validCreateMeetingInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, 1, credentials.forcedRefreshes)
	require.Equal(t, 1, provider.requestCount())
	context.AdvanceAttempt()
	result, err := sdkgo.RunMutation(context, client.CreateMeeting(), zoomConnection, validCreateMeetingInput())
	require.NoError(t, err)
	require.Equal(t, zoom.CreateMeetingBranchCreated, result.Branch)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, "Bearer rejected-token", provider.request(t, 0).authorization)
	require.Equal(t, "Bearer replacement-token", provider.request(t, 1).authorization)
}

func TestCreateMeetingDoesNotReplayPostOnReusedConnection(t *testing.T) {
	var postRequests atomic.Int32
	var primeRemoteAddress atomic.Value
	var postRemoteAddress atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			primeRemoteAddress.Store(request.RemoteAddr)
			response.WriteHeader(http.StatusOK)
			return
		}
		postRequests.Add(1)
		postRemoteAddress.Store(request.RemoteAddr)
		if _, err := io.Copy(io.Discard, request.Body); err != nil {
			t.Errorf("read Zoom create request: %v", err)
			return
		}
		connection, _, err := response.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack Zoom response: %v", err)
			return
		}
		_ = connection.Close() // The test deliberately drops the reply.
	}))
	defer server.Close()
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Transport: headerAddingTransport{transport: transport}}
	prime, err := httpClient.Get(server.URL + "/prime")
	require.NoError(t, err)
	require.NoError(t, prime.Body.Close())
	client, err := zoom.New(zoom.Config{Endpoint: server.URL}, staticZoomCredentials(), zoom.WithHTTPClient(httpClient))
	require.NoError(t, err)
	result, err := sdkgo.RunMutation(newZoomDexContext("reused-connection"), client.CreateMeeting(), zoomConnection, validCreateMeetingInput())
	require.NoError(t, err)
	require.Equal(t, zoom.CreateMeetingBranchUncertain, result.Branch)
	require.Equal(t, int32(1), postRequests.Load())
	require.Equal(t, primeRemoteAddress.Load(), postRemoteAddress.Load())
}

type headerAddingTransport struct{ transport *http.Transport }

func (transport headerAddingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method == http.MethodPost {
		request.Header.Set("Idempotency-Key", "zoom-test-call")
	}
	return transport.transport.RoundTrip(request)
}
