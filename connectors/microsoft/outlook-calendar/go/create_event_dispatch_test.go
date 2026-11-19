// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookcalendar_test

import (
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	outlookcalendar "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-calendar/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

// createEventAttempts runs createEvent until a branch is selected or maxAttempts pass, advancing the fenced context.
func createEventAttempts(t *testing.T, ctx *singledispatchtest.Context, client *outlookcalendar.Client, maxAttempts int) (sdkgo.MutationResult[outlookcalendar.CreateEventOutput], int) {
	t.Helper()
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, err := sdkgo.RunMutation(ctx, client.CreateEvent(), calendarConnection, meetingCreateInput())
		var retry *sdkgo.RetryError
		if !errors.As(err, &retry) {
			require.NoError(t, err)
			return result, attempt
		}
		ctx.AdvanceAttempt()
	}
	return sdkgo.MutationResult[outlookcalendar.CreateEventOutput]{}, maxAttempts
}

// fencedCreateProvider answers every marker lookup with found or missing and every POST through post.
func fencedCreateProvider(t *testing.T, isFound func() bool, post func(index int32) (int, string)) (*recordingProvider, *atomic.Int32) {
	var posts atomic.Int32
	provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			if isFound() {
				writeJSON(t, response, http.StatusOK, `{"value":[`+timedEventJSON("evt-1", "Kickoff")+`]}`)
				return
			}
			writeJSON(t, response, http.StatusOK, `{"value":[]}`)
			return
		}
		status, body := post(posts.Add(1))
		writeJSON(t, response, status, body)
	})
	return provider, &posts
}

func TestCreateEventResendsAnUnknownOutcomeBelowTheCapAndSelectsUncertainAtIt(t *testing.T) {
	provider, posts := fencedCreateProvider(t, func() bool { return false }, func(int32) (int, string) {
		return http.StatusInternalServerError, `{"error":{"code":"generalException"}}`
	})
	result, attempts := createEventAttempts(t, singledispatchtest.NewContext("create-cap", "create"), newCalendarClient(t, provider.URL), 6)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	require.Equal(t, 3, attempts)
	require.EqualValues(t, 3, posts.Load(), "the default cap allows three possibly effective sends")
}

func TestCreateEventReadsBackBeforeResending(t *testing.T) {
	var isCreated atomic.Bool
	provider, posts := fencedCreateProvider(t, isCreated.Load, func(int32) (int, string) {
		isCreated.Store(true)
		return http.StatusGatewayTimeout, `{"error":{"code":"timeout"}}`
	})
	result, _ := createEventAttempts(t, singledispatchtest.NewContext("create-read-back", "create"), newCalendarClient(t, provider.URL), 4)
	require.Equal(t, outlookcalendar.CreateEventBranchCreated, result.Branch)
	require.True(t, result.Value.WasAlreadyCreated)
	require.EqualValues(t, 1, posts.Load())
}

func TestCreateEventRejectsADefiniteFirstRefusalOnceAndAfterAnEarlierSendSelectsUncertain(t *testing.T) {
	provider, posts := fencedCreateProvider(t, func() bool { return false }, func(int32) (int, string) {
		return http.StatusForbidden, `{"error":{"code":"ErrorAccessDenied"}}`
	})
	result, attempts := createEventAttempts(t, singledispatchtest.NewContext("create-refused", "create"), newCalendarClient(t, provider.URL), 3)
	require.Equal(t, outlookcalendar.CreateEventBranchProviderRejected, result.Branch)
	require.Equal(t, 1, attempts)
	require.EqualValues(t, 1, posts.Load())

	provider, posts = fencedCreateProvider(t, func() bool { return false }, func(index int32) (int, string) {
		if index == 1 {
			return http.StatusBadGateway, `{"error":{"code":"badGateway"}}`
		}
		return http.StatusForbidden, `{"error":{"code":"ErrorAccessDenied"}}`
	})
	result, _ = createEventAttempts(t, singledispatchtest.NewContext("create-refused-later", "create"), newCalendarClient(t, provider.URL), 3)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch, "the first POST may have created the event")
	require.EqualValues(t, 2, posts.Load())
}

func TestCreateEventDoesNotCountThrottledSendsAgainstTheCap(t *testing.T) {
	provider, posts := fencedCreateProvider(t, func() bool { return false }, func(index int32) (int, string) {
		if index <= 3 {
			return http.StatusTooManyRequests, `{"error":{"code":"ApplicationThrottled"}}`
		}
		return http.StatusCreated, timedEventJSON("evt-1", "Kickoff")
	})
	result, attempts := createEventAttempts(t, singledispatchtest.NewContext("create-throttled", "create"), newCalendarClient(t, provider.URL), 5)
	require.Equal(t, outlookcalendar.CreateEventBranchCreated, result.Branch)
	require.Equal(t, 4, attempts)
	require.EqualValues(t, 4, posts.Load())
}

func TestCreateEventRetriesAnUnreadableCheckpointWithoutARequest(t *testing.T) {
	provider, posts := fencedCreateProvider(t, func() bool { return false }, func(int32) (int, string) {
		return http.StatusCreated, timedEventJSON("evt-1", "Kickoff")
	})
	ctx := singledispatchtest.NewContext("create-fail-closed", "create")
	ctx.SetReadError(errors.New("heartbeat unreadable"))
	_, err := sdkgo.RunMutation(ctx, newCalendarClient(t, provider.URL).CreateEvent(), calendarConnection, meetingCreateInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Zero(t, posts.Load())
	require.Zero(t, provider.requestCount())
}

func TestCreateEventReadsBackWhenAnAcceptedBodyBreaksAfterTheHeaders(t *testing.T) {
	var isCreated atomic.Bool
	var posts atomic.Int32
	provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			if isCreated.Load() {
				writeJSON(t, response, http.StatusOK, `{"value":[`+timedEventJSON("evt-1", "Kickoff")+`]}`)
				return
			}
			writeJSON(t, response, http.StatusOK, `{"value":[]}`)
			return
		}
		posts.Add(1)
		isCreated.Store(true)
		// The declared length is never written, so the client's body read fails after a 201.
		response.Header().Set("Content-Length", "4096")
		response.WriteHeader(http.StatusCreated)
		_, err := response.Write([]byte(`{"id":"evt-1"`))
		require.NoError(t, err)
	})
	result, attempts := createEventAttempts(t, singledispatchtest.NewContext("create-broken-body", "create"), newCalendarClient(t, provider.URL), 4)
	require.Equal(t, outlookcalendar.CreateEventBranchCreated, result.Branch)
	require.Equal(t, 1, attempts, "the same attempt reads the marker back")
	require.EqualValues(t, 1, posts.Load())
	require.Equal(t, "evt-1", result.Value.Event.ID)
}

func TestCreateEventNeverResendsWhenAnAcceptedBodyBreaksAndTheLookupLags(t *testing.T) {
	var posts atomic.Int32
	provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, `{"value":[]}`)
			return
		}
		posts.Add(1)
		response.Header().Set("Content-Length", "4096")
		response.WriteHeader(http.StatusCreated)
		_, err := response.Write([]byte(`{"id":"evt-1"`))
		require.NoError(t, err)
	})
	result, attempts := createEventAttempts(t, singledispatchtest.NewContext("create-broken-body-lag", "create"), newCalendarClient(t, provider.URL), 4)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	require.Equal(t, 2, attempts, "the second attempt only reads back")
	require.EqualValues(t, 1, posts.Load())
}

func TestCreateEventRetriesARefusedPostWhoseCredentialRefreshFailsTransiently(t *testing.T) {
	var posts atomic.Int32
	provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, `{"value":[]}`)
			return
		}
		posts.Add(1)
		if request.Header.Get("Authorization") == "Bearer rejected-token" {
			writeJSON(t, response, http.StatusUnauthorized, `{"error":{"code":"InvalidAuthenticationToken"}}`)
			return
		}
		writeJSON(t, response, http.StatusCreated, timedEventJSON("evt-1", "Kickoff"))
	})
	unavailable := errors.New("credential broker is unavailable")
	credentials := &rejectionRefreshingCredentialProvider{refreshErrors: []error{unavailable, unavailable, unavailable}}
	client, err := outlookcalendar.New(outlookcalendar.Config{}, credentials, outlookcalendar.WithLocalProviderURL(provider.URL))
	require.NoError(t, err)
	result, attempts := createEventAttempts(t, singledispatchtest.NewContext("create-refresh-transient", "create"), client, 6)
	require.Equal(t, outlookcalendar.CreateEventBranchCreated, result.Branch, "a refused POST does not count against the cap")
	require.Equal(t, 4, attempts)
	require.EqualValues(t, 5, posts.Load())
}

func TestCreateEventRejectsARefusedPostWhoseCredentialRefreshIsTerminal(t *testing.T) {
	var posts atomic.Int32
	provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, `{"value":[]}`)
			return
		}
		posts.Add(1)
		writeJSON(t, response, http.StatusUnauthorized, `{"error":{"code":"InvalidAuthenticationToken"}}`)
	})
	credentials := &rejectionRefreshingCredentialProvider{refreshErrors: []error{sdkgo.ErrReauthorizationRequired}}
	client, err := outlookcalendar.New(outlookcalendar.Config{}, credentials, outlookcalendar.WithLocalProviderURL(provider.URL))
	require.NoError(t, err)
	result, attempts := createEventAttempts(t, singledispatchtest.NewContext("create-refresh-terminal", "create"), client, 3)
	require.Equal(t, outlookcalendar.CreateEventBranchProviderRejected, result.Branch)
	require.Equal(t, 1, attempts)
	require.EqualValues(t, 1, posts.Load())
}

func TestCreateEventNeverResendsAfterAnAcceptedUnreadableAnswer(t *testing.T) {
	provider, posts := fencedCreateProvider(t, func() bool { return false }, func(int32) (int, string) {
		return http.StatusCreated, `{"id":`
	})
	result, attempts := createEventAttempts(t, singledispatchtest.NewContext("create-accepted", "create"), newCalendarClient(t, provider.URL), 4)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	require.Equal(t, 2, attempts, "the second attempt only reads back")
	require.EqualValues(t, 1, posts.Load())
}
