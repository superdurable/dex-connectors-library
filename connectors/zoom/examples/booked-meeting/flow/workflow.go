// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package bookedmeeting demonstrates every Zoom operation in one booked
// consultation: schedule the meeting once and record its join URL, reconcile an
// unknown create from the meeting list instead of creating again, reschedule
// through a Dex Web Action, and record attendance after the meeting ends.
package bookedmeeting

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/connectors/zoom"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "ZoomBookedMeeting"
	// ConnectionName is the static Dex Web connection for Zoom.
	ConnectionName = "zoom-scheduler"
	// ManageBookedMeetingPermission is required by the reschedule and attendance Actions.
	ManageBookedMeetingPermission = "zoom-booked-meeting.manage"

	recordBookingStepType              = "RecordBooking"
	createBookedMeetingStepType        = "CreateBookedMeeting"
	recordScheduledMeetingStepType     = "RecordScheduledMeeting"
	recordRejectedMeetingStepType      = "RecordRejectedMeeting"
	recordUncertainMeetingStepType     = "RecordUncertainMeeting"
	findUncertainMeetingStepType       = "FindUncertainMeeting"
	evaluateUncertainMeetingStepType   = "EvaluateUncertainMeeting"
	awaitBookedMeetingStepType         = "AwaitBookedMeeting"
	moveBookedMeetingStepType          = "MoveBookedMeeting"
	readBackRescheduledMeetingStepType = "ReadBackRescheduledMeeting"
	recordRescheduledMeetingStepType   = "RecordRescheduledMeeting"
	recordCancelledMeetingStepType     = "RecordCancelledMeeting"
	listMeetingParticipantsStepType    = "ListMeetingParticipants"
	recordMeetingAttendanceStepType    = "RecordMeetingAttendance"

	// maximumListedMeetings is Zoom's largest page, which the reconciliation reads once.
	maximumListedMeetings = 300
	// reconciliationClockSkew tolerates a Zoom created_at slightly before the Worker's clock.
	reconciliationClockSkew = 5 * time.Minute
)

// Booking phases stored in the zoom-booked-meeting-phase Attribute.
const (
	// PhaseScheduling means the createMeeting Step is about to run or running.
	PhaseScheduling = "scheduling"
	// PhaseScheduled means Zoom holds the meeting and the Flow waits for it to end.
	PhaseScheduled = "scheduled"
	// PhaseRescheduling means a reschedule request is being applied and read back.
	PhaseRescheduling = "rescheduling"
	// PhaseCheckingAttendance means the Flow is listing the ended meeting's participants.
	PhaseCheckingAttendance = "checkingAttendance"
	// PhaseReconciling means the create outcome was unknown and the Flow is searching Zoom for the meeting.
	PhaseReconciling = "reconciling"
	// PhaseAttended means at least one guest other than the host joined.
	PhaseAttended = "attended"
	// PhaseNoShow means no guest joined, or Zoom has no ended instance of the meeting.
	PhaseNoShow = "noShow"
	// PhaseAttendanceUnknown means Zoom refused the participant listing, such as on a free account.
	PhaseAttendanceUnknown = "attendanceUnknown"
	// PhaseCancelled means the meeting was deleted in Zoom before it ended.
	PhaseCancelled = "cancelled"
	// PhaseRejected means Zoom conclusively refused to create the meeting; nothing was created.
	PhaseRejected = "rejected"
	// PhaseNeedsReconciliation means the meeting may exist but could not be identified; it was not created again.
	PhaseNeedsReconciliation = "needsReconciliation"
)

var (
	bookingPhaseAttribute   = dex.DefineAttribute[string]("zoom-booked-meeting-phase")
	bookedMeetingAttribute  = dex.DefineAttribute[BookedMeeting]("zoom-booked-meeting")
	rescheduleRequests      = dex.DefineChannel[RescheduleBookedMeetingInput]("zoom-booked-meeting-reschedule-requests")
	attendanceCheckRequests = dex.DefineChannel[dex.None]("zoom-booked-meeting-attendance-check-requests")

	bookingIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	errNotScheduled  = errors.New("the booked meeting is not scheduled")
)

// Input is the booked slot entered in Dex Web Start Flow.
type Input struct {
	// BookingID is the booking system's reference, 1 to 64 letters, digits, dots, underscores, or hyphens.
	BookingID string `json:"bookingId"`
	// Topic is the meeting topic, 1 to 200 characters.
	Topic string `json:"topic"`
	// StartTime is the slot start in RFC 3339 with an explicit offset, such as 2026-10-08T09:00:00-07:00.
	StartTime string `json:"startTime"`
	// TimeZone is the booker's IANA time zone, such as America/Los_Angeles; Zoom displays the meeting in it.
	TimeZone string `json:"timeZone"`
	// DurationMinutes is the slot length, from 1 to 1440 minutes.
	DurationMinutes int `json:"durationMinutes"`
	// Agenda is the optional meeting description, at most 2000 characters.
	Agenda string `json:"agenda,omitempty"`
}

// RescheduleBookedMeetingInput moves the booked meeting to another future slot.
type RescheduleBookedMeetingInput struct {
	// StartTime is the new start in RFC 3339 with an explicit offset.
	StartTime string `json:"startTime"`
	// TimeZone is the IANA time zone Zoom displays the meeting in.
	TimeZone string `json:"timeZone"`
	// DurationMinutes is the length of the moved meeting, from 1 to 1440 minutes.
	DurationMinutes int64 `json:"durationMinutes"`
}

// MeetingWait is the scheduled meeting the Flow waits on.
type MeetingWait struct {
	// MeetingID is Zoom's numeric meeting ID.
	MeetingID int64 `json:"meetingId"`
	// AttendanceCheckAt is when the Flow lists participants: the scheduled end plus the policy delay.
	AttendanceCheckAt time.Time `json:"attendanceCheckAt"`
}

// MeetingReschedule is one reschedule applied to the booked meeting.
type MeetingReschedule struct {
	// MeetingID is Zoom's numeric meeting ID.
	MeetingID int64 `json:"meetingId"`
	// Request is the validated reschedule request.
	Request RescheduleBookedMeetingInput `json:"request"`
}

// AttendanceCheck identifies the ended meeting whose participants the Flow lists.
type AttendanceCheck struct {
	// MeetingID is Zoom's numeric meeting ID.
	MeetingID int64 `json:"meetingId"`
}

// UncertainCreate records a dispatched create whose outcome Zoom did not confirm.
type UncertainCreate struct {
	// CallID is the connector call identity of the uncertain create.
	CallID string `json:"callId"`
	// ObservedAt is when the connector observed the unknown outcome.
	ObservedAt time.Time `json:"observedAt"`
	// FailureKind is the safe connector failure category, such as TRANSPORT.
	FailureKind sdkgo.FailureKind `json:"failureKind"`
}

// ZoomRefusal is the safe reason Zoom gave for refusing a request.
type ZoomRefusal struct {
	// FailureKind is the safe connector failure category.
	FailureKind sdkgo.FailureKind `json:"failureKind"`
	// ZoomErrorCode is Zoom's numeric error code, such as 3161 for a user who cannot host.
	ZoomErrorCode string `json:"zoomErrorCode,omitempty"`
}

// BookedMeeting is the Flow's durable record of one booked consultation.
type BookedMeeting struct {
	// Booking is the validated Start Flow input.
	Booking Input `json:"booking"`
	// Phase mirrors the zoom-booked-meeting-phase Attribute.
	Phase string `json:"phase"`
	// RecordedAt is when the Flow recorded the booking; reconciliation adopts only meetings Zoom created after it.
	RecordedAt time.Time `json:"recordedAt"`
	// MeetingID is Zoom's numeric meeting ID once Zoom confirmed or the Flow identified the meeting.
	MeetingID int64 `json:"meetingId,omitempty"`
	// JoinURL is the participant join link to send to the booker.
	JoinURL string `json:"joinUrl,omitempty"`
	// HostID is the Zoom user ID of the host, used to tell guests from the host in attendance.
	HostID string `json:"hostId,omitempty"`
	// StartTime is the start Zoom stores, in UTC.
	StartTime *time.Time `json:"startTime,omitempty"`
	// TimeZone is the zone Zoom displays the meeting in.
	TimeZone string `json:"timeZone,omitempty"`
	// DurationMinutes is the length Zoom stores.
	DurationMinutes int `json:"durationMinutes,omitempty"`
	// Reschedules counts applied reschedules.
	Reschedules int `json:"reschedules"`
	// PendingReschedule is the reschedule being applied.
	PendingReschedule *RescheduleBookedMeetingInput `json:"pendingReschedule,omitempty"`
	// UncertainCreate describes an unknown create outcome being, or that was, reconciled.
	UncertainCreate *UncertainCreate `json:"uncertainCreate,omitempty"`
	// Refusal is Zoom's reason after a rejected create or attendance listing.
	Refusal *ZoomRefusal `json:"refusal,omitempty"`
	// GuestJoins counts joins by participants other than the host.
	GuestJoins int `json:"guestJoins"`
	// Note explains a reconciliation or attendance outcome.
	Note string `json:"note,omitempty"`
}

// AttendancePolicy decides when the Flow lists participants after a meeting.
type AttendancePolicy struct {
	// CheckDelay is the wait after the scheduled end before listing
	// participants, so a meeting that runs over has ended; zero or more.
	CheckDelay time.Duration
}

// DefaultAttendancePolicy checks attendance 15 minutes after the scheduled end.
func DefaultAttendancePolicy() AttendancePolicy {
	return AttendancePolicy{CheckDelay: 15 * time.Minute}
}

// Flow schedules one booked consultation in Zoom and follows it to attendance.
type Flow struct {
	dex.FlowDefaults
	connection zoom.Connection
	policy     AttendancePolicy
}

// NewFlow binds the Zoom Connection and the attendance policy at registration time.
// It panics when policy is nil or its delay is negative.
func NewFlow(connection zoom.Connection, policy *AttendancePolicy) *Flow {
	if policy == nil || policy.CheckDelay < 0 {
		panic("booked meeting requires an attendance policy with a non-negative check delay")
	}
	return &Flow{connection: connection, policy: *policy}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the booking, Zoom, reconciliation, reschedule, and attendance Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordBooking{}),
		dex.DefineStep(zoom.NewCreateMeetingStep(zoom.CreateMeetingStepConfig[Input]{
			StepType: createBookedMeetingStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "zoom", GroupLabel: "Zoom",
				Explanation: "Create the meeting once; an unknown outcome is reconciled, never created again automatically.",
			},
			Connection: flow.connection, MapToOperationInput: MapToCreateMeetingInput,
			Created:          sdkgo.GoTo(recordScheduledMeeting{}),
			ProviderRejected: sdkgo.GoTo(recordRejectedMeeting{}),
			Uncertain:        sdkgo.GoTo(recordUncertainMeeting{}),
		})),
		dex.DefineStep(recordScheduledMeeting{attendanceCheckDelay: flow.policy.CheckDelay}),
		dex.DefineStep(recordRejectedMeeting{}),
		dex.DefineStep(recordUncertainMeeting{}),
		dex.DefineStep(zoom.NewListMeetingsStep(zoom.ListMeetingsStepConfig[UncertainCreate]{
			StepType: findUncertainMeetingStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "zoom", GroupLabel: "Zoom",
				Explanation: "List the host's upcoming meetings to find a meeting the unknown create may have made.",
			},
			Connection: flow.connection, MapToOperationInput: MapToFindUncertainMeetingInput,
			Listed: sdkgo.GoTo(evaluateUncertainMeeting{attendanceCheckDelay: flow.policy.CheckDelay}),
		})),
		dex.DefineStep(evaluateUncertainMeeting{attendanceCheckDelay: flow.policy.CheckDelay}),
		dex.DefineStep(awaitBookedMeeting{}),
		dex.DefineStep(zoom.NewUpdateMeetingStep(zoom.UpdateMeetingStepConfig[MeetingReschedule]{
			StepType: moveBookedMeetingStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "zoom", GroupLabel: "Zoom",
				Explanation: "Move the meeting to the requested slot; the patch sets absolute values, so a repeat is safe.",
			},
			Connection: flow.connection, MapToOperationInput: MapToUpdateMeetingInput,
			Updated:  sdkgo.GoTo(sdkgo.StepRef[zoom.UpdateMeetingResult](readBackRescheduledMeetingStepType)),
			NotFound: sdkgo.GoTo(recordCancelledMeeting{}),
		})),
		dex.DefineStep(zoom.NewGetMeetingStep(zoom.GetMeetingStepConfig[zoom.UpdateMeetingResult]{
			StepType: readBackRescheduledMeetingStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "zoom", GroupLabel: "Zoom",
				Explanation: "Read the meeting back, because Zoom's patch response carries no meeting.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetMeetingInput,
			Found: sdkgo.GoTo(recordRescheduledMeeting{attendanceCheckDelay: flow.policy.CheckDelay}),
		})),
		dex.DefineStep(recordRescheduledMeeting{attendanceCheckDelay: flow.policy.CheckDelay}),
		dex.DefineStep(recordCancelledMeeting{}),
		dex.DefineStep(zoom.NewListPastMeetingParticipantsStep(zoom.ListPastMeetingParticipantsStepConfig[AttendanceCheck]{
			StepType: listMeetingParticipantsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "zoom", GroupLabel: "Zoom",
				Explanation: "List who joined the ended meeting, up to Zoom's largest page.",
			},
			Connection: flow.connection, MapToOperationInput: MapToListPastMeetingParticipantsInput,
			Listed:           sdkgo.GoTo(recordMeetingAttendance{}),
			NotFound:         sdkgo.GoTo(recordMeetingAttendance{}),
			ProviderRejected: sdkgo.GoTo(recordMeetingAttendance{}),
		})),
		dex.DefineStep(recordMeetingAttendance{}),
	}
}

// GetRPCs returns the reschedule and attendance Actions, the record read RPC, and the Dex Web views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	bookingLocks := []dex.AttributeLock{dex.LockAttribute(bookingPhaseAttribute), dex.LockAttribute(bookedMeetingAttribute)}
	return []dex.RPCDef{
		dex.DefineRPC(flow.RescheduleBookedMeeting, &dex.RPCOptions{
			Action: dex.DefineAction(
				"Reschedule meeting",
				dex.WhenAttributeMatches(bookingPhaseAttribute, dex.AttributeMatchEqual(PhaseScheduled)),
				dex.ActionRequiresPermission(ManageBookedMeetingPermission),
			),
			LockAttributes: bookingLocks,
		}),
		dex.DefineRPC(flow.CheckBookedMeetingAttendance, &dex.RPCOptions{
			Action: dex.DefineAction(
				"Check attendance now",
				dex.WhenAttributeMatches(bookingPhaseAttribute, dex.AttributeMatchEqual(PhaseScheduled)),
				dex.ActionRequiresPermission(ManageBookedMeetingPermission),
			),
			LockAttributes: bookingLocks,
		}),
		dex.DefineRPC(flow.GetBookedMeeting, nil),
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the phase and booking Attributes and both request Channels.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{
		Attributes: []dex.AttributeDef{bookingPhaseAttribute, bookedMeetingAttribute},
		Channels:   []dex.ChannelDef{rescheduleRequests, attendanceCheckRequests},
	}
}

// MapToCreateMeetingInput schedules the booked slot with a waiting room, so guests wait for the host.
func MapToCreateMeetingInput(booking Input) zoom.CreateMeetingInput {
	isWaitingRoomOn := true
	return zoom.CreateMeetingInput{
		Topic: booking.Topic, StartTime: booking.StartTime, TimeZone: booking.TimeZone,
		DurationMinutes: booking.DurationMinutes, Agenda: booking.Agenda,
		Settings: &zoom.MeetingSettingsInput{WaitingRoom: &isWaitingRoomOn},
	}
}

// MapToFindUncertainMeetingInput reads one page of upcoming meetings, Zoom's largest.
func MapToFindUncertainMeetingInput(UncertainCreate) zoom.ListMeetingsInput {
	return zoom.ListMeetingsInput{Type: zoom.MeetingListUpcoming, PageSize: maximumListedMeetings}
}

// MapToUpdateMeetingInput moves the meeting to the requested slot and length.
func MapToUpdateMeetingInput(reschedule MeetingReschedule) zoom.UpdateMeetingInput {
	return zoom.UpdateMeetingInput{
		MeetingID: reschedule.MeetingID, StartTime: reschedule.Request.StartTime, TimeZone: reschedule.Request.TimeZone,
		DurationMinutes: int(reschedule.Request.DurationMinutes),
	}
}

// MapToGetMeetingInput reads back the meeting the reschedule patched.
func MapToGetMeetingInput(result zoom.UpdateMeetingResult) zoom.GetMeetingInput {
	return zoom.GetMeetingInput{MeetingID: result.Value.MeetingID}
}

// MapToListPastMeetingParticipantsInput lists one page of participants, Zoom's largest.
func MapToListPastMeetingParticipantsInput(check AttendanceCheck) zoom.ListPastMeetingParticipantsInput {
	return zoom.ListPastMeetingParticipantsInput{MeetingID: check.MeetingID, PageSize: maximumListedMeetings}
}

// RescheduleBookedMeeting queues a move to another future slot while the meeting is scheduled.
// A repeated request while the first is applied is ignored.
//
// dex:input field-name:startTime value-type:string source:user required:true description:"New start in RFC 3339 with an offset, such as 2026-10-08T09:00:00-07:00"
// dex:input field-name:timeZone value-type:string source:user required:true description:"IANA time zone Zoom displays the meeting in, such as America/Los_Angeles"
// dex:input field-name:durationMinutes value-type:int64 source:user required:true description:"Length of the moved meeting in minutes, from 1 to 1440"
func (*Flow) RescheduleBookedMeeting(ctx dex.Context, input RescheduleBookedMeetingInput) (*dex.RPCResult[dex.None], error) {
	record, err := bookedMeetingAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if record.Phase == PhaseRescheduling {
		return &dex.RPCResult[dex.None]{}, nil
	}
	if record.Phase != PhaseScheduled {
		return nil, errNotScheduled
	}
	if _, err := parseSlotStart(input.StartTime, input.TimeZone, time.Now()); err != nil {
		return nil, err
	}
	if input.DurationMinutes < 1 || input.DurationMinutes > 1440 {
		return nil, errors.New("durationMinutes must be between 1 and 1440")
	}
	record.Phase = PhaseRescheduling
	record.PendingReschedule = &input
	if err := bookingPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := bookedMeetingAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	if err := rescheduleRequests.Publish(ctx, input); err != nil {
		return nil, err
	}
	return &dex.RPCResult[dex.None]{}, nil
}

// CheckBookedMeetingAttendance lists participants now instead of waiting for the
// scheduled end, such as after a meeting that ended early.
func (*Flow) CheckBookedMeetingAttendance(ctx dex.Context, _ dex.None) (*dex.RPCResult[dex.None], error) {
	record, err := bookedMeetingAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if record.Phase == PhaseCheckingAttendance {
		return &dex.RPCResult[dex.None]{}, nil
	}
	if record.Phase != PhaseScheduled {
		return nil, errNotScheduled
	}
	record.Phase = PhaseCheckingAttendance
	if err := bookingPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := bookedMeetingAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	if err := attendanceCheckRequests.Publish(ctx, nil); err != nil {
		return nil, err
	}
	return &dex.RPCResult[dex.None]{}, nil
}

// GetBookedMeeting returns the current booking record.
func (*Flow) GetBookedMeeting(ctx dex.Context, _ dex.None) (*dex.RPCResult[BookedMeeting], error) {
	record, err := bookedMeetingAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[BookedMeeting]{Output: record}, nil
}

// GetDexSummary returns the booking phase and record for Dex Web lists.
//
// dex:field attribute-key:zoom-booked-meeting-phase value-type:string editable:false description:"Booking phase"
// dex:field attribute-key:zoom-booked-meeting value-type:json editable:false description:"Booking, Zoom meeting ID, and join URL"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	phase, err := optionalAttribute(ctx, bookingPhaseAttribute)
	if err != nil {
		return nil, err
	}
	record, err := optionalAttribute(ctx, bookedMeetingAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"zoom-booked-meeting-phase": phase,
		"zoom-booked-meeting":       record,
	}}, nil
}

// GetDexDisplay returns the booking phase and record for the Dex Web run view.
//
// dex:field attribute-key:zoom-booked-meeting-phase value-type:string editable:false description:"Booking phase" ui-slot:status
// dex:field attribute-key:zoom-booked-meeting value-type:json editable:false description:"Booking, Zoom meeting, reschedules, any reconciliation, and attendance"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	phase, err := optionalAttribute(ctx, bookingPhaseAttribute)
	if err != nil {
		return nil, err
	}
	record, err := optionalAttribute(ctx, bookedMeetingAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"zoom-booked-meeting-phase": phase,
		"zoom-booked-meeting":       record,
	}}, nil
}

// FindCreatedMeeting returns the listed meetings an unknown create may have made:
// same topic, start instant, and duration, created after the booking was recorded.
func FindCreatedMeeting(meetings []zoom.MeetingSummary, booking Input, recordedAt time.Time) ([]zoom.MeetingSummary, error) {
	start, err := time.Parse(time.RFC3339, booking.StartTime)
	if err != nil {
		return nil, fmt.Errorf("recorded booking start: %w", err)
	}
	var matches []zoom.MeetingSummary
	for _, meeting := range meetings {
		if meeting.Topic == booking.Topic && meeting.StartTime != nil && meeting.StartTime.Equal(start) &&
			meeting.DurationMinutes == booking.DurationMinutes && meeting.CreatedAt != nil &&
			!meeting.CreatedAt.Before(recordedAt.Add(-reconciliationClockSkew)) {
			matches = append(matches, meeting)
		}
	}
	return matches, nil
}

// CountGuestJoins counts joins by anyone other than the host. A guest who
// joined without signing in has no participant ID and always counts.
func CountGuestJoins(participants []zoom.MeetingParticipant, hostID string) int {
	guestJoins := 0
	for _, participant := range participants {
		if participant.ID == "" || participant.ID != hostID {
			guestJoins++
		}
	}
	return guestJoins
}

// dex:group group-id:booking group-label:"Booking"
// dex:explanation text:"Validate the booked slot and record it before calling Zoom."
type recordBooking struct {
	dex.StepDefaults
}

func (recordBooking) GetStepType() string { return recordBookingStepType }

func (recordBooking) GetStepOptions() *dex.StepOptions { return bookingLockedStepOptions() }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordBooking) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordBooking) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	booking, err := validateBooking(input, time.Now())
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	record := BookedMeeting{Booking: booking, Phase: PhaseScheduling, RecordedAt: time.Now().UTC()}
	if err := bookingPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := bookedMeetingAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](createBookedMeetingStepType), booking), nil
}

// dex:group group-id:booking group-label:"Booking"
// dex:explanation text:"Record the meeting ID and join URL Zoom confirmed, then wait for the meeting."
type recordScheduledMeeting struct {
	dex.StepDefaultsNoWaitFor[zoom.CreateMeetingResult]
	attendanceCheckDelay time.Duration
}

func (recordScheduledMeeting) GetStepType() string { return recordScheduledMeetingStepType }

func (recordScheduledMeeting) GetStepOptions() *dex.StepOptions { return bookingLockedStepOptions() }

func (step recordScheduledMeeting) Execute(ctx dex.Context, result zoom.CreateMeetingResult) (*dex.StepDecision, error) {
	record, err := bookedMeetingAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	meeting := result.Value
	record.adoptMeeting(meeting.ID, meeting.JoinURL, meeting.HostID, meeting.StartTime, meeting.TimeZone, meeting.DurationMinutes)
	record.Phase = PhaseScheduled
	if note := describeStoredSlotMismatch(record, record.Booking.StartTime, record.Booking.DurationMinutes); note != "" {
		record.Phase = PhaseNeedsReconciliation
		record.Note = note
	}
	if err := bookingPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := bookedMeetingAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	if record.Phase == PhaseNeedsReconciliation {
		return dex.GracefulComplete(record), nil
	}
	return dex.GoTo(awaitBookedMeeting{}, MeetingWait{MeetingID: record.MeetingID, AttendanceCheckAt: attendanceCheckTime(record, step.attendanceCheckDelay)}), nil
}

// dex:group group-id:booking group-label:"Booking"
// dex:explanation text:"Complete with Zoom's error code after a conclusive rejection; nothing was created."
type recordRejectedMeeting struct {
	dex.StepDefaultsNoWaitFor[zoom.CreateMeetingResult]
}

func (recordRejectedMeeting) GetStepType() string { return recordRejectedMeetingStepType }

func (recordRejectedMeeting) GetStepOptions() *dex.StepOptions { return bookingLockedStepOptions() }

func (recordRejectedMeeting) Execute(ctx dex.Context, result zoom.CreateMeetingResult) (*dex.StepDecision, error) {
	record, err := bookedMeetingAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase = PhaseRejected
	record.Refusal = refusalFrom(result.Failure, result.Receipt)
	if err := bookingPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := bookedMeetingAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(record), nil
}

// dex:group group-id:reconciliation group-label:"Reconciliation"
// dex:explanation text:"Record the unknown create and search Zoom instead of creating the meeting again."
type recordUncertainMeeting struct {
	dex.StepDefaultsNoWaitFor[zoom.CreateMeetingResult]
}

func (recordUncertainMeeting) GetStepType() string { return recordUncertainMeetingStepType }

func (recordUncertainMeeting) GetStepOptions() *dex.StepOptions { return bookingLockedStepOptions() }

func (recordUncertainMeeting) Execute(ctx dex.Context, result zoom.CreateMeetingResult) (*dex.StepDecision, error) {
	record, err := bookedMeetingAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	uncertain := UncertainCreate{CallID: string(result.Receipt.CallID), ObservedAt: result.Receipt.ObservedAt}
	if result.Failure != nil {
		uncertain.FailureKind = result.Failure.Kind
	}
	record.Phase = PhaseReconciling
	record.UncertainCreate = &uncertain
	if err := bookingPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := bookedMeetingAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[UncertainCreate](findUncertainMeetingStepType), uncertain), nil
}

// dex:group group-id:reconciliation group-label:"Reconciliation"
// dex:explanation text:"Adopt the one listed meeting the create made, or stop for an operator without creating again."
type evaluateUncertainMeeting struct {
	dex.StepDefaultsNoWaitFor[zoom.ListMeetingsResult]
	attendanceCheckDelay time.Duration
}

func (evaluateUncertainMeeting) GetStepType() string { return evaluateUncertainMeetingStepType }

func (evaluateUncertainMeeting) GetStepOptions() *dex.StepOptions { return bookingLockedStepOptions() }

func (step evaluateUncertainMeeting) Execute(ctx dex.Context, result zoom.ListMeetingsResult) (*dex.StepDecision, error) {
	record, err := bookedMeetingAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	matches, err := FindCreatedMeeting(result.Value.Meetings, record.Booking, record.RecordedAt)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	record.Phase = PhaseNeedsReconciliation
	switch {
	case len(matches) == 1:
		meeting := matches[0]
		record.adoptMeeting(meeting.ID, meeting.JoinURL, meeting.HostID, meeting.StartTime, meeting.TimeZone, meeting.DurationMinutes)
		record.Phase = PhaseScheduled
		record.Note = "adopted the meeting the unknown create made"
	case len(matches) > 1:
		record.Note = "Zoom lists more than one matching meeting; keep one and delete the others in Zoom"
	case result.Value.NextPageToken != "":
		record.Note = "Zoom lists more upcoming meetings than one page; search Zoom for the booking before creating it again"
	default:
		record.Note = "Zoom lists no matching meeting; it may still appear, so check Zoom before creating it again"
	}
	if err := bookingPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := bookedMeetingAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	if record.Phase == PhaseNeedsReconciliation {
		return dex.GracefulComplete(record), nil
	}
	return dex.GoTo(awaitBookedMeeting{}, MeetingWait{MeetingID: record.MeetingID, AttendanceCheckAt: attendanceCheckTime(record, step.attendanceCheckDelay)}), nil
}

// dex:group group-id:booking group-label:"Booking"
// dex:explanation text:"Wait durably for the scheduled end, a reschedule request, or an attendance check request."
type awaitBookedMeeting struct {
	dex.StepDefaults
}

func (awaitBookedMeeting) GetStepType() string { return awaitBookedMeetingStepType }

func (awaitBookedMeeting) GetStepOptions() *dex.StepOptions { return bookingLockedStepOptions() }

func (awaitBookedMeeting) WaitFor(_ dex.Context, wait MeetingWait) (*dex.Wait, error) {
	return dex.AnyOf(
		dex.Timer(max(time.Until(wait.AttendanceCheckAt), time.Second)),
		rescheduleRequests.ForOne(),
		attendanceCheckRequests.ForOne(),
	), nil
}

func (awaitBookedMeeting) Execute(ctx dex.Context, wait MeetingWait) (*dex.StepDecision, error) {
	reschedules, err := rescheduleRequests.GetConditionResults(ctx)
	if err != nil {
		return nil, err
	}
	if !ctx.HasTimerFired() && len(reschedules) > 0 {
		return dex.GoTo(sdkgo.StepRef[MeetingReschedule](moveBookedMeetingStepType),
			MeetingReschedule{MeetingID: wait.MeetingID, Request: reschedules[0]}), nil
	}
	record, err := bookedMeetingAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	// A reschedule requested after the attendance time has nothing left to move.
	record.Phase = PhaseCheckingAttendance
	record.PendingReschedule = nil
	if err := bookingPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := bookedMeetingAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[AttendanceCheck](listMeetingParticipantsStepType), AttendanceCheck{MeetingID: wait.MeetingID}), nil
}

// dex:group group-id:booking group-label:"Booking"
// dex:explanation text:"Confirm Zoom stores the requested slot, then wait for the moved meeting."
type recordRescheduledMeeting struct {
	dex.StepDefaultsNoWaitFor[zoom.GetMeetingResult]
	attendanceCheckDelay time.Duration
}

func (recordRescheduledMeeting) GetStepType() string { return recordRescheduledMeetingStepType }

func (recordRescheduledMeeting) GetStepOptions() *dex.StepOptions { return bookingLockedStepOptions() }

func (step recordRescheduledMeeting) Execute(ctx dex.Context, result zoom.GetMeetingResult) (*dex.StepDecision, error) {
	record, err := bookedMeetingAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if record.PendingReschedule == nil {
		return dex.ForceFail("a read-back arrived without a pending reschedule"), nil
	}
	request := *record.PendingReschedule
	requestedDuration := int(request.DurationMinutes)
	meeting := result.Value
	record.adoptMeeting(meeting.ID, meeting.JoinURL, meeting.HostID, meeting.StartTime, meeting.TimeZone, meeting.DurationMinutes)
	record.PendingReschedule = nil
	record.Reschedules++
	record.Phase = PhaseScheduled
	if note := describeStoredSlotMismatch(record, request.StartTime, requestedDuration); note != "" {
		record.Phase = PhaseNeedsReconciliation
		record.Note = note
	}
	if err := bookingPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := bookedMeetingAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	if record.Phase == PhaseNeedsReconciliation {
		return dex.GracefulComplete(record), nil
	}
	return dex.GoTo(awaitBookedMeeting{}, MeetingWait{MeetingID: record.MeetingID, AttendanceCheckAt: attendanceCheckTime(record, step.attendanceCheckDelay)}), nil
}

// dex:group group-id:booking group-label:"Booking"
// dex:explanation text:"Complete as cancelled when the meeting was deleted in Zoom before it ended."
type recordCancelledMeeting struct {
	dex.StepDefaultsNoWaitFor[zoom.UpdateMeetingResult]
}

func (recordCancelledMeeting) GetStepType() string { return recordCancelledMeetingStepType }

func (recordCancelledMeeting) GetStepOptions() *dex.StepOptions { return bookingLockedStepOptions() }

func (recordCancelledMeeting) Execute(ctx dex.Context, _ zoom.UpdateMeetingResult) (*dex.StepDecision, error) {
	record, err := bookedMeetingAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase = PhaseCancelled
	record.PendingReschedule = nil
	record.Note = "Zoom no longer has the meeting, so it was deleted before the reschedule"
	if err := bookingPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := bookedMeetingAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(record), nil
}

// dex:group group-id:attendance group-label:"Attendance"
// dex:explanation text:"Complete as attended, no-show, or unknown from Zoom's participant list."
type recordMeetingAttendance struct {
	dex.StepDefaultsNoWaitFor[zoom.ListPastMeetingParticipantsResult]
}

func (recordMeetingAttendance) GetStepType() string { return recordMeetingAttendanceStepType }

func (recordMeetingAttendance) GetStepOptions() *dex.StepOptions { return bookingLockedStepOptions() }

func (recordMeetingAttendance) Execute(ctx dex.Context, result zoom.ListPastMeetingParticipantsResult) (*dex.StepDecision, error) {
	record, err := bookedMeetingAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	switch result.Branch {
	case zoom.ListPastMeetingParticipantsBranchListed:
		record.GuestJoins = CountGuestJoins(result.Value.Participants, record.HostID)
		switch {
		case record.GuestJoins > 0:
			record.Phase = PhaseAttended
		case result.Value.NextPageToken != "":
			record.Phase = PhaseAttendanceUnknown
			record.Note = "the first participant page lists only the host"
		default:
			record.Phase = PhaseNoShow
		}
	case zoom.ListPastMeetingParticipantsBranchNotFound:
		record.Phase = PhaseNoShow
		record.Note = "Zoom has no ended instance of the meeting, so nobody joined"
	default:
		record.Phase = PhaseAttendanceUnknown
		record.Refusal = refusalFrom(result.Failure, result.Receipt)
	}
	if err := bookingPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := bookedMeetingAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(record), nil
}

// describeStoredSlotMismatch explains a stored slot that differs from the request by instant, or returns "".
func describeStoredSlotMismatch(record BookedMeeting, requestedStart string, requestedDuration int) string {
	start, err := time.Parse(time.RFC3339, requestedStart)
	if err != nil {
		return "the requested start is not RFC 3339"
	}
	if record.StartTime == nil || !record.StartTime.Equal(start) || record.DurationMinutes != requestedDuration {
		return fmt.Sprintf("Zoom stores the meeting at a different slot than the requested %s for %d minutes",
			start.UTC().Format(time.RFC3339), requestedDuration)
	}
	return ""
}

// attendanceCheckTime is the stored scheduled end plus the policy delay.
func attendanceCheckTime(record BookedMeeting, checkDelay time.Duration) time.Time {
	return record.StartTime.Add(time.Duration(record.DurationMinutes)*time.Minute + checkDelay)
}

func (record *BookedMeeting) adoptMeeting(meetingID int64, joinURL string, hostID string, startTime *time.Time, timeZone string, durationMinutes int) {
	record.MeetingID = meetingID
	record.HostID = hostID
	record.StartTime = startTime
	record.TimeZone = timeZone
	record.DurationMinutes = durationMinutes
	if joinURL != "" {
		record.JoinURL = joinURL
	}
}

func validateBooking(input Input, now time.Time) (Input, error) {
	input.BookingID = strings.TrimSpace(input.BookingID)
	if !bookingIDPattern.MatchString(input.BookingID) {
		return Input{}, errors.New("bookingId must be 1 to 64 letters, digits, dots, underscores, or hyphens")
	}
	if strings.TrimSpace(input.Topic) == "" || utf8.RuneCountInString(input.Topic) > 200 {
		return Input{}, errors.New("topic must be 1 to 200 characters")
	}
	if utf8.RuneCountInString(input.Agenda) > 2000 {
		return Input{}, errors.New("agenda must be at most 2000 characters")
	}
	if _, err := parseSlotStart(input.StartTime, input.TimeZone, now); err != nil {
		return Input{}, err
	}
	if err := validateDurationMinutes(input.DurationMinutes); err != nil {
		return Input{}, err
	}
	return input, nil
}

// parseSlotStart accepts only a future RFC 3339 start with an explicit offset and an IANA zone.
func parseSlotStart(startTime string, timeZone string, now time.Time) (time.Time, error) {
	start, err := time.Parse(time.RFC3339, startTime)
	if err != nil {
		return time.Time{}, fmt.Errorf("startTime %q must be RFC 3339 with an explicit offset, such as 2026-10-08T09:00:00-07:00", startTime)
	}
	if timeZone == "" || timeZone == "Local" {
		return time.Time{}, errors.New("timeZone must be an IANA time zone name such as America/Los_Angeles")
	}
	if _, err := time.LoadLocation(timeZone); err != nil {
		return time.Time{}, fmt.Errorf("timeZone %q is not a known IANA time zone name", timeZone)
	}
	if !start.After(now) {
		return time.Time{}, fmt.Errorf("startTime %s is not in the future", start.UTC().Format(time.RFC3339))
	}
	return start, nil
}

func validateDurationMinutes(durationMinutes int) error {
	if durationMinutes < 1 || durationMinutes > 1440 {
		return errors.New("durationMinutes must be between 1 and 1440")
	}
	return nil
}

func refusalFrom(failure *sdkgo.Failure, receipt sdkgo.Receipt) *ZoomRefusal {
	refusal := &ZoomRefusal{ZoomErrorCode: receipt.Metadata["zoomErrorCode"]}
	if failure != nil {
		refusal.FailureKind = failure.Kind
	}
	return refusal
}

func bookingLockedStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(bookingPhaseAttribute), dex.LockAttribute(bookedMeetingAttribute)}}
}

func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		var zero T
		return zero, nil
	}
	return value, err
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[RescheduleBookedMeetingInput, dex.None] = (*Flow)(nil).RescheduleBookedMeeting
var _ dex.RPC[dex.None, dex.None] = (*Flow)(nil).CheckBookedMeetingAttendance
var _ dex.RPC[dex.None, BookedMeeting] = (*Flow)(nil).GetBookedMeeting
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
