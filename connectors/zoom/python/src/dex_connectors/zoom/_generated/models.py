# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

from dex_connectors.sdk import format_rfc3339_nano

from .timestamps import canonical_go_timestamp

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListMeetingsInput selects one page of the authorized user's meetings.
@dataclass(frozen=True, slots=True)
class ListMeetingsInput:
    # Type selects the meetings to list; blank lists upcoming meetings.
    type: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # PageSize is the number of meetings per page, from 1 to 300; zero uses Zoom's default of 30.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # PageToken is the NextPageToken of the previous page; Zoom expires it after 15 minutes.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken', 'dex_json_omit_default': True})


setattr(ListMeetingsInput, '__module__', 'dex_connectors.zoom.models')


# MeetingSummary is one listed meeting. Zoom truncates a listed agenda to 250
# characters; use getMeeting for the full agenda and the settings.
@dataclass(frozen=True, slots=True)
class MeetingSummary:
    # ID is Zoom's numeric meeting ID.
    id: int = 0
    # UUID identifies the current meeting instance.
    uuid: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # HostID is the Zoom user ID of the meeting host.
    host_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'hostId', 'dex_json_omit_default': True})
    # Topic is the meeting topic.
    topic: str = ''
    # Type is Zoom's meeting type: 2 scheduled, 3 recurring without a fixed time, 8 recurring with a fixed time.
    type: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # StartTime is the scheduled start instant in UTC; nil for a meeting without a fixed time.
    start_time: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'startTime', 'dex_json_omit_default': True})
    # DurationMinutes is the scheduled duration in minutes.
    duration_minutes: int = _dataclass_field(default=0, metadata={'dex_json_name': 'durationMinutes', 'dex_json_omit_default': True})
    # TimeZone is the IANA time zone Zoom uses to display StartTime.
    time_zone: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeZone', 'dex_json_omit_default': True})
    # Agenda is the meeting description, truncated by Zoom to 250 characters.
    agenda: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # JoinURL is the participant join link. Share it only with invitees: it can embed the passcode.
    join_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'joinUrl', 'dex_json_omit_default': True})
    # CreatedAt is when Zoom created the meeting.
    created_at: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if self.start_time is not None:
            timestamp = self.start_time
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime) else canonical_go_timestamp(timestamp, 'start_time'))
            object.__setattr__(self, 'start_time', canonical)
        if self.created_at is not None:
            timestamp = self.created_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime) else canonical_go_timestamp(timestamp, 'created_at'))
            object.__setattr__(self, 'created_at', canonical)


setattr(MeetingSummary, '__module__', 'dex_connectors.zoom.models')


# MeetingPage is one page of meetings in Zoom's order.
@dataclass(frozen=True, slots=True)
class MeetingPage:
    # Meetings lists the page's meetings.
    meetings: tuple[MeetingSummary, ...] = ()
    # NextPageToken requests the next page; blank means this is the last page.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken', 'dex_json_omit_default': True})
    # TotalRecords is Zoom's count of meetings across every page.
    total_records: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalRecords'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'meetings', _replace_nil_slices(self.meetings, 1))


MeetingPage.__annotations__['meetings'] = tuple[MeetingSummary, ...] | None
MeetingPage.__dataclass_fields__['meetings'].type = tuple[MeetingSummary, ...] | None
setattr(MeetingPage, '__module__', 'dex_connectors.zoom.models')


# GetMeetingInput identifies one meeting.
@dataclass(frozen=True, slots=True)
class GetMeetingInput:
    # MeetingID is Zoom's numeric meeting ID, such as the ID createMeeting returned.
    meeting_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'meetingId'})


setattr(GetMeetingInput, '__module__', 'dex_connectors.zoom.models')


# MeetingSettings is the subset of Zoom meeting settings this connector exposes.
@dataclass(frozen=True, slots=True)
class MeetingSettings:
    # HostVideo starts the meeting with the host's video on.
    host_video: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hostVideo'})
    # ParticipantVideo starts the meeting with participants' video on.
    participant_video: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'participantVideo'})
    # JoinBeforeHost lets participants join before the host.
    join_before_host: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'joinBeforeHost'})
    # MuteUponEntry mutes participants when they join.
    mute_upon_entry: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'muteUponEntry'})
    # WaitingRoom holds participants in a waiting room; Zoom then disables JoinBeforeHost.
    waiting_room: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'waitingRoom'})
    # AutoRecording is Zoom's automatic recording value, passed through unchanged.
    auto_recording: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'autoRecording', 'dex_json_omit_default': True})


setattr(MeetingSettings, '__module__', 'dex_connectors.zoom.models')


# Meeting is the safe view of one Zoom meeting. It never contains the host's
# start URL, which logs anyone in as the host, or any meeting passcode.
@dataclass(frozen=True, slots=True)
class Meeting:
    # ID is Zoom's numeric meeting ID, also called the meeting number. Zero means
    # Zoom has not confirmed a meeting, as on the createMeeting uncertain branch.
    id: int = 0
    # UUID identifies the current meeting instance; Zoom issues a new one after each instance ends.
    uuid: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # HostID is the Zoom user ID of the meeting host.
    host_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'hostId', 'dex_json_omit_default': True})
    # HostEmail is the host's email address.
    host_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'hostEmail', 'dex_json_omit_default': True})
    # Topic is the meeting topic.
    topic: str = ''
    # Type is Zoom's meeting type: 1 instant, 2 scheduled, 3 recurring without a fixed time, 8 recurring with a fixed time.
    type: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Status is Zoom's status value, such as waiting or started, passed through unchanged.
    status: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # StartTime is the scheduled start instant in UTC; nil for a meeting without a fixed time.
    start_time: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'startTime', 'dex_json_omit_default': True})
    # DurationMinutes is the scheduled duration in minutes.
    duration_minutes: int = _dataclass_field(default=0, metadata={'dex_json_name': 'durationMinutes', 'dex_json_omit_default': True})
    # TimeZone is the IANA time zone Zoom uses to display StartTime.
    time_zone: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeZone', 'dex_json_omit_default': True})
    # Agenda is the meeting description.
    agenda: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # JoinURL is the participant join link. Share it only with invitees: it can embed the passcode.
    join_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'joinUrl', 'dex_json_omit_default': True})
    # CreatedAt is when Zoom created the meeting.
    created_at: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True})
    # Settings holds the meeting settings this connector reads and writes.
    settings: MeetingSettings = _dataclass_field(default_factory=MeetingSettings)

    def __post_init__(self) -> None:
        if self.start_time is not None:
            timestamp = self.start_time
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime) else canonical_go_timestamp(timestamp, 'start_time'))
            object.__setattr__(self, 'start_time', canonical)
        if self.created_at is not None:
            timestamp = self.created_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime) else canonical_go_timestamp(timestamp, 'created_at'))
            object.__setattr__(self, 'created_at', canonical)


setattr(Meeting, '__module__', 'dex_connectors.zoom.models')


# MeetingSettingsInput changes meeting settings. A nil field, or a blank
# AutoRecording, leaves Zoom's value, which defaults to the user's settings.
@dataclass(frozen=True, slots=True)
class MeetingSettingsInput:
    # HostVideo starts the meeting with the host's video on.
    host_video: bool | None = _dataclass_field(default=None, metadata={'dex_json_name': 'hostVideo', 'dex_json_omit_default': True})
    # ParticipantVideo starts the meeting with participants' video on.
    participant_video: bool | None = _dataclass_field(default=None, metadata={'dex_json_name': 'participantVideo', 'dex_json_omit_default': True})
    # JoinBeforeHost lets participants join before the host.
    join_before_host: bool | None = _dataclass_field(default=None, metadata={'dex_json_name': 'joinBeforeHost', 'dex_json_omit_default': True})
    # MuteUponEntry mutes participants when they join.
    mute_upon_entry: bool | None = _dataclass_field(default=None, metadata={'dex_json_name': 'muteUponEntry', 'dex_json_omit_default': True})
    # WaitingRoom holds participants in a waiting room; Zoom then disables JoinBeforeHost.
    waiting_room: bool | None = _dataclass_field(default=None, metadata={'dex_json_name': 'waitingRoom', 'dex_json_omit_default': True})
    # AutoRecording is none, local, or cloud.
    auto_recording: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'autoRecording', 'dex_json_omit_default': True})


setattr(MeetingSettingsInput, '__module__', 'dex_connectors.zoom.models')


# CreateMeetingInput describes one scheduled meeting for the authorized user.
@dataclass(frozen=True, slots=True)
class CreateMeetingInput:
    # Topic is the meeting topic, 1 to 200 characters.
    topic: str = ''
    # StartTime is the start instant in RFC 3339 with an explicit Z or ±hh:mm
    # offset, such as 2026-02-24T09:00:00-08:00. It must be in the future and a
    # whole second; a value without an offset is rejected rather than guessed.
    start_time: str = _dataclass_field(default='', metadata={'dex_json_name': 'startTime'})
    # TimeZone is the IANA time zone Zoom uses to display the meeting, such as
    # America/Los_Angeles. It never moves the instant StartTime names.
    time_zone: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeZone'})
    # DurationMinutes is the scheduled duration, from 1 to 1440 minutes.
    duration_minutes: int = _dataclass_field(default=0, metadata={'dex_json_name': 'durationMinutes'})
    # Agenda is the optional meeting description, at most 2000 characters.
    agenda: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Settings optionally overrides the user's default meeting settings.
    settings: MeetingSettingsInput | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})


setattr(CreateMeetingInput, '__module__', 'dex_connectors.zoom.models')


# UpdateMeetingInput patches one meeting. Every field other than MeetingID is
# optional, at least one must be set, and each set field replaces Zoom's value,
# so repeating the same patch leaves the same meeting.
@dataclass(frozen=True, slots=True)
class UpdateMeetingInput:
    # MeetingID is Zoom's numeric meeting ID.
    meeting_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'meetingId'})
    # Topic replaces the topic, 1 to 200 characters; blank leaves it.
    topic: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Agenda replaces the agenda, at most 2000 characters; nil leaves it and an empty string clears it.
    agenda: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # StartTime moves the meeting to a future RFC 3339 instant with an explicit
    # offset; blank leaves it. Zoom silently ignores a past start, so the connector rejects one.
    start_time: str = _dataclass_field(default='', metadata={'dex_json_name': 'startTime', 'dex_json_omit_default': True})
    # TimeZone is the IANA time zone Zoom uses to display the meeting. It is
    # required with StartTime and must be blank without it.
    time_zone: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeZone', 'dex_json_omit_default': True})
    # DurationMinutes replaces the duration, from 1 to 1440 minutes; zero leaves it.
    duration_minutes: int = _dataclass_field(default=0, metadata={'dex_json_name': 'durationMinutes', 'dex_json_omit_default': True})
    # Settings changes the listed settings and leaves the others.
    settings: MeetingSettingsInput | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})


setattr(UpdateMeetingInput, '__module__', 'dex_connectors.zoom.models')


# UpdatedMeeting identifies the meeting Zoom patched. Zoom returns no meeting
# body, so read it with getMeeting to observe the stored values.
@dataclass(frozen=True, slots=True)
class UpdatedMeeting:
    # MeetingID is Zoom's numeric meeting ID.
    meeting_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'meetingId'})


setattr(UpdatedMeeting, '__module__', 'dex_connectors.zoom.models')


# ListPastMeetingParticipantsInput selects one page of a past meeting's participants.
@dataclass(frozen=True, slots=True)
class ListPastMeetingParticipantsInput:
    # MeetingID is Zoom's numeric meeting ID; Zoom answers for its latest ended instance.
    meeting_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'meetingId'})
    # PageSize is the number of participants per page, from 1 to 300; zero uses Zoom's default of 30.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # PageToken is the NextPageToken of the previous page; Zoom expires it after 15 minutes.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken', 'dex_json_omit_default': True})


setattr(ListPastMeetingParticipantsInput, '__module__', 'dex_connectors.zoom.models')


# MeetingParticipant is one join of a past meeting.
@dataclass(frozen=True, slots=True)
class MeetingParticipant:
    # ID is the participant's Zoom user ID when they joined signed in; blank or a
    # meeting-specific value otherwise.
    id: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Name is the participant's display name.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Email is the participant's email address; Zoom leaves it blank for most people outside the host's account.
    email: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # JoinTime is when this join started.
    join_time: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'joinTime', 'dex_json_omit_default': True})
    # LeaveTime is when this join ended.
    leave_time: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'leaveTime', 'dex_json_omit_default': True})
    # DurationSeconds is the time between JoinTime and LeaveTime in seconds.
    duration_seconds: int = _dataclass_field(default=0, metadata={'dex_json_name': 'durationSeconds'})
    # Status is Zoom's status value, such as in_meeting or in_waiting_room, passed through unchanged.
    status: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # IsInternalUser reports whether the participant belongs to the host's Zoom account.
    is_internal_user: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isInternalUser'})

    def __post_init__(self) -> None:
        if self.join_time is not None:
            timestamp = self.join_time
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime) else canonical_go_timestamp(timestamp, 'join_time'))
            object.__setattr__(self, 'join_time', canonical)
        if self.leave_time is not None:
            timestamp = self.leave_time
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime) else canonical_go_timestamp(timestamp, 'leave_time'))
            object.__setattr__(self, 'leave_time', canonical)


setattr(MeetingParticipant, '__module__', 'dex_connectors.zoom.models')


# ParticipantPage is one page of a past meeting's participants. Zoom lists a
# person once per join, so a participant who rejoined appears more than once.
@dataclass(frozen=True, slots=True)
class ParticipantPage:
    # MeetingID is the meeting whose latest ended instance was listed.
    meeting_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'meetingId'})
    # Participants lists the page's participants.
    participants: tuple[MeetingParticipant, ...] = ()
    # NextPageToken requests the next page; blank means this is the last page.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken', 'dex_json_omit_default': True})
    # TotalRecords is Zoom's count of participants across every page.
    total_records: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalRecords'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'participants', _replace_nil_slices(self.participants, 1))


ParticipantPage.__annotations__['participants'] = tuple[MeetingParticipant, ...] | None
ParticipantPage.__dataclass_fields__['participants'].type = tuple[MeetingParticipant, ...] | None
setattr(ParticipantPage, '__module__', 'dex_connectors.zoom.models')
