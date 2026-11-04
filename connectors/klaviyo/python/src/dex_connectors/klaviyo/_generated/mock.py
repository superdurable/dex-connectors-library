# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import CreateEventInput, EventAcceptance, GetProfileInput, Profile
from .definitions import CONNECTOR_ID, ACCEPTED, CREATE_EVENT_DEFINITION, DEFECT, FOUND, GET_PROFILE_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def create_event_accepted(value: EventAcceptance, failure: Failure | None = None) -> Case[EventAcceptance]:
    return Case.branch_result(ACCEPTED, value, failure)

setattr(create_event_accepted, '__module__', "dex_connectors.klaviyo.klaviyomock")

def create_event_provider_rejected(value: EventAcceptance, failure: Failure | None = None) -> Case[EventAcceptance]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_event_provider_rejected, '__module__', "dex_connectors.klaviyo.klaviyomock")

def create_event_defect(value: EventAcceptance, failure: Failure | None = None) -> Case[EventAcceptance]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_event_defect, '__module__', "dex_connectors.klaviyo.klaviyomock")

def create_event_retry(failure: Failure, after_seconds: float = 0) -> Case[EventAcceptance]:
    return Case.retry(failure, after_seconds)

setattr(create_event_retry, '__module__', "dex_connectors.klaviyo.klaviyomock")

def create_event_accepted_default_event() -> Case[EventAcceptance]:
    return Case.branch_result(ACCEPTED, EventAcceptance(accepted=True))

setattr(create_event_accepted_default_event, '__module__', "dex_connectors.klaviyo.klaviyomock")

def get_profile_found(value: Profile, failure: Failure | None = None) -> Case[Profile]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_profile_found, '__module__', "dex_connectors.klaviyo.klaviyomock")

def get_profile_not_found(value: Profile, failure: Failure | None = None) -> Case[Profile]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_profile_not_found, '__module__', "dex_connectors.klaviyo.klaviyomock")

def get_profile_provider_rejected(value: Profile, failure: Failure | None = None) -> Case[Profile]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_profile_provider_rejected, '__module__', "dex_connectors.klaviyo.klaviyomock")

def get_profile_invalid_response(value: Profile, failure: Failure | None = None) -> Case[Profile]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_profile_invalid_response, '__module__', "dex_connectors.klaviyo.klaviyomock")

def get_profile_defect(value: Profile, failure: Failure | None = None) -> Case[Profile]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_profile_defect, '__module__', "dex_connectors.klaviyo.klaviyomock")

def get_profile_retry(failure: Failure, after_seconds: float = 0) -> Case[Profile]:
    return Case.retry(failure, after_seconds)

setattr(get_profile_retry, '__module__', "dex_connectors.klaviyo.klaviyomock")

def get_profile_found_default_profile() -> Case[Profile]:
    return Case.branch_result(FOUND, Profile(id='profile-meridian-1', email='member@meridian.co'))

setattr(get_profile_found_default_profile, '__module__', "dex_connectors.klaviyo.klaviyomock")


class Mock:

    def __init__(self, connection_name: str = "klaviyo") -> None:
        self.create_event_script: Mutation[CreateEventInput, EventAcceptance] = Mutation(CREATE_EVENT_DEFINITION)
        self.create_event_script.default(create_event_accepted_default_event())
        self.get_profile_script: Query[GetProfileInput, Profile] = Query(GET_PROFILE_DEFINITION)
        self.get_profile_script.default(get_profile_found_default_profile())
        self._connection = new_connection_with_operations(
            create_event=self.create_event_script,
            get_profile=self.get_profile_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.klaviyo.klaviyomock")

__all__ = [
    "Mock",
    "create_event_accepted",
    "create_event_provider_rejected",
    "create_event_defect",
    "create_event_retry",
    "create_event_accepted_default_event",
    "get_profile_found",
    "get_profile_not_found",
    "get_profile_provider_rejected",
    "get_profile_invalid_response",
    "get_profile_defect",
    "get_profile_retry",
    "get_profile_found_default_profile",
]
