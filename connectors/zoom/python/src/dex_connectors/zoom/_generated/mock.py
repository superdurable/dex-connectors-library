# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import CreateMeetingInput, GetMeetingInput, ListMeetingsInput, ListPastMeetingParticipantsInput, Meeting, MeetingPage, MeetingParticipant, MeetingSettings, MeetingSettingsInput, MeetingSummary, ParticipantPage, UpdateMeetingInput, UpdatedMeeting
from .definitions import CONNECTOR_ID, CREATED, CREATE_MEETING_DEFINITION, DEFECT, FOUND, GET_MEETING_DEFINITION, INVALID_RESPONSE, LISTED, LIST_MEETINGS_DEFINITION, LIST_PAST_MEETING_PARTICIPANTS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, UNCERTAIN, UPDATED, UPDATE_MEETING_DEFINITION

def list_meetings_listed(value: MeetingPage, failure: Failure | None = None) -> Case[MeetingPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_meetings_listed, '__module__', "dex_connectors.zoom.zoommock")

def list_meetings_provider_rejected(value: MeetingPage, failure: Failure | None = None) -> Case[MeetingPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_meetings_provider_rejected, '__module__', "dex_connectors.zoom.zoommock")

def list_meetings_invalid_response(value: MeetingPage, failure: Failure | None = None) -> Case[MeetingPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_meetings_invalid_response, '__module__', "dex_connectors.zoom.zoommock")

def list_meetings_defect(value: MeetingPage, failure: Failure | None = None) -> Case[MeetingPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_meetings_defect, '__module__', "dex_connectors.zoom.zoommock")

def list_meetings_retry(failure: Failure, after_seconds: float = 0) -> Case[MeetingPage]:
    return Case.retry(failure, after_seconds)

setattr(list_meetings_retry, '__module__', "dex_connectors.zoom.zoommock")

def list_meetings_listed_default_answer() -> Case[MeetingPage]:
    return Case.paged(LISTED, (MeetingPage(meetings=(MeetingSummary(id=92674392836, topic='Consultation', type=2, start_time='2036-02-24T17:00:00Z', duration_minutes=30, time_zone='America/Los_Angeles', created_at='2036-02-23T17:00:00Z'), MeetingSummary(id=92674392840, topic='Team review', type=2, start_time='2036-02-25T17:00:00Z', duration_minutes=45, time_zone='America/Los_Angeles', created_at='2036-02-24T17:00:00Z')), next_page_token='zoom-page-2', total_records=3), MeetingPage(meetings=(MeetingSummary(id=92674392840, topic='Team review', type=2, start_time='2036-02-25T17:00:00Z', duration_minutes=45, time_zone='America/Los_Angeles', created_at='2036-02-24T17:00:00Z'), MeetingSummary(id=92674392841, topic='Planning', type=2, start_time='2036-02-26T17:00:00Z', duration_minutes=60, time_zone='America/Los_Angeles', created_at='2036-02-25T17:00:00Z')), next_page_token='zoom-page-3', total_records=3), MeetingPage(meetings=(), next_page_token='', total_records=3)))

setattr(list_meetings_listed_default_answer, '__module__', "dex_connectors.zoom.zoommock")

def list_meetings_provider_rejected_failure() -> Case[MeetingPage]:
    return Case.branch_result(PROVIDER_REJECTED, MeetingPage(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "listMeetings", "Zoom denied access to the meeting list."))

setattr(list_meetings_provider_rejected_failure, '__module__', "dex_connectors.zoom.zoommock")

def list_meetings_daily_limit_failure() -> Case[MeetingPage]:
    return Case.branch_result(PROVIDER_REJECTED, MeetingPage(), Failure(FailureKind.RATE_LIMIT, CONNECTOR_ID, "listMeetings", "Zoom daily API rate limit reached with HTTP 429 (code 429)"))

setattr(list_meetings_daily_limit_failure, '__module__', "dex_connectors.zoom.zoommock")

def list_meetings_invalid_response_failure() -> Case[MeetingPage]:
    return Case.branch_result(INVALID_RESPONSE, MeetingPage(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "listMeetings", "Zoom returned an unusable meeting page."))

setattr(list_meetings_invalid_response_failure, '__module__', "dex_connectors.zoom.zoommock")

def get_meeting_found(value: Meeting, failure: Failure | None = None) -> Case[Meeting]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_meeting_found, '__module__', "dex_connectors.zoom.zoommock")

def get_meeting_not_found(value: Meeting, failure: Failure | None = None) -> Case[Meeting]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_meeting_not_found, '__module__', "dex_connectors.zoom.zoommock")

def get_meeting_provider_rejected(value: Meeting, failure: Failure | None = None) -> Case[Meeting]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_meeting_provider_rejected, '__module__', "dex_connectors.zoom.zoommock")

def get_meeting_invalid_response(value: Meeting, failure: Failure | None = None) -> Case[Meeting]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_meeting_invalid_response, '__module__', "dex_connectors.zoom.zoommock")

def get_meeting_defect(value: Meeting, failure: Failure | None = None) -> Case[Meeting]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_meeting_defect, '__module__', "dex_connectors.zoom.zoommock")

def get_meeting_retry(failure: Failure, after_seconds: float = 0) -> Case[Meeting]:
    return Case.retry(failure, after_seconds)

setattr(get_meeting_retry, '__module__', "dex_connectors.zoom.zoommock")

def get_meeting_found_default_answer() -> Case[Meeting]:
    return Case.branch_result(FOUND, Meeting(id=92674392836, host_id='zoom-host-1', topic='Consultation', type=2, start_time='2036-02-24T17:00:00Z', duration_minutes=30, time_zone='America/Los_Angeles', join_url='https://us05web.zoom.us/j/92674392836?pwd=mock', created_at='2036-02-23T17:00:00Z', settings=MeetingSettings(waiting_room=True)))

setattr(get_meeting_found_default_answer, '__module__', "dex_connectors.zoom.zoommock")

def get_meeting_not_found_failure() -> Case[Meeting]:
    return Case.branch_result(NOT_FOUND, Meeting(settings=MeetingSettings()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getMeeting", "The meeting does not exist or was deleted."))

setattr(get_meeting_not_found_failure, '__module__', "dex_connectors.zoom.zoommock")

def get_meeting_provider_rejected_failure() -> Case[Meeting]:
    return Case.branch_result(PROVIDER_REJECTED, Meeting(settings=MeetingSettings()), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getMeeting", "Zoom denied access to the meeting."))

setattr(get_meeting_provider_rejected_failure, '__module__', "dex_connectors.zoom.zoommock")

def get_meeting_daily_limit_failure() -> Case[Meeting]:
    return Case.branch_result(PROVIDER_REJECTED, Meeting(settings=MeetingSettings()), Failure(FailureKind.RATE_LIMIT, CONNECTOR_ID, "getMeeting", "Zoom daily API rate limit reached with HTTP 429 (code 429)"))

setattr(get_meeting_daily_limit_failure, '__module__', "dex_connectors.zoom.zoommock")

def get_meeting_invalid_response_failure() -> Case[Meeting]:
    return Case.branch_result(INVALID_RESPONSE, Meeting(settings=MeetingSettings()), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "getMeeting", "Zoom returned an unusable meeting."))

setattr(get_meeting_invalid_response_failure, '__module__', "dex_connectors.zoom.zoommock")

def create_meeting_created(value: Meeting, failure: Failure | None = None) -> Case[Meeting]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_meeting_created, '__module__', "dex_connectors.zoom.zoommock")

def create_meeting_provider_rejected(value: Meeting, failure: Failure | None = None) -> Case[Meeting]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_meeting_provider_rejected, '__module__', "dex_connectors.zoom.zoommock")

def create_meeting_uncertain(value: Meeting, failure: Failure) -> Case[Meeting]:
    return Case.uncertain(value, failure)

setattr(create_meeting_uncertain, '__module__', "dex_connectors.zoom.zoommock")

def create_meeting_defect(value: Meeting, failure: Failure | None = None) -> Case[Meeting]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_meeting_defect, '__module__', "dex_connectors.zoom.zoommock")

def create_meeting_retry(failure: Failure, after_seconds: float = 0) -> Case[Meeting]:
    return Case.retry(failure, after_seconds)

setattr(create_meeting_retry, '__module__', "dex_connectors.zoom.zoommock")

def create_meeting_created_default_answer() -> Case[Meeting]:
    return Case.branch_result(CREATED, Meeting(id=92674392836, host_id='zoom-host-1', topic='Consultation', type=2, start_time='2036-02-24T17:00:00Z', duration_minutes=30, time_zone='America/Los_Angeles', join_url='https://us05web.zoom.us/j/92674392836?pwd=mock', created_at='2036-02-23T17:00:00Z', settings=MeetingSettings(waiting_room=True)))

setattr(create_meeting_created_default_answer, '__module__', "dex_connectors.zoom.zoommock")

def create_meeting_provider_rejected_failure() -> Case[Meeting]:
    return Case.branch_result(PROVIDER_REJECTED, Meeting(settings=MeetingSettings()), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "createMeeting", "Zoom conclusively rejected the meeting because the user cannot host; nothing was created."))

setattr(create_meeting_provider_rejected_failure, '__module__', "dex_connectors.zoom.zoommock")

def create_meeting_daily_limit_failure() -> Case[Meeting]:
    return Case.branch_result(PROVIDER_REJECTED, Meeting(settings=MeetingSettings()), Failure(FailureKind.RATE_LIMIT, CONNECTOR_ID, "createMeeting", "Zoom daily API rate limit reached with HTTP 429 (code 429)"))

setattr(create_meeting_daily_limit_failure, '__module__', "dex_connectors.zoom.zoommock")

def update_meeting_updated(value: UpdatedMeeting, failure: Failure | None = None) -> Case[UpdatedMeeting]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_meeting_updated, '__module__', "dex_connectors.zoom.zoommock")

def update_meeting_not_found(value: UpdatedMeeting, failure: Failure | None = None) -> Case[UpdatedMeeting]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_meeting_not_found, '__module__', "dex_connectors.zoom.zoommock")

def update_meeting_provider_rejected(value: UpdatedMeeting, failure: Failure | None = None) -> Case[UpdatedMeeting]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_meeting_provider_rejected, '__module__', "dex_connectors.zoom.zoommock")

def update_meeting_defect(value: UpdatedMeeting, failure: Failure | None = None) -> Case[UpdatedMeeting]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_meeting_defect, '__module__', "dex_connectors.zoom.zoommock")

def update_meeting_retry(failure: Failure, after_seconds: float = 0) -> Case[UpdatedMeeting]:
    return Case.retry(failure, after_seconds)

setattr(update_meeting_retry, '__module__', "dex_connectors.zoom.zoommock")

def update_meeting_updated_default_answer() -> Case[UpdatedMeeting]:
    return Case.branch_result(UPDATED, UpdatedMeeting(meeting_id=92674392836))

setattr(update_meeting_updated_default_answer, '__module__', "dex_connectors.zoom.zoommock")

def update_meeting_not_found_failure() -> Case[UpdatedMeeting]:
    return Case.branch_result(NOT_FOUND, UpdatedMeeting(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updateMeeting", "The meeting does not exist or was deleted."))

setattr(update_meeting_not_found_failure, '__module__', "dex_connectors.zoom.zoommock")

def update_meeting_provider_rejected_failure() -> Case[UpdatedMeeting]:
    return Case.branch_result(PROVIDER_REJECTED, UpdatedMeeting(), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "updateMeeting", "Zoom conclusively rejected the patch because a field value is invalid."))

setattr(update_meeting_provider_rejected_failure, '__module__', "dex_connectors.zoom.zoommock")

def update_meeting_daily_limit_failure() -> Case[UpdatedMeeting]:
    return Case.branch_result(PROVIDER_REJECTED, UpdatedMeeting(), Failure(FailureKind.RATE_LIMIT, CONNECTOR_ID, "updateMeeting", "Zoom daily API rate limit reached with HTTP 429 (code 429)"))

setattr(update_meeting_daily_limit_failure, '__module__', "dex_connectors.zoom.zoommock")

def list_past_meeting_participants_listed(value: ParticipantPage, failure: Failure | None = None) -> Case[ParticipantPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_past_meeting_participants_listed, '__module__', "dex_connectors.zoom.zoommock")

def list_past_meeting_participants_not_found(value: ParticipantPage, failure: Failure | None = None) -> Case[ParticipantPage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_past_meeting_participants_not_found, '__module__', "dex_connectors.zoom.zoommock")

def list_past_meeting_participants_provider_rejected(value: ParticipantPage, failure: Failure | None = None) -> Case[ParticipantPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_past_meeting_participants_provider_rejected, '__module__', "dex_connectors.zoom.zoommock")

def list_past_meeting_participants_invalid_response(value: ParticipantPage, failure: Failure | None = None) -> Case[ParticipantPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_past_meeting_participants_invalid_response, '__module__', "dex_connectors.zoom.zoommock")

def list_past_meeting_participants_defect(value: ParticipantPage, failure: Failure | None = None) -> Case[ParticipantPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_past_meeting_participants_defect, '__module__', "dex_connectors.zoom.zoommock")

def list_past_meeting_participants_retry(failure: Failure, after_seconds: float = 0) -> Case[ParticipantPage]:
    return Case.retry(failure, after_seconds)

setattr(list_past_meeting_participants_retry, '__module__', "dex_connectors.zoom.zoommock")

def list_past_meeting_participants_listed_default_answer() -> Case[ParticipantPage]:
    return Case.paged(LISTED, (ParticipantPage(meeting_id=92674392836, participants=(MeetingParticipant(id='zoom-host-1', name='Host', duration_seconds=1800, is_internal_user=True), MeetingParticipant(id='guest-1', name='Guest', duration_seconds=1680)), next_page_token='zoom-attendance-2', total_records=3), ParticipantPage(meeting_id=92674392836, participants=(MeetingParticipant(id='guest-1', name='Guest', duration_seconds=1680), MeetingParticipant(id='guest-2', name='Second guest', duration_seconds=1200)), next_page_token='zoom-attendance-3', total_records=3), ParticipantPage(meeting_id=92674392836, participants=(), next_page_token='', total_records=3)))

setattr(list_past_meeting_participants_listed_default_answer, '__module__', "dex_connectors.zoom.zoommock")

def list_past_meeting_participants_not_found_failure() -> Case[ParticipantPage]:
    return Case.branch_result(NOT_FOUND, ParticipantPage(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "listPastMeetingParticipants", "Zoom has no ended instance of the meeting."))

setattr(list_past_meeting_participants_not_found_failure, '__module__', "dex_connectors.zoom.zoommock")

def list_past_meeting_participants_provider_rejected_failure() -> Case[ParticipantPage]:
    return Case.branch_result(PROVIDER_REJECTED, ParticipantPage(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "listPastMeetingParticipants", "Zoom denied access to the participant list."))

setattr(list_past_meeting_participants_provider_rejected_failure, '__module__', "dex_connectors.zoom.zoommock")

def list_past_meeting_participants_daily_limit_failure() -> Case[ParticipantPage]:
    return Case.branch_result(PROVIDER_REJECTED, ParticipantPage(), Failure(FailureKind.RATE_LIMIT, CONNECTOR_ID, "listPastMeetingParticipants", "Zoom daily API rate limit reached with HTTP 429 (code 429)"))

setattr(list_past_meeting_participants_daily_limit_failure, '__module__', "dex_connectors.zoom.zoommock")

def list_past_meeting_participants_invalid_response_failure() -> Case[ParticipantPage]:
    return Case.branch_result(INVALID_RESPONSE, ParticipantPage(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "listPastMeetingParticipants", "Zoom returned an unusable participant page."))

setattr(list_past_meeting_participants_invalid_response_failure, '__module__', "dex_connectors.zoom.zoommock")


class Mock:

    def __init__(self, connection_name: str = "zoom") -> None:
        self.list_meetings_script: Query[ListMeetingsInput, MeetingPage] = Query(LIST_MEETINGS_DEFINITION)
        self.list_meetings_script.default(list_meetings_listed_default_answer())
        self.get_meeting_script: Query[GetMeetingInput, Meeting] = Query(GET_MEETING_DEFINITION)
        self.get_meeting_script.default(get_meeting_found_default_answer())
        self.create_meeting_script: Mutation[CreateMeetingInput, Meeting] = Mutation(CREATE_MEETING_DEFINITION)
        self.create_meeting_script.default(create_meeting_created_default_answer())
        self.update_meeting_script: Mutation[UpdateMeetingInput, UpdatedMeeting] = Mutation(UPDATE_MEETING_DEFINITION)
        self.update_meeting_script.default(update_meeting_updated_default_answer())
        self.list_past_meeting_participants_script: Query[ListPastMeetingParticipantsInput, ParticipantPage] = Query(LIST_PAST_MEETING_PARTICIPANTS_DEFINITION)
        self.list_past_meeting_participants_script.default(list_past_meeting_participants_listed_default_answer())
        self._connection = new_connection_with_operations(
            list_meetings=self.list_meetings_script,
            get_meeting=self.get_meeting_script,
            create_meeting=self.create_meeting_script,
            update_meeting=self.update_meeting_script,
            list_past_meeting_participants=self.list_past_meeting_participants_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.zoom.zoommock")

__all__ = [
    "Mock",
    "list_meetings_listed",
    "list_meetings_provider_rejected",
    "list_meetings_invalid_response",
    "list_meetings_defect",
    "list_meetings_retry",
    "list_meetings_listed_default_answer",
    "list_meetings_provider_rejected_failure",
    "list_meetings_daily_limit_failure",
    "list_meetings_invalid_response_failure",
    "get_meeting_found",
    "get_meeting_not_found",
    "get_meeting_provider_rejected",
    "get_meeting_invalid_response",
    "get_meeting_defect",
    "get_meeting_retry",
    "get_meeting_found_default_answer",
    "get_meeting_not_found_failure",
    "get_meeting_provider_rejected_failure",
    "get_meeting_daily_limit_failure",
    "get_meeting_invalid_response_failure",
    "create_meeting_created",
    "create_meeting_provider_rejected",
    "create_meeting_uncertain",
    "create_meeting_defect",
    "create_meeting_retry",
    "create_meeting_created_default_answer",
    "create_meeting_provider_rejected_failure",
    "create_meeting_daily_limit_failure",
    "update_meeting_updated",
    "update_meeting_not_found",
    "update_meeting_provider_rejected",
    "update_meeting_defect",
    "update_meeting_retry",
    "update_meeting_updated_default_answer",
    "update_meeting_not_found_failure",
    "update_meeting_provider_rejected_failure",
    "update_meeting_daily_limit_failure",
    "list_past_meeting_participants_listed",
    "list_past_meeting_participants_not_found",
    "list_past_meeting_participants_provider_rejected",
    "list_past_meeting_participants_invalid_response",
    "list_past_meeting_participants_defect",
    "list_past_meeting_participants_retry",
    "list_past_meeting_participants_listed_default_answer",
    "list_past_meeting_participants_not_found_failure",
    "list_past_meeting_participants_provider_rejected_failure",
    "list_past_meeting_participants_daily_limit_failure",
    "list_past_meeting_participants_invalid_response_failure",
]
