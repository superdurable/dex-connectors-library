# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated manifest-backed provider outcomes and scripts."""

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import CreateEventInput, EventAcceptance, GetProfileInput, Profile
from .definitions import CONNECTOR_ID, ACCEPTED, CREATE_EVENT_DEFINITION, DEFECT, FOUND, GET_PROFILE_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_profile_found(value: Profile, failure: Failure | None = None) -> Case[Profile]:
    """Script the found branch."""
    return Case.branch_result(FOUND, value, failure)

setattr(get_profile_found, '__module__', "dex_connectors.klaviyo.klaviyomock")

def get_profile_not_found(value: Profile, failure: Failure | None = None) -> Case[Profile]:
    """Script the notFound branch."""
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_profile_not_found, '__module__', "dex_connectors.klaviyo.klaviyomock")

def get_profile_provider_rejected(value: Profile, failure: Failure | None = None) -> Case[Profile]:
    """Script the providerRejected branch."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_profile_provider_rejected, '__module__', "dex_connectors.klaviyo.klaviyomock")

def get_profile_invalid_response(value: Profile, failure: Failure | None = None) -> Case[Profile]:
    """Script the invalidResponse branch."""
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_profile_invalid_response, '__module__', "dex_connectors.klaviyo.klaviyomock")

def get_profile_defect(value: Profile, failure: Failure | None = None) -> Case[Profile]:
    """Script the defect branch."""
    return Case.branch_result(DEFECT, value, failure)

setattr(get_profile_defect, '__module__', "dex_connectors.klaviyo.klaviyomock")

def get_profile_retry(failure: Failure, after_seconds: float = 0) -> Case[Profile]:
    """Script a retry of this provider operation."""
    return Case.retry(failure, after_seconds)

setattr(get_profile_retry, '__module__', "dex_connectors.klaviyo.klaviyomock")

def get_profile_found_default_profile() -> Case[Profile]:
    """Return the manifest foundDefaultProfile outcome."""
    return Case.branch_result(FOUND, Profile(id='profile-meridian-1', email='member@meridian.co'))

setattr(get_profile_found_default_profile, '__module__', "dex_connectors.klaviyo.klaviyomock")

def create_event_accepted(value: EventAcceptance, failure: Failure | None = None) -> Case[EventAcceptance]:
    """Script the accepted branch."""
    return Case.branch_result(ACCEPTED, value, failure)

setattr(create_event_accepted, '__module__', "dex_connectors.klaviyo.klaviyomock")

def create_event_provider_rejected(value: EventAcceptance, failure: Failure | None = None) -> Case[EventAcceptance]:
    """Script the providerRejected branch."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_event_provider_rejected, '__module__', "dex_connectors.klaviyo.klaviyomock")

def create_event_defect(value: EventAcceptance, failure: Failure | None = None) -> Case[EventAcceptance]:
    """Script the defect branch."""
    return Case.branch_result(DEFECT, value, failure)

setattr(create_event_defect, '__module__', "dex_connectors.klaviyo.klaviyomock")

def create_event_retry(failure: Failure, after_seconds: float = 0) -> Case[EventAcceptance]:
    """Script a retry of this provider operation."""
    return Case.retry(failure, after_seconds)

setattr(create_event_retry, '__module__', "dex_connectors.klaviyo.klaviyomock")

def create_event_accepted_default_event() -> Case[EventAcceptance]:
    """Return the manifest acceptedDefaultEvent outcome."""
    return Case.branch_result(ACCEPTED, EventAcceptance(accepted=True))

setattr(create_event_accepted_default_event, '__module__', "dex_connectors.klaviyo.klaviyomock")


class Mock:
    """Script manifest outcomes for this connector."""

    def __init__(self, connection_name: str = "klaviyo") -> None:
        self.get_profile_script: Query[GetProfileInput, Profile] = Query(GET_PROFILE_DEFINITION)
        self.get_profile_script.default(get_profile_found_default_profile())
        self.create_event_script: Mutation[CreateEventInput, EventAcceptance] = Mutation(CREATE_EVENT_DEFINITION)
        self.create_event_script.default(create_event_accepted_default_event())
        self._connection = new_connection_with_operations(
            get_profile=self.get_profile_script,
            create_event=self.create_event_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        """Return this mock connection for Dex Step registration."""
        return self._connection

setattr(Mock, '__module__', "dex_connectors.klaviyo.klaviyomock")

__all__ = [
    "Mock",
    "get_profile_found",
    "get_profile_not_found",
    "get_profile_provider_rejected",
    "get_profile_invalid_response",
    "get_profile_defect",
    "get_profile_retry",
    "get_profile_found_default_profile",
    "create_event_accepted",
    "create_event_provider_rejected",
    "create_event_defect",
    "create_event_retry",
    "create_event_accepted_default_event",
]
