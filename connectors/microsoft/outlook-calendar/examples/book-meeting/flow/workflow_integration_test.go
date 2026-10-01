//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bookmeeting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	outlookcalendar "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-calendar"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationTenantID = "72f988bf-86f1-41af-91ab-2d7cd011db47"
	integrationMailbox  = "scheduling@contoso.com"
	graphUTCWallTime    = "2006-01-02T15:04:05"
)

func integrationMeetingInput() Input {
	return Input{
		Subject: "Meridian kickoff", Body: "Agenda to follow.",
		Start: losAngelesBoundary("2026-02-20T14:00:00-08:00"), End: losAngelesBoundary("2026-02-20T15:00:00-08:00"),
		Attendees: []string{"priya@meridian.example.com", "alice@example.com"}, RequestOnlineMeeting: true,
	}
}

func TestBookMeetingSurvivesALostCreateResponseAndInvitesOnceWithRealDex(t *testing.T) {
	provider := newFakeGraph(t, "/v1.0/me")
	provider.losesFirstCreateResponse = true
	provider.seedListedEvents(
		// Decoys that overlap by text or by time but do not block the meeting.
		map[string]any{"id": "timezone-trap", "subject": "Vendor call", "bodyPreview": "Starts 2:00 PM PST", "showAs": "busy",
			"start": utcBoundary("2026-02-20T19:00:00"), "end": utcBoundary("2026-02-20T20:00:00")},
		map[string]any{"id": "focus", "subject": "Focus", "showAs": "free", "start": utcBoundary("2026-02-20T22:00:00"), "end": utcBoundary("2026-02-20T23:00:00")},
		map[string]any{"id": "cancelled-prep", "subject": "Old prep", "showAs": "busy", "isCancelled": true,
			"start": utcBoundary("2026-02-20T22:00:00"), "end": utcBoundary("2026-02-20T23:00:00")},
	)
	flow, harness := newBookMeetingHarness(t, newStaticConnection(t, provider.URL), CalendarSelection{CalendarID: "team-calendar", CalendarName: "Ops Team"})
	ctx := integrationContext(t)

	flowID := startBookMeeting(t, ctx, harness.client, flow, "scheduled", integrationMeetingInput())
	outcome := waitForCompletedMeeting(t, ctx, harness.client, flowID)

	require.Equal(t, MeetingScheduled, outcome.Status)
	require.Equal(t, "team-calendar", outcome.CalendarID)
	require.NotNil(t, outcome.Event)
	require.Equal(t, []string{"priya@meridian.example.com", "alice@example.com"}, attendeeEmails(outcome.Event.Attendees))
	require.Equal(t, losAngelesBoundary("2026-02-20T14:00:00-08:00"), outcome.Event.Start)
	require.Equal(t, 1, provider.storedEventCount(), "the retried Step found its own event instead of creating a second")
	require.Equal(t, 1, provider.count("create"))
	require.Equal(t, 1, provider.count("lookupFound"), "the retried attempt found the hold through the idempotency marker")
	require.Equal(t, 1, provider.count("patch"))

	schedule := provider.lastScheduleRequest()
	require.Equal(t, []any{"priya@meridian.example.com", "alice@example.com"}, schedule["schedules"])
	require.Equal(t, map[string]any{"dateTime": "2026-02-20T22:00:00", "timeZone": "UTC"}, schedule["startTime"])
	require.Equal(t, map[string]any{"dateTime": "2026-02-20T23:00:00", "timeZone": "UTC"}, schedule["endTime"])

	create := provider.lastCreateRequest()
	require.Equal(t, "/v1.0/me/calendars/team-calendar/events", create.path)
	require.NotContains(t, create.body, "attendees", "the hold is placed before anyone is invited")
	require.Equal(t, map[string]any{"dateTime": "2026-02-20T14:00:00", "timeZone": "America/Los_Angeles"}, create.payload["start"])
	patch := provider.lastPatchRequest()
	require.Equal(t, `W/"1"`, patch.ifMatch)
}

// Dex dispatches a backup attempt for an async Execute that runs past about seven seconds. Here
// Graph stores the hold at once and answers nine seconds later, so the backup's lookup finds it.
func TestBookMeetingSlowCreateBackupAttemptFindsTheStoredHoldWithRealDex(t *testing.T) {
	provider := newFakeGraph(t, "/v1.0/me")
	provider.firstCreateDelayAfterStore = 9 * time.Second
	flow, harness := newBookMeetingHarness(t, newStaticConnection(t, provider.URL), CalendarSelection{})
	ctx := integrationContext(t)

	flowID := startBookMeeting(t, ctx, harness.client, flow, "slow-after-store", integrationMeetingInput())
	outcome := waitForCompletedMeeting(t, ctx, harness.client, flowID)

	require.Equal(t, MeetingScheduled, outcome.Status)
	require.Equal(t, 1, provider.storedEventCount(), "every attempt of the Step resolves to one event")
	require.Equal(t, 1, provider.count("create"), "the backup attempt found the stored hold and sent no POST")
	require.GreaterOrEqual(t, provider.count("lookupFound"), 1, "Dex dispatched a second attempt while the first was in flight")
	require.Equal(t, 1, provider.count("patch"))
	t.Logf("slow create after store: creates=%d lookupsMissing=%d lookupsFound=%d", provider.count("create"), provider.count("lookupMissing"), provider.count("lookupFound"))
}

// Here Graph is still committing the first POST when the backup attempt looks the key up, so the backup
// POSTs again with the same transactionId. The fake rejects the repeat with 409, as Graph might.
func TestBookMeetingSlowCreateBackupAttemptRepeatsTheTransactionIDWithRealDex(t *testing.T) {
	provider := newFakeGraph(t, "/v1.0/me")
	provider.firstCreateDelayBeforeStore = 9 * time.Second
	flow, harness := newBookMeetingHarness(t, newStaticConnection(t, provider.URL), CalendarSelection{})
	ctx := integrationContext(t)

	flowID := startBookMeeting(t, ctx, harness.client, flow, "slow-before-store", integrationMeetingInput())
	outcome := waitForCompletedMeeting(t, ctx, harness.client, flowID)

	require.Equal(t, MeetingScheduled, outcome.Status)
	require.Equal(t, 1, provider.storedEventCount(), "the repeated transactionId never created a second event")
	require.GreaterOrEqual(t, provider.count("createRepeatedTransaction"), 1, "Dex dispatched a second POST while the first was in flight")
	require.Equal(t, 1, provider.transactionIDCount(), "every POST carried the Step's one transactionId")
	require.Equal(t, 1, provider.count("patch"))
	t.Logf("slow create before store: creates=%d repeated=%d lookupsMissing=%d lookupsFound=%d", provider.count("create"),
		provider.count("createRepeatedTransaction"), provider.count("lookupMissing"), provider.count("lookupFound"))
}

func TestBookMeetingCompletesBusyWithoutWritingThroughAnAppOnlyConnectionWithRealDex(t *testing.T) {
	provider := newFakeGraph(t, "/v1.0/users/"+integrationMailbox)
	provider.expectedAccessToken = "app-only-token"
	provider.busyAttendees = map[string]bool{"alice@example.com": true}
	flow, harness := newBookMeetingHarness(t, newLocalAppOnlyConnection(t, provider.URL), CalendarSelection{})
	ctx := integrationContext(t)

	flowID := startBookMeeting(t, ctx, harness.client, flow, "busy", integrationMeetingInput())
	outcome := waitForCompletedMeeting(t, ctx, harness.client, flowID)

	require.Equal(t, MeetingBusy, outcome.Status)
	require.Equal(t, []string{"alice@example.com"}, outcome.BusyAttendees)
	require.Equal(t, 1, provider.count("token"), "the client credentials grant ran once at the configured tenant")
	require.Equal(t, 1, provider.count("schedule"))
	require.Zero(t, provider.count("create"))
}

func TestBookMeetingLeavesAConflictingHoldUninvitedWithRealDex(t *testing.T) {
	provider := newFakeGraph(t, "/v1.0/me")
	provider.seedListedEvents(map[string]any{"id": "board-prep", "subject": "Board prep", "showAs": "tentative",
		"start": utcBoundary("2026-02-20T22:30:00"), "end": utcBoundary("2026-02-21T00:00:00")})
	flow, harness := newBookMeetingHarness(t, newStaticConnection(t, provider.URL), CalendarSelection{})
	ctx := integrationContext(t)

	flowID := startBookMeeting(t, ctx, harness.client, flow, "conflict", integrationMeetingInput())
	outcome := waitForCompletedMeeting(t, ctx, harness.client, flowID)

	require.Equal(t, MeetingConflict, outcome.Status)
	require.Equal(t, []string{"board-prep"}, outcome.ConflictingEventIDs)
	require.NotNil(t, outcome.Event)
	require.Empty(t, outcome.Event.Attendees)
	require.Equal(t, 1, provider.count("create"))
	require.Equal(t, "/v1.0/me/calendar/events", provider.lastCreateRequest().path, "an unsaved picker uses the default calendar")
	require.Zero(t, provider.count("patch"), "nobody is invited to a double-booked hold")
}

func TestBookMeetingFailsOnUnwiredIncompleteFreeBusyWithRealDex(t *testing.T) {
	provider := newFakeGraph(t, "/v1.0/me")
	provider.scheduleErrors = map[string]string{"alice@example.com": "ErrorMailRecipientNotFound"}
	flow, harness := newBookMeetingHarness(t, newStaticConnection(t, provider.URL), CalendarSelection{})
	ctx := integrationContext(t)

	flowID := startBookMeeting(t, ctx, harness.client, flow, "incomplete", integrationMeetingInput())
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{})
	require.NoError(t, err)
	require.Equal(t, dex.FlowFailed, result.Status, "an unknown availability must not look free")
	require.Equal(t, 1, provider.count("schedule"))
	require.Zero(t, provider.count("create"))
}

func TestBookMeetingRejectsANaiveTimeBeforeCallingGraphWithRealDex(t *testing.T) {
	provider := newFakeGraph(t, "/v1.0/me")
	flow, harness := newBookMeetingHarness(t, newStaticConnection(t, provider.URL), CalendarSelection{})
	ctx := integrationContext(t)
	input := integrationMeetingInput()
	input.Start.DateTime = "2026-02-20T14:00:00"

	flowID := startBookMeeting(t, ctx, harness.client, flow, "naive", input)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{})
	require.NoError(t, err)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Zero(t, provider.count("any"))
}

func startBookMeeting(t *testing.T, ctx context.Context, client *dex.Client, flow *Flow, scenario string, input Input) string {
	t.Helper()
	flowID := "outlook-calendar-book-meeting-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
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

func attendeeEmails(attendees []outlookcalendar.EventAttendee) []string {
	emails := make([]string, len(attendees))
	for index, attendee := range attendees {
		emails[index] = attendee.Email
	}
	return emails
}

func utcBoundary(wallTime string) map[string]any {
	return map[string]any{"dateTime": wallTime + ".0000000", "timeZone": "UTC"}
}

type fakeRecordedRequest struct {
	path    string
	ifMatch string
	body    string
	payload map[string]any
}

// fakeGraph is a stateful Graph fake; its view ignores the window, and a repeated transactionId answers 409.
type fakeGraph struct {
	*httptest.Server
	mailboxRoot                 string
	expectedAccessToken         string
	mutex                       sync.Mutex
	events                      map[string]map[string]any
	eventIDsByMarker            map[string]string
	transactionIDs              map[string]bool
	listedEvents                []map[string]any
	busyAttendees               map[string]bool
	scheduleErrors              map[string]string
	losesFirstCreateResponse    bool
	firstCreateDelayAfterStore  time.Duration
	firstCreateDelayBeforeStore time.Duration
	counts                      map[string]int
	scheduleRequests            []map[string]any
	createRequests              []fakeRecordedRequest
	patchRequests               []fakeRecordedRequest
}

func newFakeGraph(t *testing.T, mailboxRoot string) *fakeGraph {
	t.Helper()
	provider := &fakeGraph{
		mailboxRoot: mailboxRoot, expectedAccessToken: "calendar-token", events: map[string]map[string]any{},
		eventIDsByMarker: map[string]string{}, transactionIDs: map[string]bool{}, counts: map[string]int{},
	}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeGraph) serveHTTP(response http.ResponseWriter, request *http.Request) {
	contents, err := io.ReadAll(request.Body)
	if err != nil {
		writeFakeJSON(response, http.StatusBadRequest, map[string]any{"error": map[string]any{"code": "invalidRequest"}})
		return
	}
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.counts["any"]++
	if request.Method == http.MethodPost && request.URL.Path == "/"+integrationTenantID+"/oauth2/v2.0/token" {
		provider.counts["token"]++
		if !strings.Contains(string(contents), "grant_type=client_credentials") {
			writeFakeJSON(response, http.StatusBadRequest, map[string]any{"error": "unsupported_grant_type"})
			return
		}
		writeFakeJSON(response, http.StatusOK, map[string]any{"token_type": "Bearer", "expires_in": 3599, "access_token": provider.expectedAccessToken})
		return
	}
	if request.Header.Get("Authorization") != "Bearer "+provider.expectedAccessToken {
		writeFakeJSON(response, http.StatusUnauthorized, map[string]any{"error": map[string]any{"code": "InvalidAuthenticationToken"}})
		return
	}
	path, isMailboxPath := strings.CutPrefix(request.URL.Path, provider.mailboxRoot)
	if !isMailboxPath {
		writeFakeJSON(response, http.StatusNotFound, map[string]any{"error": map[string]any{"code": "ResourceNotFound"}})
		return
	}
	var body map[string]any
	if len(contents) != 0 && json.Unmarshal(contents, &body) != nil {
		writeFakeJSON(response, http.StatusBadRequest, map[string]any{"error": map[string]any{"code": "invalidRequest"}})
		return
	}
	segments := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case request.Method == http.MethodPost && path == "/calendar/getSchedule":
		provider.serveSchedule(response, body)
	case request.Method == http.MethodGet && path == "/events":
		provider.serveMarkerLookup(response, request.URL.Query().Get("$filter"))
	case request.Method == http.MethodPost && (path == "/calendar/events" || (len(segments) == 3 && segments[0] == "calendars" && segments[2] == "events")):
		provider.serveCreate(response, request.URL.Path, string(contents), body)
	case request.Method == http.MethodGet && len(segments) == 2 && segments[0] == "events":
		event, exists := provider.events[segments[1]]
		if !exists {
			writeFakeJSON(response, http.StatusNotFound, map[string]any{"error": map[string]any{"code": "ErrorItemNotFound"}})
			return
		}
		writeFakeJSON(response, http.StatusOK, event)
	case request.Method == http.MethodGet && strings.HasSuffix(path, "/calendarView"):
		provider.counts["list"]++
		items := append([]map[string]any{}, provider.listedEvents...)
		for _, event := range provider.events {
			items = append(items, event)
		}
		writeFakeJSON(response, http.StatusOK, map[string]any{"value": items})
	case request.Method == http.MethodPatch && len(segments) == 2 && segments[0] == "events":
		provider.servePatch(response, segments[1], request.Header.Get("If-Match"), string(contents), body)
	default:
		writeFakeJSON(response, http.StatusNotFound, map[string]any{"error": map[string]any{"code": "ResourceNotFound"}})
	}
}

func (provider *fakeGraph) serveSchedule(response http.ResponseWriter, body map[string]any) {
	provider.counts["schedule"]++
	provider.scheduleRequests = append(provider.scheduleRequests, body)
	var schedules []any
	for _, value := range body["schedules"].([]any) {
		scheduleID := value.(string)
		information := map[string]any{"scheduleId": scheduleID, "availabilityView": "00", "scheduleItems": []any{}}
		if responseCode, ok := provider.scheduleErrors[scheduleID]; ok {
			information = map[string]any{"scheduleId": scheduleID, "availabilityView": "", "scheduleItems": []any{},
				"error": map[string]any{"message": "The recipient was not found.", "responseCode": responseCode}}
		} else if provider.busyAttendees[scheduleID] {
			information["availabilityView"] = "22"
			information["scheduleItems"] = []any{map[string]any{"status": "busy", "start": body["startTime"], "end": body["endTime"]}}
		}
		schedules = append(schedules, information)
	}
	writeFakeJSON(response, http.StatusOK, map[string]any{"value": schedules})
}

func (provider *fakeGraph) serveMarkerLookup(response http.ResponseWriter, filter string) {
	_, afterValue, hasValue := strings.Cut(filter, "ep/value eq '")
	key, _, hasEnd := strings.Cut(afterValue, "'")
	if !hasValue || !hasEnd || !strings.Contains(filter, "Name DexIdempotencyKey") {
		writeFakeJSON(response, http.StatusBadRequest, map[string]any{"error": map[string]any{"code": "invalidRequest"}})
		return
	}
	eventID, exists := provider.eventIDsByMarker[key]
	if !exists {
		provider.counts["lookupMissing"]++
		writeFakeJSON(response, http.StatusOK, map[string]any{"value": []any{}})
		return
	}
	provider.counts["lookupFound"]++
	writeFakeJSON(response, http.StatusOK, map[string]any{"value": []any{provider.events[eventID]}})
}

func (provider *fakeGraph) serveCreate(response http.ResponseWriter, path string, contents string, body map[string]any) {
	provider.counts["create"]++
	provider.createRequests = append(provider.createRequests, fakeRecordedRequest{path: path, body: contents, payload: body})
	transactionID, _ := body["transactionId"].(string)
	if provider.transactionIDs[transactionID] {
		provider.counts["createRepeatedTransaction"]++
		writeFakeJSON(response, http.StatusConflict, map[string]any{"error": map[string]any{"code": "ErrorDuplicateTransactionId"}})
		return
	}
	provider.transactionIDs[transactionID] = true
	isFirstCreate := provider.counts["create"] == 1
	if isFirstCreate && provider.firstCreateDelayBeforeStore > 0 {
		// Graph accepted the POST but commits the event only after the delay.
		provider.mutex.Unlock()
		time.Sleep(provider.firstCreateDelayBeforeStore)
		provider.mutex.Lock()
	}
	event, err := storedEventFromCreate(fmt.Sprintf("event-%d", len(provider.events)+1), body)
	if err != nil {
		writeFakeJSON(response, http.StatusBadRequest, map[string]any{"error": map[string]any{"code": "ErrorInvalidRequest"}})
		return
	}
	provider.events[event["id"].(string)] = event
	for _, property := range body["singleValueExtendedProperties"].([]any) {
		provider.eventIDsByMarker[property.(map[string]any)["value"].(string)] = event["id"].(string)
	}
	if isFirstCreate && provider.firstCreateDelayAfterStore > 0 {
		// Graph has stored the event; the response arrives late while other requests proceed.
		provider.mutex.Unlock()
		time.Sleep(provider.firstCreateDelayAfterStore)
		provider.mutex.Lock()
	}
	if isFirstCreate && provider.losesFirstCreateResponse {
		writeFakeJSON(response, http.StatusServiceUnavailable, map[string]any{"error": map[string]any{"code": "serviceNotAvailable"}})
		return
	}
	writeFakeJSON(response, http.StatusCreated, event)
}

func (provider *fakeGraph) servePatch(response http.ResponseWriter, eventID string, ifMatch string, contents string, body map[string]any) {
	provider.counts["patch"]++
	provider.patchRequests = append(provider.patchRequests, fakeRecordedRequest{ifMatch: ifMatch, body: contents, payload: body})
	event, exists := provider.events[eventID]
	if !exists {
		writeFakeJSON(response, http.StatusNotFound, map[string]any{"error": map[string]any{"code": "ErrorItemNotFound"}})
		return
	}
	if ifMatch != event["@odata.etag"] {
		writeFakeJSON(response, http.StatusPreconditionFailed, map[string]any{"error": map[string]any{"code": "ErrorIrresolvableConflict"}})
		return
	}
	if attendees, ok := body["attendees"].([]any); ok {
		for _, attendee := range attendees {
			attendee.(map[string]any)["status"] = map[string]any{"response": "none"}
		}
		event["attendees"] = attendees
	}
	event["@odata.etag"] = `W/"2"`
	writeFakeJSON(response, http.StatusOK, event)
}

// storedEventFromCreate converts the request's wall times and zones into the UTC form Graph returns.
func storedEventFromCreate(eventID string, body map[string]any) (map[string]any, error) {
	start, err := fakeUTCBoundary(body["start"])
	if err != nil {
		return nil, err
	}
	end, err := fakeUTCBoundary(body["end"])
	if err != nil {
		return nil, err
	}
	attendees, _ := body["attendees"].([]any)
	if attendees == nil {
		attendees = []any{}
	}
	return map[string]any{
		"@odata.etag": `W/"1"`, "id": eventID, "subject": body["subject"], "showAs": "busy", "type": "singleInstance",
		"isOrganizer": true, "transactionId": body["transactionId"], "start": start, "end": end, "attendees": attendees,
		"isOnlineMeeting": body["isOnlineMeeting"] == true, "onlineMeetingProvider": body["onlineMeetingProvider"],
		"originalStartTimeZone": body["start"].(map[string]any)["timeZone"],
	}, nil
}

func fakeUTCBoundary(value any) (map[string]any, error) {
	boundary, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("boundary is missing")
	}
	location, err := time.LoadLocation(boundary["timeZone"].(string))
	if err != nil {
		return nil, err
	}
	wallTime, err := time.ParseInLocation(graphUTCWallTime, boundary["dateTime"].(string), location)
	if err != nil {
		return nil, err
	}
	return utcBoundary(wallTime.UTC().Format(graphUTCWallTime)), nil
}

func (provider *fakeGraph) seedListedEvents(events ...map[string]any) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.listedEvents = append(provider.listedEvents, events...)
}

func (provider *fakeGraph) count(name string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.counts[name]
}

func (provider *fakeGraph) storedEventCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.events)
}

func (provider *fakeGraph) transactionIDCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.transactionIDs)
}

func (provider *fakeGraph) lastScheduleRequest() map[string]any {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.scheduleRequests[len(provider.scheduleRequests)-1]
}

func (provider *fakeGraph) lastCreateRequest() fakeRecordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.createRequests[len(provider.createRequests)-1]
}

func (provider *fakeGraph) lastPatchRequest() fakeRecordedRequest {
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
	response.Header().Set("request-id", "fake-graph-request")
	response.WriteHeader(status)
	if _, err := response.Write(contents); err != nil {
		panic(err)
	}
}

func newStaticConnection(t *testing.T, providerURL string) outlookcalendar.Connection {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "microsoft", Name: ConnectionName}
	client, err := outlookcalendar.New(outlookcalendar.Config{}, sdkgo.StaticCredentialProvider[outlookcalendar.Credentials]{
		reference: {AuthMethodID: outlookcalendar.MicrosoftOAuthAuthMethodID, AccessToken: sdkgo.NewSecretString("calendar-token")},
	}, outlookcalendar.WithLocalProviderURL(providerURL))
	require.NoError(t, err)
	connection, err := outlookcalendar.NewConnection(client, reference)
	require.NoError(t, err)
	return connection
}

// newLocalAppOnlyConnection loads the record Dex Web writes for an app-only connection, as main does.
func newLocalAppOnlyConnection(t *testing.T, providerURL string) outlookcalendar.Connection {
	t.Helper()
	record := map[string]any{
		"connectorId": outlookcalendar.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-calendar",
		"moduleVersion": "v0.1.0", "provider": "microsoft", "connectionName": ConnectionName, "authMethodId": outlookcalendar.AppOnlyAuthMethodID,
		"configuration": map[string]any{"tenantId": integrationTenantID, "mailbox": integrationMailbox},
		"credentials":   map[string]any{"auth_method": outlookcalendar.AppOnlyAuthMethodID, "client_id": "client-id", "client_secret": "fake-client" + "-secret"},
	}
	contents, err := json.Marshal(map[string]any{"schemaVersion": localconfig.SchemaVersion, "connections": []any{record}})
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "connections.json")
	require.NoError(t, os.WriteFile(path, contents, 0o600))
	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)
	connection, err := outlookcalendar.NewLocalConnection(store, ConnectionName, outlookcalendar.WithLocalProviderURL(providerURL))
	require.NoError(t, err)
	return connection
}

type bookMeetingHarness struct {
	cache        *blobcache.Cache
	worker       *dex.Worker
	workerResult chan error
	client       *dex.Client
}

func newBookMeetingHarness(t *testing.T, connection outlookcalendar.Connection, selection CalendarSelection) (*Flow, *bookMeetingHarness) {
	t.Helper()
	flow := NewFlow(connection, selection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	workerAddress := net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	serverAddress := environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
	harness := &bookMeetingHarness{cache: cache}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	harness.worker, err = dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: workerAddress, FlowServiceAddress: serverAddress, WorkerTarget: dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	harness.workerResult = make(chan error, 1)
	go func() { harness.workerResult <- harness.worker.Start() }()
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
