# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import timedelta

from dex_connectors.sdk import (
    BranchDefinition,
    MutationDefinition,
    OperationRef,
    QueryDefinition,
    RetryPolicy,
    StepDefaults,
    default_connector_step_execute_retry_policy,
)

CONNECTOR_ID = "zoom"
PROVIDER = "zoom"
LIST_MEETINGS_OPERATION_ID = "listMeetings"
GET_MEETING_OPERATION_ID = "getMeeting"
CREATE_MEETING_OPERATION_ID = "createMeeting"
UPDATE_MEETING_OPERATION_ID = "updateMeeting"
LIST_PAST_MEETING_PARTICIPANTS_OPERATION_ID = "listPastMeetingParticipants"
CREATED = "created"
DEFECT = "defect"
FOUND = "found"
INVALID_RESPONSE = "invalidResponse"
LISTED = "listed"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
UNCERTAIN = "uncertain"
UPDATED = "updated"

LIST_MEETINGS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_MEETINGS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "One page of the user's meetings was listed.", False),
        BranchDefinition(PROVIDER_REJECTED, "Zoom conclusively rejected the listing, including a documented daily-limit 429 or a user without meeting hosting capability.", True),
        BranchDefinition(INVALID_RESPONSE, "Zoom returned an invalid or oversized meeting page.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

GET_MEETING_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_MEETING_OPERATION_ID),
    (
        BranchDefinition(FOUND, "The meeting was read.", False),
        BranchDefinition(NOT_FOUND, "The meeting does not exist or was deleted.", True),
        BranchDefinition(PROVIDER_REJECTED, "Zoom conclusively rejected the meeting read, including a documented daily-limit 429.", True),
        BranchDefinition(INVALID_RESPONSE, "Zoom returned an invalid or oversized meeting.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

CREATE_MEETING_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, CREATE_MEETING_OPERATION_ID),
    (
        BranchDefinition(CREATED, "Zoom created the meeting and returned its ID and join URL.", False),
        BranchDefinition(PROVIDER_REJECTED, "Zoom conclusively rejected the meeting, including a documented daily-limit 429 or a user who cannot host; nothing was created.", True),
        BranchDefinition(UNCERTAIN, "The request was dispatched but its outcome cannot be confirmed; the meeting may exist and must not be created again automatically.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        1,
        execute_retry=RetryPolicy(timedelta(seconds=1), 2, timedelta(seconds=30), 5, timedelta(seconds=120)),
    ),
)

UPDATE_MEETING_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, UPDATE_MEETING_OPERATION_ID),
    (
        BranchDefinition(UPDATED, "Zoom accepted the patch.", False),
        BranchDefinition(NOT_FOUND, "The meeting does not exist or was deleted.", True),
        BranchDefinition(PROVIDER_REJECTED, "Zoom conclusively rejected the patch, including a documented daily-limit 429 or an invalid field value.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)

LIST_PAST_MEETING_PARTICIPANTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, LIST_PAST_MEETING_PARTICIPANTS_OPERATION_ID),
    (
        BranchDefinition(LISTED, "One page of participants was listed.", False),
        BranchDefinition(NOT_FOUND, "Zoom has no ended instance of the meeting.", True),
        BranchDefinition(PROVIDER_REJECTED, "Zoom conclusively rejected the listing, including a documented daily-limit 429, a free account, or a meeting older than Zoom keeps.", True),
        BranchDefinition(INVALID_RESPONSE, "Zoom returned an invalid or oversized participant page.", True),
        BranchDefinition(DEFECT, "Local input, connection configuration, or connector definition is invalid.", True),
    ),
    StepDefaults(
        timedelta(seconds=30),
        2,
        execute_retry=default_connector_step_execute_retry_policy(),
    ),
)
