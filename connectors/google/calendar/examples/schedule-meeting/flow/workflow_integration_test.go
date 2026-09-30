//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package schedulemeeting

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	calendar "github.com/superdurable/dex-connectors-library/connectors/google/calendar"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func integrationMeetingInput() Input {
	return Input{
		Summary: "Meridian kickoff", Description: "Agenda to follow.",
		Start: losAngelesBoundary("2026-02-20T14:00:00-08:00"), End: losAngelesBoundary("2026-02-20T15:00:00-08:00"),
		Attendees: []string{"priya@meridian.example.com", "alice@example.com"}, RequestConference: true,
	}
}

func TestScheduleMeetingSurvivesALostInsertResponseAndInvitesOnceWithRealDex(t *testing.T) {
	provider := newFakeGoogleCalendar(t)
	provider.losesFirstInsertResponse = true
	provider.seedListedEvents(
		// Decoys that overlap by text or by time but do not block the meeting.
		map[string]any{"id": "tztrap0001", "status": "confirmed", "summary": "Vendor call", "description": "Starts 2:00 PM PST",
			"start": map[string]any{"dateTime": "2026-02-20T19:00:00Z"}, "end": map[string]any{"dateTime": "2026-02-20T20:00:00Z"}},
		map[string]any{"id": "focusblock1", "status": "confirmed", "summary": "Focus", "transparency": "transparent",
			"start": map[string]any{"dateTime": "2026-02-20T22:00:00Z"}, "end": map[string]any{"dateTime": "2026-02-20T23:00:00Z"}},
		map[string]any{"id": "oldboardprep", "status": "cancelled"},
	)
	flow, harness := newScheduleMeetingHarness(t, provider.URL, CalendarSelection{CalendarID: "team@group.calendar.google.com", CalendarName: "Ops Team"})
	ctx := integrationContext(t)

	flowID := startScheduleMeeting(t, ctx, harness.client, flow, "scheduled", integrationMeetingInput())
	outcome := waitForCompletedMeeting(t, ctx, harness.client, flowID)

	require.Equal(t, MeetingScheduled, outcome.Status)
	require.Equal(t, "team@group.calendar.google.com", outcome.CalendarID)
	require.NotNil(t, outcome.Event)
	require.Equal(t, []string{"priya@meridian.example.com", "alice@example.com"}, attendeeEmails(outcome.Event.Attendees))
	require.Equal(t, 1, provider.storedEventCount(), "the retried Step found its own event instead of inserting a second")
	require.Equal(t, 1, provider.count("insert"))
	require.Equal(t, 1, provider.count("getMissing"), "only the first createEvent attempt found the stable ID unused")
	require.Equal(t, 3, provider.count("getFound"), "the retried attempt, the read-back, and the update each read the hold")
	require.Equal(t, 1, provider.count("patch"))

	freeBusy := provider.lastFreeBusyRequest()
	require.Equal(t, "2026-02-20T14:00:00-08:00", freeBusy["timeMin"])
	require.Equal(t, "2026-02-20T15:00:00-08:00", freeBusy["timeMax"])
	require.Equal(t, "America/Los_Angeles", freeBusy["timeZone"])
	require.Equal(t, []any{map[string]any{"id": "team@group.calendar.google.com"}}, freeBusy["items"])

	insert := provider.lastInsertRequest()
	require.NotContains(t, insert.body, "attendees", "the hold is placed before anyone is invited")
	require.Equal(t, "none", insert.query.Get("sendUpdates"))
	require.Equal(t, "1", insert.query.Get("conferenceDataVersion"))
	patch := provider.lastPatchRequest()
	require.Equal(t, "all", patch.query.Get("sendUpdates"))
	require.Equal(t, `"1"`, patch.ifMatch)
}

// Dex dispatches a backup attempt for an async Execute that runs past about seven seconds.
func TestScheduleMeetingSlowInsertBackupAttemptCreatesOneEventWithRealDex(t *testing.T) {
	provider := newFakeGoogleCalendar(t)
	provider.slowFirstInsertDelay = 9 * time.Second
	flow, harness := newScheduleMeetingHarness(t, provider.URL, CalendarSelection{})
	ctx := integrationContext(t)

	flowID := startScheduleMeeting(t, ctx, harness.client, flow, "slow-insert", integrationMeetingInput())
	outcome := waitForCompletedMeeting(t, ctx, harness.client, flowID)

	require.Equal(t, MeetingScheduled, outcome.Status)
	require.Equal(t, 1, provider.storedEventCount(), "every concurrent attempt writes the same stable event ID")
	require.LessOrEqual(t, provider.count("insert"), 2)
	require.Equal(t, 1, provider.count("patch"))
	t.Logf("slow insert: inserts=%d missingReads=%d foundReads=%d", provider.count("insert"), provider.count("getMissing"), provider.count("getFound"))
}

func TestScheduleMeetingCompletesBusyWithoutWritingWithRealDex(t *testing.T) {
	provider := newFakeGoogleCalendar(t)
	provider.busy = []map[string]string{{"start": "2026-02-20T14:30:00-08:00", "end": "2026-02-20T15:30:00-08:00"}}
	flow, harness := newScheduleMeetingHarness(t, provider.URL, CalendarSelection{})
	ctx := integrationContext(t)

	flowID := startScheduleMeeting(t, ctx, harness.client, flow, "busy", integrationMeetingInput())
	outcome := waitForCompletedMeeting(t, ctx, harness.client, flowID)

	require.Equal(t, MeetingBusy, outcome.Status)
	require.Equal(t, DefaultCalendarID, outcome.CalendarID)
	require.Equal(t, []calendar.BusyInterval{{Start: "2026-02-20T14:30:00-08:00", End: "2026-02-20T15:30:00-08:00"}}, outcome.Busy)
	require.Zero(t, provider.count("insert"))
}

func TestScheduleMeetingLeavesAConflictingHoldUninvitedWithRealDex(t *testing.T) {
	provider := newFakeGoogleCalendar(t)
	provider.seedListedEvents(map[string]any{"id": "boardprep01", "status": "confirmed", "summary": "Board prep",
		"start": map[string]any{"dateTime": "2026-02-20T14:30:00-08:00"}, "end": map[string]any{"dateTime": "2026-02-20T16:00:00-08:00"}})
	flow, harness := newScheduleMeetingHarness(t, provider.URL, CalendarSelection{})
	ctx := integrationContext(t)

	flowID := startScheduleMeeting(t, ctx, harness.client, flow, "conflict", integrationMeetingInput())
	outcome := waitForCompletedMeeting(t, ctx, harness.client, flowID)

	require.Equal(t, MeetingConflict, outcome.Status)
	require.Equal(t, []string{"boardprep01"}, outcome.ConflictingEventIDs)
	require.NotNil(t, outcome.Event)
	require.Empty(t, outcome.Event.Attendees)
	require.Equal(t, 1, provider.count("insert"))
	require.Zero(t, provider.count("patch"), "nobody is invited to a double-booked hold")
}

func TestScheduleMeetingFailsOnUnwiredIncompleteFreeBusyWithRealDex(t *testing.T) {
	provider := newFakeGoogleCalendar(t)
	provider.freeBusyErrors = []map[string]string{{"domain": "global", "reason": "notFound"}}
	flow, harness := newScheduleMeetingHarness(t, provider.URL, CalendarSelection{})
	ctx := integrationContext(t)

	flowID := startScheduleMeeting(t, ctx, harness.client, flow, "incomplete", integrationMeetingInput())
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{})
	require.NoError(t, err)
	require.Equal(t, dex.FlowFailed, result.Status, "an unknown availability must not look free")
	require.Equal(t, 1, provider.count("freeBusy"))
	require.Zero(t, provider.count("insert"))
}

func TestScheduleMeetingRejectsANaiveTimeBeforeCallingGoogleWithRealDex(t *testing.T) {
	provider := newFakeGoogleCalendar(t)
	flow, harness := newScheduleMeetingHarness(t, provider.URL, CalendarSelection{})
	ctx := integrationContext(t)
	input := integrationMeetingInput()
	input.Start.DateTime = "2026-02-20T14:00:00"

	flowID := startScheduleMeeting(t, ctx, harness.client, flow, "naive", input)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{})
	require.NoError(t, err)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Zero(t, provider.count("any"))
}

func startScheduleMeeting(t *testing.T, ctx context.Context, client *dex.Client, flow *Flow, scenario string, input Input) string {
	t.Helper()
	flowID := "google-calendar-schedule-meeting-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	return flowID
}

func waitForCompletedMeeting(t *testing.T, ctx context.Context, client *dex.Client, flowID string) MeetingOutcome {
	t.Helper()
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, "flow %s: %s", flowID, result.ErrorMessage)
	var outcome MeetingOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	return outcome
}

func attendeeEmails(attendees []calendar.EventAttendee) []string {
	emails := make([]string, len(attendees))
	for index, attendee := range attendees {
		emails[index] = attendee.Email
	}
	return emails
}

type fakeRecordedRequest struct {
	query   url.Values
	ifMatch string
	body    string
}

// fakeGoogleCalendar is a stateful Calendar fake; its list ignores the window so every decoy reaches the overlap check.
type fakeGoogleCalendar struct {
	*httptest.Server
	mutex                    sync.Mutex
	events                   map[string]map[string]any
	listedEvents             []map[string]any
	busy                     []map[string]string
	freeBusyErrors           []map[string]string
	losesFirstInsertResponse bool
	slowFirstInsertDelay     time.Duration
	counts                   map[string]int
	freeBusyRequests         []map[string]any
	insertRequests           []fakeRecordedRequest
	patchRequests            []fakeRecordedRequest
}

func newFakeGoogleCalendar(t *testing.T) *fakeGoogleCalendar {
	t.Helper()
	provider := &fakeGoogleCalendar{events: map[string]map[string]any{}, counts: map[string]int{}}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeGoogleCalendar) serveHTTP(response http.ResponseWriter, request *http.Request) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.counts["any"]++
	if request.Header.Get("Authorization") != "Bearer calendar-token" {
		writeFakeJSON(response, http.StatusUnauthorized, map[string]any{"error": map[string]any{"code": 401}})
		return
	}
	var body map[string]any
	contents, err := io.ReadAll(request.Body)
	if err != nil {
		writeFakeJSON(response, http.StatusBadRequest, map[string]any{"error": map[string]any{"code": 400}})
		return
	}
	if len(contents) != 0 {
		if err := json.Unmarshal(contents, &body); err != nil {
			writeFakeJSON(response, http.StatusBadRequest, map[string]any{"error": map[string]any{"code": 400}})
			return
		}
	}
	segments := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	switch {
	case request.Method == http.MethodPost && request.URL.Path == "/freeBusy":
		provider.counts["freeBusy"]++
		provider.freeBusyRequests = append(provider.freeBusyRequests, body)
		calendars := map[string]any{}
		for _, item := range body["items"].([]any) {
			calendarID := item.(map[string]any)["id"].(string)
			entry := map[string]any{"busy": provider.busy}
			if provider.busy == nil {
				entry["busy"] = []any{}
			}
			if provider.freeBusyErrors != nil {
				entry["errors"] = provider.freeBusyErrors
			}
			calendars[calendarID] = entry
		}
		writeFakeJSON(response, http.StatusOK, map[string]any{"timeMin": body["timeMin"], "timeMax": body["timeMax"], "calendars": calendars})
	case len(segments) == 3 && segments[0] == "calendars" && segments[2] == "events" && request.Method == http.MethodGet:
		provider.counts["list"]++
		items := append([]map[string]any{}, provider.listedEvents...)
		for _, event := range provider.events {
			items = append(items, event)
		}
		writeFakeJSON(response, http.StatusOK, map[string]any{"timeZone": "America/Los_Angeles", "items": items})
	case len(segments) == 3 && segments[0] == "calendars" && segments[2] == "events" && request.Method == http.MethodPost:
		provider.counts["insert"]++
		provider.insertRequests = append(provider.insertRequests, fakeRecordedRequest{query: request.URL.Query(), body: string(contents)})
		eventID := body["id"].(string)
		if _, exists := provider.events[eventID]; exists {
			writeFakeJSON(response, http.StatusConflict, map[string]any{"error": map[string]any{"errors": []any{map[string]any{"reason": "duplicate"}}}})
			return
		}
		body["status"], body["etag"] = "confirmed", `"1"`
		body["conferenceData"] = map[string]any{"conferenceId": "abc-defg-hij", "createRequest": map[string]any{"status": map[string]any{"statusCode": "pending"}}}
		provider.events[eventID] = body
		if provider.slowFirstInsertDelay > 0 && provider.counts["insert"] == 1 {
			// Google has stored the event; the response arrives late while other requests proceed.
			provider.mutex.Unlock()
			time.Sleep(provider.slowFirstInsertDelay)
			provider.mutex.Lock()
		}
		if provider.losesFirstInsertResponse && provider.counts["insert"] == 1 {
			writeFakeJSON(response, http.StatusServiceUnavailable, map[string]any{"error": map[string]any{"code": 503}})
			return
		}
		writeFakeJSON(response, http.StatusOK, body)
	case len(segments) == 4 && segments[0] == "calendars" && segments[2] == "events" && request.Method == http.MethodGet:
		event, exists := provider.events[segments[3]]
		if !exists {
			provider.counts["getMissing"]++
			writeFakeJSON(response, http.StatusNotFound, map[string]any{"error": map[string]any{"errors": []any{map[string]any{"reason": "notFound"}}}})
			return
		}
		provider.counts["getFound"]++
		writeFakeJSON(response, http.StatusOK, event)
	case len(segments) == 4 && segments[0] == "calendars" && segments[2] == "events" && request.Method == http.MethodPatch:
		provider.counts["patch"]++
		provider.patchRequests = append(provider.patchRequests, fakeRecordedRequest{query: request.URL.Query(), ifMatch: request.Header.Get("If-Match"), body: string(contents)})
		event, exists := provider.events[segments[3]]
		if !exists {
			writeFakeJSON(response, http.StatusNotFound, map[string]any{})
			return
		}
		if request.Header.Get("If-Match") != event["etag"] {
			writeFakeJSON(response, http.StatusPreconditionFailed, map[string]any{})
			return
		}
		if attendees, ok := body["attendees"].([]any); ok {
			for _, attendee := range attendees {
				attendee.(map[string]any)["responseStatus"] = "needsAction"
			}
			event["attendees"] = attendees
		}
		event["etag"] = `"2"`
		writeFakeJSON(response, http.StatusOK, event)
	default:
		writeFakeJSON(response, http.StatusNotFound, map[string]any{})
	}
}

func (provider *fakeGoogleCalendar) seedListedEvents(events ...map[string]any) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.listedEvents = append(provider.listedEvents, events...)
}

func (provider *fakeGoogleCalendar) count(name string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.counts[name]
}

func (provider *fakeGoogleCalendar) storedEventCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.events)
}

func (provider *fakeGoogleCalendar) lastFreeBusyRequest() map[string]any {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.freeBusyRequests[len(provider.freeBusyRequests)-1]
}

func (provider *fakeGoogleCalendar) lastInsertRequest() fakeRecordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.insertRequests[len(provider.insertRequests)-1]
}

func (provider *fakeGoogleCalendar) lastPatchRequest() fakeRecordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.patchRequests[len(provider.patchRequests)-1]
}

func writeFakeJSON(response http.ResponseWriter, status int, body any) {
	contents, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if _, err := response.Write(contents); err != nil {
		panic(err)
	}
}

type scheduleMeetingHarness struct {
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newScheduleMeetingHarness(t *testing.T, endpoint string, selection CalendarSelection) (*Flow, *scheduleMeetingHarness) {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "google", Name: ConnectionName}
	providerClient, err := calendar.New(calendar.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[calendar.Credentials]{
		reference: {AuthMethodID: calendar.GoogleOAuthAuthMethodID, AccessToken: sdkgo.NewSecretString("calendar-token")},
	})
	require.NoError(t, err)
	connection, err := calendar.NewConnection(providerClient, reference)
	require.NoError(t, err)
	flow := NewFlow(connection, selection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	workerAddress := net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness := &scheduleMeetingHarness{
		registry: registry, cache: cache, serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801"), workerAddress: workerAddress,
	}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: workerAddress, FlowServiceAddress: harness.serverAddress, WorkerTarget: dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	harness.worker = worker
	harness.workerResult = make(chan error, 1)
	go func() { harness.workerResult <- worker.Start() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	return flow, harness
}

func integrationContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return ctx
}

func availableIntegrationPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}

func environmentOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
