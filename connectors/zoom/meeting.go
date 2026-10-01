// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zoom

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	// Embedded zone data makes IANA validation identical on hosts without a zoneinfo database.
	_ "time/tzdata"
)

const (
	// zoomUTCLayout is Zoom's documented GMT form; an instant sent this way never depends on Zoom's zone table.
	zoomUTCLayout = "2006-01-02T15:04:05Z"

	maximumTopicCharacters  = 200
	maximumAgendaCharacters = 2000
	maximumDurationMinutes  = 1440
	maximumPageSize         = 300
	defaultPageSize         = 30
	maximumPageTokenBytes   = 1024
)

var errMeetingResponseInvalid = errors.New("Zoom returned an unusable meeting")

// AutoRecording is Zoom's automatic recording setting for a meeting.
type AutoRecording string

const (
	// AutoRecordingNone disables automatic recording.
	AutoRecordingNone AutoRecording = "none"
	// AutoRecordingLocal records automatically on the host's computer.
	AutoRecordingLocal AutoRecording = "local"
	// AutoRecordingCloud records automatically to Zoom cloud recording, which needs a paid plan.
	AutoRecordingCloud AutoRecording = "cloud"
)

// Meeting is the safe view of one Zoom meeting. It never contains the host's
// start URL, which logs anyone in as the host, or any meeting passcode.
type Meeting struct {
	// ID is Zoom's numeric meeting ID, also called the meeting number. Zero means
	// Zoom has not confirmed a meeting, as on the createMeeting uncertain branch.
	ID int64 `json:"id"`
	// UUID identifies the current meeting instance; Zoom issues a new one after each instance ends.
	UUID string `json:"uuid,omitempty"`
	// HostID is the Zoom user ID of the meeting host.
	HostID string `json:"hostId,omitempty"`
	// HostEmail is the host's email address.
	HostEmail string `json:"hostEmail,omitempty"`
	// Topic is the meeting topic.
	Topic string `json:"topic"`
	// Type is Zoom's meeting type: 1 instant, 2 scheduled, 3 recurring without a fixed time, 8 recurring with a fixed time.
	Type int `json:"type,omitempty"`
	// Status is Zoom's status value, such as waiting or started, passed through unchanged.
	Status string `json:"status,omitempty"`
	// StartTime is the scheduled start instant in UTC; nil for a meeting without a fixed time.
	StartTime *time.Time `json:"startTime,omitempty"`
	// DurationMinutes is the scheduled duration in minutes.
	DurationMinutes int `json:"durationMinutes,omitempty"`
	// TimeZone is the IANA time zone Zoom uses to display StartTime.
	TimeZone string `json:"timeZone,omitempty"`
	// Agenda is the meeting description.
	Agenda string `json:"agenda,omitempty"`
	// JoinURL is the participant join link. Share it only with invitees: it can embed the passcode.
	JoinURL string `json:"joinUrl,omitempty"`
	// CreatedAt is when Zoom created the meeting.
	CreatedAt *time.Time `json:"createdAt,omitempty"`
	// Settings holds the meeting settings this connector reads and writes.
	Settings MeetingSettings `json:"settings"`
}

// MeetingSettings is the subset of Zoom meeting settings this connector exposes.
type MeetingSettings struct {
	// HostVideo starts the meeting with the host's video on.
	HostVideo bool `json:"hostVideo"`
	// ParticipantVideo starts the meeting with participants' video on.
	ParticipantVideo bool `json:"participantVideo"`
	// JoinBeforeHost lets participants join before the host.
	JoinBeforeHost bool `json:"joinBeforeHost"`
	// MuteUponEntry mutes participants when they join.
	MuteUponEntry bool `json:"muteUponEntry"`
	// WaitingRoom holds participants in a waiting room; Zoom then disables JoinBeforeHost.
	WaitingRoom bool `json:"waitingRoom"`
	// AutoRecording is Zoom's automatic recording value, passed through unchanged.
	AutoRecording AutoRecording `json:"autoRecording,omitempty"`
}

// MeetingSettingsInput changes meeting settings. A nil field, or a blank
// AutoRecording, leaves Zoom's value, which defaults to the user's settings.
type MeetingSettingsInput struct {
	// HostVideo starts the meeting with the host's video on.
	HostVideo *bool `json:"hostVideo,omitempty"`
	// ParticipantVideo starts the meeting with participants' video on.
	ParticipantVideo *bool `json:"participantVideo,omitempty"`
	// JoinBeforeHost lets participants join before the host.
	JoinBeforeHost *bool `json:"joinBeforeHost,omitempty"`
	// MuteUponEntry mutes participants when they join.
	MuteUponEntry *bool `json:"muteUponEntry,omitempty"`
	// WaitingRoom holds participants in a waiting room; Zoom then disables JoinBeforeHost.
	WaitingRoom *bool `json:"waitingRoom,omitempty"`
	// AutoRecording is none, local, or cloud.
	AutoRecording AutoRecording `json:"autoRecording,omitempty"`
}

type zoomMeetingResource struct {
	ID        int64                        `json:"id"`
	UUID      string                       `json:"uuid"`
	HostID    string                       `json:"host_id"`
	HostEmail string                       `json:"host_email"`
	Topic     string                       `json:"topic"`
	Type      int                          `json:"type"`
	Status    string                       `json:"status"`
	StartTime string                       `json:"start_time"`
	Duration  int                          `json:"duration"`
	TimeZone  string                       `json:"timezone"`
	Agenda    string                       `json:"agenda"`
	JoinURL   string                       `json:"join_url"`
	CreatedAt string                       `json:"created_at"`
	Settings  *zoomMeetingSettingsResource `json:"settings"`
}

type zoomMeetingSettingsResource struct {
	HostVideo        *bool          `json:"host_video,omitempty"`
	ParticipantVideo *bool          `json:"participant_video,omitempty"`
	JoinBeforeHost   *bool          `json:"join_before_host,omitempty"`
	MuteUponEntry    *bool          `json:"mute_upon_entry,omitempty"`
	WaitingRoom      *bool          `json:"waiting_room,omitempty"`
	AutoRecording    *AutoRecording `json:"auto_recording,omitempty"`
}

// decodeMeeting validates one meeting object from getMeeting or createMeeting.
func decodeMeeting(contents []byte) (Meeting, error) {
	var resource zoomMeetingResource
	if err := decodeSingleJSONObject(contents, &resource); err != nil {
		return Meeting{}, errMeetingResponseInvalid
	}
	return resource.toMeeting()
}

func (resource zoomMeetingResource) toMeeting() (Meeting, error) {
	if resource.ID < 1 || resource.Duration < 0 {
		return Meeting{}, errMeetingResponseInvalid
	}
	startTime, err := parseOptionalZoomTime(resource.StartTime)
	if err != nil {
		return Meeting{}, err
	}
	createdAt, err := parseOptionalZoomTime(resource.CreatedAt)
	if err != nil {
		return Meeting{}, err
	}
	if resource.JoinURL != "" && !isHTTPSURL(resource.JoinURL) {
		return Meeting{}, errMeetingResponseInvalid
	}
	meeting := Meeting{
		ID: resource.ID, UUID: resource.UUID, HostID: resource.HostID, HostEmail: resource.HostEmail,
		Topic: resource.Topic, Type: resource.Type, Status: resource.Status, StartTime: startTime,
		DurationMinutes: resource.Duration, TimeZone: resource.TimeZone, Agenda: resource.Agenda,
		JoinURL: resource.JoinURL, CreatedAt: createdAt,
	}
	if settings := resource.Settings; settings != nil {
		meeting.Settings = MeetingSettings{
			HostVideo: isTrue(settings.HostVideo), ParticipantVideo: isTrue(settings.ParticipantVideo),
			JoinBeforeHost: isTrue(settings.JoinBeforeHost), MuteUponEntry: isTrue(settings.MuteUponEntry),
			WaitingRoom: isTrue(settings.WaitingRoom),
		}
		if settings.AutoRecording != nil {
			meeting.Settings.AutoRecording = *settings.AutoRecording
		}
	}
	return meeting, nil
}

// toZoomSettings returns Zoom's settings object, or nil when nothing is set.
func (settings *MeetingSettingsInput) toZoomSettings() *zoomMeetingSettingsResource {
	if settings == nil {
		return nil
	}
	resource := &zoomMeetingSettingsResource{
		HostVideo: settings.HostVideo, ParticipantVideo: settings.ParticipantVideo,
		JoinBeforeHost: settings.JoinBeforeHost, MuteUponEntry: settings.MuteUponEntry, WaitingRoom: settings.WaitingRoom,
	}
	if settings.AutoRecording != "" {
		autoRecording := settings.AutoRecording
		resource.AutoRecording = &autoRecording
	}
	if *resource == (zoomMeetingSettingsResource{}) {
		return nil
	}
	return resource
}

func (settings *MeetingSettingsInput) validate() error {
	if settings == nil {
		return nil
	}
	switch settings.AutoRecording {
	case "", AutoRecordingNone, AutoRecordingLocal, AutoRecordingCloud:
		return nil
	default:
		return fmt.Errorf("settings.autoRecording %q must be none, local, or cloud", settings.AutoRecording)
	}
}

// parseMeetingStart returns an explicit future start; Zoom silently replaces a past start_time.
func parseMeetingStart(startTime string, timeZone string, now time.Time) (time.Time, error) {
	instant, err := time.Parse(time.RFC3339Nano, startTime)
	if err != nil {
		return time.Time{}, fmt.Errorf("startTime %q must be RFC 3339 with an explicit offset, such as 2026-02-24T09:00:00-08:00 or 2026-02-24T17:00:00Z", startTime)
	}
	if instant.Nanosecond() != 0 {
		return time.Time{}, fmt.Errorf("startTime %q must be a whole second, because Zoom stores seconds", startTime)
	}
	if err := validateTimeZoneName(timeZone); err != nil {
		return time.Time{}, err
	}
	if !instant.After(now) {
		return time.Time{}, fmt.Errorf("startTime %s is not in the future, and Zoom would replace it with the current time",
			instant.UTC().Format(time.RFC3339))
	}
	return instant, nil
}

func validateTimeZoneName(timeZone string) error {
	if timeZone == "" {
		return errors.New("timeZone is required with startTime; use an IANA name such as America/Los_Angeles")
	}
	if timeZone != strings.TrimSpace(timeZone) || timeZone == "Local" {
		return fmt.Errorf("timeZone %q must be an IANA time zone name such as America/Los_Angeles", timeZone)
	}
	if _, err := time.LoadLocation(timeZone); err != nil {
		return fmt.Errorf("timeZone %q is not a known IANA time zone name", timeZone)
	}
	return nil
}

func validateTopic(topic string) error {
	if strings.TrimSpace(topic) == "" {
		return errors.New("topic is required")
	}
	if !utf8.ValidString(topic) || utf8.RuneCountInString(topic) > maximumTopicCharacters {
		return fmt.Errorf("topic must be valid UTF-8 of at most %d characters", maximumTopicCharacters)
	}
	return nil
}

func validateAgenda(agenda string) error {
	if !utf8.ValidString(agenda) || utf8.RuneCountInString(agenda) > maximumAgendaCharacters {
		return fmt.Errorf("agenda must be valid UTF-8 of at most %d characters", maximumAgendaCharacters)
	}
	return nil
}

func validateDurationMinutes(durationMinutes int) error {
	if durationMinutes < 1 || durationMinutes > maximumDurationMinutes {
		return fmt.Errorf("durationMinutes must be between 1 and %d", maximumDurationMinutes)
	}
	return nil
}

func validateMeetingID(meetingID int64) error {
	if meetingID < 1 {
		return errors.New("meetingId must be Zoom's positive numeric meeting ID")
	}
	return nil
}

// resolvePageSize returns Zoom's page_size value; zero uses Zoom's documented default.
func resolvePageSize(pageSize int) (int, error) {
	if pageSize == 0 {
		return defaultPageSize, nil
	}
	if pageSize < 1 || pageSize > maximumPageSize {
		return 0, fmt.Errorf("pageSize must be between 1 and %d", maximumPageSize)
	}
	return pageSize, nil
}

// validatePageToken accepts a printable ASCII token, which Zoom expires after 15 minutes.
func validatePageToken(pageToken string) error {
	if len(pageToken) > maximumPageTokenBytes || !isPrintableASCIIWithoutSpaces(pageToken) {
		return fmt.Errorf("pageToken must be printable ASCII without spaces and at most %d bytes", maximumPageTokenBytes)
	}
	return nil
}

func parseOptionalZoomTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	instant, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, errMeetingResponseInvalid
	}
	instant = instant.UTC()
	return &instant, nil
}

// decodeSingleJSONObject decodes exactly one JSON value and rejects trailing data.
func decodeSingleJSONObject(contents []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("unexpected trailing JSON")
	}
	return nil
}

func isHTTPSURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() != "" && parsed.User == nil
}

func isPrintableASCIIWithoutSpaces(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] <= ' ' || value[index] > '~' {
			return false
		}
	}
	return true
}

func isTrue(value *bool) bool { return value != nil && *value }
