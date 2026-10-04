//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bookedmeeting

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
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoom"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationAccessToken = "zoom-integration-access-token"
	integrationHostID      = "host-30R7kT7bTIKSNUFEuH"

	// Each scenario is selected by the booking topic the fake receives.
	topicScheduled       = "Consultation"
	topicSlowCreate      = "Slow create"
	topicStalledCreate   = "Stalled create"
	topicServerError     = "Server error"
	topicRejected        = "Hosting not allowed"
	topicSlowReschedule  = "Slow reschedule"
	topicDeletedMeeting  = "Deleted meeting"
	topicNoShow          = "No show"
	topicWorkerLost      = "Worker lost"
	topicAttendanceTimer = "Attendance timer"

	// slowProviderDelay outlasts Dex's roughly seven-second async local phase.
	slowProviderDelay = 9 * time.Second
	// providerRequestTimeout lets a slow provider answer; the stalled-create harness uses a shorter one.
	providerRequestTimeout = 15 * time.Second
	stalledRequestTimeout  = 500 * time.Millisecond
	// workerLossInFlight is how long the create request is in flight when the Worker is killed.
	workerLossInFlight = time.Second
)

func TestBookedMeetingIsCreatedOnceAndRecordsItsJoinURLWithRealDex(t *testing.T) {
	provider, harness := newBookedMeetingHarness(t, providerRequestTimeout, &AttendancePolicy{})
	ctx := integrationContext(t)
	booking := futureBooking(topicScheduled, time.Hour)

	flowID := harness.startBookedMeeting(t, ctx, "scheduled", booking)
	record := harness.waitForPhase(t, ctx, flowID, PhaseScheduled)
	meeting := provider.meetingByTopic(t, topicScheduled)
	require.Equal(t, meeting.id, record.MeetingID)
	require.Equal(t, meeting.joinURL(), record.JoinURL)
	require.Equal(t, integrationHostID, record.HostID)
	require.Equal(t, 1, provider.createCount(topicScheduled))
	created := provider.createBody(t, topicScheduled, 0)
	require.Equal(t, mustParseTime(t, booking.StartTime).UTC().Format("2006-01-02T15:04:05Z"), created["start_time"], "Zoom receives the instant in UTC")
	require.Equal(t, "America/Los_Angeles", created["timezone"])
	require.Equal(t, map[string]any{"waiting_room": true}, created["settings"])
}

// TestSlowCreateIsSentOnceWithRealDex guards sync durability; an async fallback attempt would create twice.
func TestSlowCreateIsSentOnceWithRealDex(t *testing.T) {
	provider, harness := newBookedMeetingHarness(t, providerRequestTimeout, &AttendancePolicy{})
	ctx := integrationContext(t)

	flowID := harness.startBookedMeeting(t, ctx, "slow-create", futureBooking(topicSlowCreate, time.Hour))
	record := harness.waitForPhase(t, ctx, flowID, PhaseScheduled)
	require.Equal(t, provider.meetingByTopic(t, topicSlowCreate).id, record.MeetingID)
	require.Equal(t, 1, provider.createCount(topicSlowCreate), "one Step execution sends one create request")
	require.Nil(t, record.UncertainCreate)
}

func TestUnknownCreateIsAdoptedFromTheMeetingListWithoutCreatingAgainWithRealDex(t *testing.T) {
	provider, harness := newBookedMeetingHarness(t, stalledRequestTimeout, &AttendancePolicy{})
	ctx := integrationContext(t)

	flowID := harness.startBookedMeeting(t, ctx, "stalled-create", futureBooking(topicStalledCreate, time.Hour))
	record := harness.waitForPhase(t, ctx, flowID, PhaseScheduled)
	require.NotNil(t, record.UncertainCreate)
	require.Equal(t, sdkgo.FailureTransport, record.UncertainCreate.FailureKind)
	require.Equal(t, "adopted the meeting the unknown create made", record.Note)
	require.Equal(t, provider.meetingByTopic(t, topicStalledCreate).id, record.MeetingID)
	require.NotEmpty(t, record.JoinURL)
	require.Equal(t, 1, provider.createCount(topicStalledCreate), "reconciliation adopted the meeting instead of creating again")
	require.Equal(t, 1, provider.listCount())
}

func TestUnknownCreateWithoutAMatchStopsForAnOperatorWithRealDex(t *testing.T) {
	provider, harness := newBookedMeetingHarness(t, providerRequestTimeout, &AttendancePolicy{})
	ctx := integrationContext(t)

	flowID := harness.startBookedMeeting(t, ctx, "server-error", futureBooking(topicServerError, time.Hour))
	record := harness.waitForFlowOutput(t, ctx, flowID)
	require.Equal(t, PhaseNeedsReconciliation, record.Phase)
	require.Equal(t, sdkgo.FailureAvailability, record.UncertainCreate.FailureKind)
	require.Contains(t, record.Note, "no matching meeting")
	require.Zero(t, record.MeetingID)
	require.Equal(t, 1, provider.createCount(topicServerError), "a 5xx after dispatch is never retried")
}

func TestRejectedCreateCompletesWithZoomsErrorCodeWithRealDex(t *testing.T) {
	provider, harness := newBookedMeetingHarness(t, providerRequestTimeout, &AttendancePolicy{})
	ctx := integrationContext(t)

	flowID := harness.startBookedMeeting(t, ctx, "rejected", futureBooking(topicRejected, time.Hour))
	record := harness.waitForFlowOutput(t, ctx, flowID)
	require.Equal(t, PhaseRejected, record.Phase)
	require.Equal(t, &ZoomRefusal{FailureKind: sdkgo.FailureProviderRejection, ZoomErrorCode: "3161"}, record.Refusal)
	require.Equal(t, 1, provider.createCount(topicRejected))
}

func TestRescheduleMovesAndReadsBackTheMeetingThenRecordsAttendanceWithRealDex(t *testing.T) {
	provider, harness := newBookedMeetingHarness(t, providerRequestTimeout, &AttendancePolicy{CheckDelay: time.Hour})
	ctx := integrationContext(t)
	flowID := harness.startBookedMeeting(t, ctx, "reschedule", futureBooking(topicScheduled, time.Hour))
	scheduled := harness.waitForPhase(t, ctx, flowID, PhaseScheduled)

	moved := RescheduleBookedMeetingInput{StartTime: futureStart(3 * time.Hour).In(berlin(t)).Format(time.RFC3339), TimeZone: "Europe/Berlin", DurationMinutes: 45}
	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, harness.flow.RescheduleBookedMeeting, moved, nil))
	rescheduled := harness.waitForRecord(t, ctx, flowID, func(record BookedMeeting) bool {
		return record.Phase == PhaseScheduled && record.Reschedules == 1
	})
	require.True(t, rescheduled.StartTime.Equal(mustParseTime(t, moved.StartTime)))
	require.Equal(t, 45, rescheduled.DurationMinutes)
	require.Equal(t, "Europe/Berlin", rescheduled.TimeZone)
	require.Nil(t, rescheduled.PendingReschedule)
	require.Equal(t, scheduled.MeetingID, rescheduled.MeetingID)
	require.Equal(t, []map[string]any{{
		"start_time": mustParseTime(t, moved.StartTime).UTC().Format("2006-01-02T15:04:05Z"), "timezone": "Europe/Berlin", "duration": float64(45),
	}}, provider.patchBodies(scheduled.MeetingID))
	require.Equal(t, 1, provider.getCount(scheduled.MeetingID), "the reschedule is read back once")

	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, harness.flow.CheckBookedMeetingAttendance, nil, nil))
	attended := harness.waitForFlowOutput(t, ctx, flowID)
	require.Equal(t, PhaseAttended, attended.Phase)
	require.Equal(t, 1, attended.GuestJoins, "the host's own join does not count")
	require.Equal(t, 1, provider.createCount(topicScheduled))
}

// TestSlowRescheduleIsSafeToRepeatWithRealDex shows why updateMeeting stays async: a repeated patch sets the same values.
func TestSlowRescheduleIsSafeToRepeatWithRealDex(t *testing.T) {
	provider, harness := newBookedMeetingHarness(t, providerRequestTimeout, &AttendancePolicy{CheckDelay: time.Hour})
	ctx := integrationContext(t)
	flowID := harness.startBookedMeeting(t, ctx, "slow-reschedule", futureBooking(topicSlowReschedule, time.Hour))
	scheduled := harness.waitForPhase(t, ctx, flowID, PhaseScheduled)

	moved := RescheduleBookedMeetingInput{StartTime: futureStart(5 * time.Hour).Format(time.RFC3339), TimeZone: "America/Los_Angeles", DurationMinutes: 30}
	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, harness.flow.RescheduleBookedMeeting, moved, nil))
	rescheduled := harness.waitForRecord(t, ctx, flowID, func(record BookedMeeting) bool {
		return record.Phase == PhaseScheduled && record.Reschedules == 1
	})
	require.True(t, rescheduled.StartTime.Equal(mustParseTime(t, moved.StartTime)))
	patches := provider.patchBodies(scheduled.MeetingID)
	require.NotEmpty(t, patches)
	for _, patch := range patches {
		require.Equal(t, patches[0], patch, "every dispatch of the Step sends the same absolute values")
	}
	t.Logf("a %s patch was dispatched %d times", slowProviderDelay, len(patches))
}

func TestRescheduleOfADeletedMeetingCompletesAsCancelledWithRealDex(t *testing.T) {
	provider, harness := newBookedMeetingHarness(t, providerRequestTimeout, &AttendancePolicy{CheckDelay: time.Hour})
	ctx := integrationContext(t)
	flowID := harness.startBookedMeeting(t, ctx, "deleted", futureBooking(topicDeletedMeeting, time.Hour))
	scheduled := harness.waitForPhase(t, ctx, flowID, PhaseScheduled)
	provider.deleteMeeting(scheduled.MeetingID)

	moved := RescheduleBookedMeetingInput{StartTime: futureStart(2 * time.Hour).Format(time.RFC3339), TimeZone: "America/Los_Angeles", DurationMinutes: 30}
	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, harness.flow.RescheduleBookedMeeting, moved, nil))
	record := harness.waitForFlowOutput(t, ctx, flowID)
	require.Equal(t, PhaseCancelled, record.Phase)
}

func TestMeetingWithoutAnEndedInstanceIsANoShowWithRealDex(t *testing.T) {
	_, harness := newBookedMeetingHarness(t, providerRequestTimeout, &AttendancePolicy{CheckDelay: time.Hour})
	ctx := integrationContext(t)
	flowID := harness.startBookedMeeting(t, ctx, "no-show", futureBooking(topicNoShow, time.Hour))
	harness.waitForPhase(t, ctx, flowID, PhaseScheduled)

	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, harness.flow.CheckBookedMeetingAttendance, nil, nil))
	record := harness.waitForFlowOutput(t, ctx, flowID)
	require.Equal(t, PhaseNoShow, record.Phase)
	require.Contains(t, record.Note, "no ended instance")
	require.Error(t, harness.client.InvokeRPC(ctx, flowID, harness.flow.RescheduleBookedMeeting,
		RescheduleBookedMeetingInput{StartTime: futureStart(time.Hour).Format(time.RFC3339), TimeZone: "UTC", DurationMinutes: 30}, nil),
		"a completed booking cannot be rescheduled")
}

// TestAttendanceTimerFiresAfterTheScheduledEndWithRealDex waits for the shortest Zoom meeting, one minute, to end.
func TestAttendanceTimerFiresAfterTheScheduledEndWithRealDex(t *testing.T) {
	provider, harness := newBookedMeetingHarness(t, providerRequestTimeout, &AttendancePolicy{})
	ctx := integrationContext(t)
	booking := futureBooking(topicAttendanceTimer, 3*time.Second)
	booking.DurationMinutes = 1

	flowID := harness.startBookedMeeting(t, ctx, "attendance-timer", booking)
	harness.waitForPhase(t, ctx, flowID, PhaseScheduled)
	// The Timer outlasts one WaitForFlow long poll, so poll the record until it completes.
	harness.waitForPhase(t, ctx, flowID, PhaseAttended)
	record := harness.waitForFlowOutput(t, ctx, flowID)
	require.Equal(t, PhaseAttended, record.Phase)
	scheduledEnd := mustParseTime(t, booking.StartTime).Add(time.Minute)
	require.False(t, provider.firstParticipantsReadAt().Before(scheduledEnd), "attendance is read only after the scheduled end")
}

// TestWorkerLostDuringCreateIsReconciledWithoutCreatingAgainWithRealDex kills a Worker process one second
// into its create request. The replacement attempt finds the dispatch checkpoint and reconciles instead.
func TestWorkerLostDuringCreateIsReconciledWithoutCreatingAgainWithRealDex(t *testing.T) {
	provider := newFakeZoom(t)
	ctx := integrationContext(t)
	workerAddress := net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	process := startWorkerProcess(t, provider, workerAddress)

	policy := DefaultAttendancePolicy()
	harness := newHarnessWithWorkerAddress(t, provider, providerRequestTimeout, &policy, workerAddress, false)
	flowID := harness.startBookedMeeting(t, ctx, "worker-lost", futureBooking(topicWorkerLost, time.Hour))
	select {
	case <-provider.workerLostCreateReceived:
	case <-ctx.Done():
		t.Fatal("the Worker process never sent the create request")
	}
	time.Sleep(workerLossInFlight)
	require.NoError(t, process.Process.Signal(syscall.SIGKILL))
	_ = process.Wait() // The killed process always exits with a signal error.
	harness.startWorker(t)

	record := harness.waitForPhase(t, ctx, flowID, PhaseScheduled)
	require.NotNil(t, record.UncertainCreate, "the replacement attempt reported the unknown outcome")
	require.Equal(t, "adopted the meeting the unknown create made", record.Note)
	require.Equal(t, provider.meetingByTopic(t, topicWorkerLost).id, record.MeetingID)
	require.Equal(t, 1, provider.createCount(topicWorkerLost), "the replacement attempt did not create the meeting again")
}

func futureBooking(topic string, startsIn time.Duration) Input {
	return Input{
		BookingID: "BK-" + strconv.FormatInt(time.Now().UnixNano(), 36), Topic: topic,
		StartTime: futureStart(startsIn).Format(time.RFC3339), TimeZone: "America/Los_Angeles", DurationMinutes: 30, Agenda: "Intro call",
	}
}

// futureStart is a whole-second start in a Pacific offset, so the UTC instant differs from the text.
func futureStart(startsIn time.Duration) time.Time {
	location, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		panic(err)
	}
	return time.Now().Add(startsIn).Truncate(time.Second).In(location)
}

func berlin(t *testing.T) *time.Location {
	t.Helper()
	location, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	return location
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	require.NoError(t, err)
	return parsed
}

func integrationContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// fakeZoom is a credential-safe Zoom Meetings fake whose behavior is chosen by the meeting topic.
type fakeZoom struct {
	*httptest.Server
	t                        *testing.T
	mutex                    sync.Mutex
	nextMeetingID            int64
	meetings                 map[int64]*fakeMeeting
	createBodies             map[string][]map[string]any
	patchBodiesByMeeting     map[int64][]map[string]any
	getsByMeeting            map[int64]int
	lists                    int
	participantsReadAt       []time.Time
	workerLostCreateReceived chan struct{}
	signalWorkerLostCreate   sync.Once
}

type fakeMeeting struct {
	id        int64
	topic     string
	start     time.Time
	duration  int
	timeZone  string
	createdAt time.Time
}

func newFakeZoom(t *testing.T) *fakeZoom {
	t.Helper()
	provider := &fakeZoom{
		t: t, nextMeetingID: 92674392836, meetings: map[int64]*fakeMeeting{}, createBodies: map[string][]map[string]any{},
		patchBodiesByMeeting: map[int64][]map[string]any{}, getsByMeeting: map[int64]int{},
		workerLostCreateReceived: make(chan struct{}),
	}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeZoom) serveHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Authorization") != "Bearer "+integrationAccessToken {
		provider.writeJSON(response, http.StatusUnauthorized, `{"code":124,"message":"Invalid access token."}`)
		return
	}
	path := strings.TrimPrefix(request.URL.Path, "/v2")
	switch {
	case request.Method == http.MethodPost && path == "/users/me/meetings":
		provider.createMeeting(response, request)
	case request.Method == http.MethodGet && path == "/users/me/meetings":
		provider.listMeetings(response)
	case strings.HasPrefix(path, "/past_meetings/") && strings.HasSuffix(path, "/participants"):
		provider.listParticipants(response, strings.TrimSuffix(strings.TrimPrefix(path, "/past_meetings/"), "/participants"))
	case request.Method == http.MethodGet && strings.HasPrefix(path, "/meetings/"):
		provider.readMeeting(response, strings.TrimPrefix(path, "/meetings/"))
	case request.Method == http.MethodPatch && strings.HasPrefix(path, "/meetings/"):
		provider.patchMeeting(response, request, strings.TrimPrefix(path, "/meetings/"))
	default:
		provider.writeJSON(response, http.StatusNotFound, `{"code":404,"message":"SENTINEL unknown path"}`)
	}
}

func (provider *fakeZoom) createMeeting(response http.ResponseWriter, request *http.Request) {
	body := provider.decodeBody(request)
	topic, _ := body["topic"].(string)
	provider.mutex.Lock()
	provider.createBodies[topic] = append(provider.createBodies[topic], body)
	provider.mutex.Unlock()
	switch topic {
	case topicServerError:
		provider.writeJSON(response, http.StatusInternalServerError, `{"code":500,"message":"SENTINEL internal error"}`)
		return
	case topicRejected:
		provider.writeJSON(response, http.StatusBadRequest, `{"code":3161,"message":"SENTINEL not allowed to host"}`)
		return
	}
	meeting := provider.storeMeeting(body)
	switch topic {
	case topicSlowCreate:
		time.Sleep(slowProviderDelay)
	case topicStalledCreate:
		// Zoom created the meeting, but the response never arrives before the connector gives up.
		<-request.Context().Done()
		return
	case topicWorkerLost:
		provider.signalWorkerLostCreate.Do(func() { close(provider.workerLostCreateReceived) })
		<-request.Context().Done()
		return
	}
	provider.writeJSON(response, http.StatusCreated, provider.meetingJSON(meeting))
}

func (provider *fakeZoom) storeMeeting(body map[string]any) *fakeMeeting {
	start, err := time.Parse("2006-01-02T15:04:05Z", body["start_time"].(string))
	require.NoError(provider.t, err)
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.nextMeetingID++
	meeting := &fakeMeeting{
		id: provider.nextMeetingID, topic: body["topic"].(string), start: start, duration: int(body["duration"].(float64)),
		timeZone: body["timezone"].(string), createdAt: time.Now().UTC(),
	}
	provider.meetings[meeting.id] = meeting
	return meeting
}

func (provider *fakeZoom) listMeetings(response http.ResponseWriter) {
	provider.mutex.Lock()
	provider.lists++
	items := make([]string, 0, len(provider.meetings))
	for _, meeting := range provider.meetings {
		items = append(items, provider.meetingJSONLocked(meeting))
	}
	provider.mutex.Unlock()
	provider.writeJSON(response, http.StatusOK, `{"page_size":300,"total_records":`+strconv.Itoa(len(items))+`,"next_page_token":"","meetings":[`+strings.Join(items, ",")+`]}`)
}

func (provider *fakeZoom) readMeeting(response http.ResponseWriter, meetingID string) {
	provider.mutex.Lock()
	id, _ := strconv.ParseInt(meetingID, 10, 64)
	meeting, found := provider.meetings[id]
	provider.getsByMeeting[id]++
	provider.mutex.Unlock()
	if !found {
		provider.writeJSON(response, http.StatusNotFound, `{"code":3001,"message":"SENTINEL Meeting does not exist"}`)
		return
	}
	provider.writeJSON(response, http.StatusOK, provider.meetingJSON(meeting))
}

func (provider *fakeZoom) patchMeeting(response http.ResponseWriter, request *http.Request, meetingID string) {
	body := provider.decodeBody(request)
	id, _ := strconv.ParseInt(meetingID, 10, 64)
	provider.mutex.Lock()
	provider.patchBodiesByMeeting[id] = append(provider.patchBodiesByMeeting[id], body)
	meeting, found := provider.meetings[id]
	provider.mutex.Unlock()
	if !found {
		provider.writeJSON(response, http.StatusNotFound, `{"code":3001,"message":"SENTINEL Meeting does not exist"}`)
		return
	}
	if meeting.topic == topicSlowReschedule {
		time.Sleep(slowProviderDelay)
	}
	start, err := time.Parse("2006-01-02T15:04:05Z", body["start_time"].(string))
	require.NoError(provider.t, err)
	provider.mutex.Lock()
	meeting.start, meeting.timeZone, meeting.duration = start, body["timezone"].(string), int(body["duration"].(float64))
	provider.mutex.Unlock()
	response.WriteHeader(http.StatusNoContent)
}

func (provider *fakeZoom) listParticipants(response http.ResponseWriter, meetingID string) {
	id, _ := strconv.ParseInt(meetingID, 10, 64)
	provider.mutex.Lock()
	provider.participantsReadAt = append(provider.participantsReadAt, time.Now())
	meeting, found := provider.meetings[id]
	provider.mutex.Unlock()
	if !found || meeting.topic == topicNoShow {
		provider.writeJSON(response, http.StatusNotFound, `{"code":3001,"message":"SENTINEL Meeting does not exist"}`)
		return
	}
	provider.writeJSON(response, http.StatusOK, `{"page_size":300,"total_records":2,"next_page_token":"","participants":[`+
		`{"id":"`+integrationHostID+`","name":"Host","user_email":"host@example.com","join_time":"2026-10-08T16:00:00Z","leave_time":"2026-10-08T16:30:00Z","duration":1800},`+
		`{"id":"","name":"SENTINEL guest name","user_email":"","join_time":"2026-10-08T16:01:00Z","leave_time":"2026-10-08T16:29:00Z","duration":1680}]}`)
}

func (provider *fakeZoom) meetingJSON(meeting *fakeMeeting) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.meetingJSONLocked(meeting)
}

func (provider *fakeZoom) meetingJSONLocked(meeting *fakeMeeting) string {
	encoded, err := json.Marshal(map[string]any{
		"id": meeting.id, "uuid": "uuid-" + strconv.FormatInt(meeting.id, 10), "host_id": integrationHostID, "host_email": "host@example.com",
		"topic": meeting.topic, "type": 2, "status": "waiting", "start_time": meeting.start.UTC().Format("2006-01-02T15:04:05Z"),
		"duration": meeting.duration, "timezone": meeting.timeZone, "created_at": meeting.createdAt.Format(time.RFC3339),
		"join_url": meeting.joinURL(), "start_url": "https://zoom.example/s/1?zak=SENTINEL-start-url", "password": "SENTINEL",
	})
	require.NoError(provider.t, err)
	return string(encoded)
}

func (meeting *fakeMeeting) joinURL() string {
	return "https://us05web.zoom.us/j/" + strconv.FormatInt(meeting.id, 10) + "?pwd=join"
}

func (provider *fakeZoom) decodeBody(request *http.Request) map[string]any {
	contents, err := io.ReadAll(request.Body)
	require.NoError(provider.t, err)
	var body map[string]any
	require.NoError(provider.t, json.Unmarshal(contents, &body))
	return body
}

func (provider *fakeZoom) writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if _, err := response.Write([]byte(body)); err != nil {
		provider.t.Logf("fake Zoom response write failed: %v", err)
	}
}

func (provider *fakeZoom) meetingByTopic(t *testing.T, topic string) *fakeMeeting {
	t.Helper()
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var matches []*fakeMeeting
	for _, meeting := range provider.meetings {
		if meeting.topic == topic {
			matches = append(matches, meeting)
		}
	}
	require.Len(t, matches, 1, "Zoom holds exactly one %q meeting", topic)
	return matches[0]
}

func (provider *fakeZoom) deleteMeeting(meetingID int64) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	delete(provider.meetings, meetingID)
}

func (provider *fakeZoom) createCount(topic string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.createBodies[topic])
}

func (provider *fakeZoom) createBody(t *testing.T, topic string, index int) map[string]any {
	t.Helper()
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	require.Greater(t, len(provider.createBodies[topic]), index)
	return provider.createBodies[topic][index]
}

func (provider *fakeZoom) patchBodies(meetingID int64) []map[string]any {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]map[string]any(nil), provider.patchBodiesByMeeting[meetingID]...)
}

func (provider *fakeZoom) getCount(meetingID int64) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.getsByMeeting[meetingID]
}

func (provider *fakeZoom) listCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.lists
}

func (provider *fakeZoom) firstParticipantsReadAt() time.Time {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	if len(provider.participantsReadAt) == 0 {
		return time.Time{}
	}
	return provider.participantsReadAt[0]
}

type bookedMeetingHarness struct {
	provider      *fakeZoom
	flow          *Flow
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newBookedMeetingHarness(t *testing.T, requestTimeout time.Duration, policy *AttendancePolicy) (*fakeZoom, *bookedMeetingHarness) {
	t.Helper()
	provider := newFakeZoom(t)
	workerAddress := net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	return provider, newHarnessWithWorkerAddress(t, provider, requestTimeout, policy, workerAddress, true)
}

func newHarnessWithWorkerAddress(
	t *testing.T,
	provider *fakeZoom,
	requestTimeout time.Duration,
	policy *AttendancePolicy,
	workerAddress string,
	isWorkerStarted bool,
) *bookedMeetingHarness {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "zoom", Name: ConnectionName}
	providerClient, err := zoom.New(
		zoom.Config{Endpoint: provider.URL + "/v2"},
		sdkgo.StaticCredentialProvider[zoom.Credentials]{reference: {AccessToken: sdkgo.NewSecretString(integrationAccessToken)}},
		zoom.WithHTTPClient(&http.Client{Timeout: requestTimeout}),
	)
	require.NoError(t, err)
	connection, err := zoom.NewConnection(providerClient, reference)
	require.NoError(t, err)
	flow := NewFlow(connection, policy)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	harness := &bookedMeetingHarness{
		provider: provider, flow: flow, registry: registry, cache: cache, workerAddress: workerAddress,
		serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801"),
	}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		var stopErr error
		if harness.worker != nil {
			stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			stopErr = errors.Join(harness.worker.Stop(stopCtx), <-harness.workerResult)
		}
		require.NoError(t, errors.Join(stopErr, harness.client.Close(), harness.cache.Close()))
	})
	if isWorkerStarted {
		harness.startWorker(t)
	}
	return harness
}

func (harness *bookedMeetingHarness) startWorker(t *testing.T) {
	t.Helper()
	worker, err := dex.NewWorker(harness.registry, harness.cache, dex.WorkerOptions{
		BindAddress: harness.workerAddress, FlowServiceAddress: harness.serverAddress,
		WorkerTarget: dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	harness.worker = worker
	harness.workerResult = make(chan error, 1)
	go func() { harness.workerResult <- worker.Start() }()
}

func (harness *bookedMeetingHarness) startBookedMeeting(t *testing.T, ctx context.Context, scenario string, booking Input) string {
	t.Helper()
	flowID := "zoom-booked-meeting-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, harness.flow, flowID, booking, dex.StartFlowOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := harness.client.StopFlow(stopCtx, flowID, dex.StopOptions{Type: dex.CancelFlow, Reason: "integration test finished"})
		// Dex Go SDK releases name the closed-Flow error type differently; each embeds this sub-status.
		var serviceError *dex.ServiceError
		isClosedFlow := errors.As(err, &serviceError) && serviceError.SubStatus == dex.ErrorSubStatusFlowNotFound
		if !isClosedFlow {
			require.NoError(t, err, "a Flow still waiting for its meeting is cancelled")
		}
	})
	return flowID
}

func (harness *bookedMeetingHarness) waitForFlowOutput(t *testing.T, ctx context.Context, flowID string) BookedMeeting {
	t.Helper()
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s", flowID)
	var record BookedMeeting
	require.NoError(t, result.DecodeSingleOutput(&record))
	encoded, err := json.Marshal(record)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL", "no provider text, start URL, or passcode reaches Flow state")
	return record
}

func (harness *bookedMeetingHarness) waitForPhase(t *testing.T, ctx context.Context, flowID string, phase string) BookedMeeting {
	t.Helper()
	return harness.waitForRecord(t, ctx, flowID, func(record BookedMeeting) bool { return record.Phase == phase })
}

// waitForRecord polls until the record is expected; a record that reached a terminal phase first fails at once.
func (harness *bookedMeetingHarness) waitForRecord(t *testing.T, ctx context.Context, flowID string, isExpected func(BookedMeeting) bool) BookedMeeting {
	t.Helper()
	var record BookedMeeting
	require.Eventually(t, func() bool {
		record = BookedMeeting{}
		err := harness.client.InvokeRPC(ctx, flowID, harness.flow.GetBookedMeeting, nil, &record)
		return err == nil && (isExpected(record) || isTerminalPhase(record.Phase))
	}, 2*time.Minute, 100*time.Millisecond, "Flow %s last record %+v", flowID, &record)
	require.True(t, isExpected(record), "Flow %s stopped at %+v", flowID, record)
	return record
}

func isTerminalPhase(phase string) bool {
	switch phase {
	case PhaseAttended, PhaseNoShow, PhaseAttendanceUnknown, PhaseCancelled, PhaseRejected, PhaseNeedsReconciliation:
		return true
	default:
		return false
	}
}

// workerProcessEndpointEnvironmentVariable gives the fake Zoom endpoint to this test binary when
// startWorkerProcess runs it again as the Worker process the test kills.
const workerProcessEndpointEnvironmentVariable = "ZOOM_BOOKED_MEETING_WORKER_PROCESS_ENDPOINT"

// TestMain runs the Worker process instead of the tests when startWorkerProcess re-executes this binary.
func TestMain(m *testing.M) {
	if endpoint := os.Getenv(workerProcessEndpointEnvironmentVariable); endpoint != "" {
		if err := runWorkerProcess(endpoint); err != nil {
			fmt.Fprintln(os.Stderr, "booked meeting Worker process stopped:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runWorkerProcess registers the booked meeting Flow as the example's Worker does and serves it until the
// test kills the process.
func runWorkerProcess(endpoint string) error {
	reference := sdkgo.ConnectionRef{Provider: "zoom", Name: ConnectionName}
	providerClient, err := zoom.New(zoom.Config{Endpoint: endpoint},
		sdkgo.StaticCredentialProvider[zoom.Credentials]{reference: {AccessToken: sdkgo.NewSecretString(integrationAccessToken)}})
	if err != nil {
		return err
	}
	connection, err := zoom.NewConnection(providerClient, reference)
	if err != nil {
		return err
	}
	policy := DefaultAttendancePolicy()
	registry, err := dex.NewRegistry([]dex.Flow{NewFlow(connection, &policy)})
	if err != nil {
		return err
	}
	cache, err := blobcache.New(&blobcache.Config{Dir: os.Getenv("DEX_BLOB_CACHE_DIR"), MaxBytes: 1 << 30})
	if err != nil {
		return err
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: os.Getenv("DEX_WORKER_BIND_ADDRESS"), FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS"),
	})
	if err != nil {
		return errors.Join(err, cache.Close())
	}
	return errors.Join(worker.Start(), cache.Close())
}

// startWorkerProcess runs this test binary again as a Worker process against the fake, so the test can kill
// it mid-request.
func startWorkerProcess(t *testing.T, provider *fakeZoom, workerAddress string) *exec.Cmd {
	t.Helper()
	directory := t.TempDir()
	process := exec.Command(os.Args[0])
	process.Env = append(os.Environ(),
		workerProcessEndpointEnvironmentVariable+"="+provider.URL+"/v2", "DEX_WORKER_BIND_ADDRESS="+workerAddress,
		"DEX_FLOW_SERVICE_ADDRESS="+environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801"),
		"DEX_BLOB_CACHE_DIR="+filepath.Join(directory, "blobs"),
	)
	logFile, err := os.Create(filepath.Join(directory, "worker.log"))
	require.NoError(t, err)
	process.Stdout, process.Stderr = logFile, logFile
	require.NoError(t, process.Start())
	t.Cleanup(func() {
		// Kill is a no-op error after the test already killed the process.
		_ = process.Process.Kill()
		require.NoError(t, logFile.Close())
	})
	require.Eventually(t, func() bool {
		connection, err := net.DialTimeout("tcp", workerAddress, 100*time.Millisecond)
		if err != nil {
			return false
		}
		return connection.Close() == nil
	}, time.Minute, 100*time.Millisecond, "the Worker process listens on %s", workerAddress)
	return process
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
