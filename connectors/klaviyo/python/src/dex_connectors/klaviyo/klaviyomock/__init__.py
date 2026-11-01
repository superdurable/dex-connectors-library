# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed Klaviyo scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..client import ACCEPTED, CONNECTOR_ID, CREATE_EVENT_DEFINITION, FOUND, GET_PROFILE_DEFINITION, NOT_FOUND
from ..connection import Connection, new_connection_with_operations
from ..models import CreateEventInput, EventAcceptance, GetProfileInput, Profile


def get_profile_found(value: Profile, failure: Failure | None = None) -> Case[Profile]:
    """Script the successful found route."""
    return Case.branch_result(FOUND, value, failure)


def get_profile_not_found(value: Profile, failure: Failure | None = None) -> Case[Profile]:
    """Script a missing profile."""
    return Case.branch_result(NOT_FOUND, value, failure)


def get_profile_default_answer() -> Case[Profile]:
    """Return the manifest's getProfile default answer."""
    return get_profile_found(Profile("profile-meridian-1", "member@meridian.co"))


def create_event_accepted(value: EventAcceptance, failure: Failure | None = None) -> Case[EventAcceptance]:
    """Script the accepted route."""
    return Case.branch_result(ACCEPTED, value, failure)


def create_event_default_answer() -> Case[EventAcceptance]:
    """Return the manifest's createEvent default answer."""
    return create_event_accepted(EventAcceptance(True))


class Mock:
    """Script Klaviyo operations, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "klaviyo") -> None:
        self.get_profile_script: Query[GetProfileInput, Profile] = Query(GET_PROFILE_DEFINITION)
        self.get_profile_script.default(get_profile_default_answer())
        self.create_event_script: Mutation[CreateEventInput, EventAcceptance] = Mutation(
            CREATE_EVENT_DEFINITION
        )
        self.create_event_script.default(create_event_default_answer())
        self._connection = new_connection_with_operations(
            self.create_event_script, self.get_profile_script, ConnectionRef(CONNECTOR_ID, connection_name)
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "get_profile_found",
    "get_profile_not_found",
    "get_profile_default_answer",
    "create_event_accepted",
    "create_event_default_answer",
]
