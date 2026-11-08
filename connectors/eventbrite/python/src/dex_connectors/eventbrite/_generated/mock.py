# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AttendeeMetadata, EventMetadata, EventsPage, GetAttendeeInput, ListEventsInput, ListOrganizationsInput, OrganizationMetadata, OrganizationsPage
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_ATTENDEE_DEFINITION, INVALID_RESPONSE, LISTED, LIST_EVENTS_DEFINITION, LIST_ORGANIZATIONS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def list_organizations_listed(value: OrganizationsPage, failure: Failure | None = None) -> Case[OrganizationsPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_organizations_listed, '__module__', "dex_connectors.eventbrite.eventbritemock")

def list_organizations_provider_rejected(value: OrganizationsPage, failure: Failure | None = None) -> Case[OrganizationsPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_organizations_provider_rejected, '__module__', "dex_connectors.eventbrite.eventbritemock")

def list_organizations_invalid_response(value: OrganizationsPage, failure: Failure | None = None) -> Case[OrganizationsPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_organizations_invalid_response, '__module__', "dex_connectors.eventbrite.eventbritemock")

def list_organizations_defect(value: OrganizationsPage, failure: Failure | None = None) -> Case[OrganizationsPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_organizations_defect, '__module__', "dex_connectors.eventbrite.eventbritemock")

def list_organizations_retry(failure: Failure, after_seconds: float = 0) -> Case[OrganizationsPage]:
    return Case.retry(failure, after_seconds)

setattr(list_organizations_retry, '__module__', "dex_connectors.eventbrite.eventbritemock")

def list_organizations_listed_default() -> Case[OrganizationsPage]:
    return Case.branch_result(LISTED, OrganizationsPage(organizations=(OrganizationMetadata(id='12345', name='Example organization'),), page_number=1, page_count=1))

setattr(list_organizations_listed_default, '__module__', "dex_connectors.eventbrite.eventbritemock")

def list_events_listed(value: EventsPage, failure: Failure | None = None) -> Case[EventsPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_events_listed, '__module__', "dex_connectors.eventbrite.eventbritemock")

def list_events_not_found(value: EventsPage, failure: Failure | None = None) -> Case[EventsPage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_events_not_found, '__module__', "dex_connectors.eventbrite.eventbritemock")

def list_events_provider_rejected(value: EventsPage, failure: Failure | None = None) -> Case[EventsPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_events_provider_rejected, '__module__', "dex_connectors.eventbrite.eventbritemock")

def list_events_invalid_response(value: EventsPage, failure: Failure | None = None) -> Case[EventsPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_events_invalid_response, '__module__', "dex_connectors.eventbrite.eventbritemock")

def list_events_defect(value: EventsPage, failure: Failure | None = None) -> Case[EventsPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_events_defect, '__module__', "dex_connectors.eventbrite.eventbritemock")

def list_events_retry(failure: Failure, after_seconds: float = 0) -> Case[EventsPage]:
    return Case.retry(failure, after_seconds)

setattr(list_events_retry, '__module__', "dex_connectors.eventbrite.eventbritemock")

def list_events_listed_default() -> Case[EventsPage]:
    return Case.branch_result(LISTED, EventsPage(events=(EventMetadata(id='67890', name='Example event', status='live'),), page_number=1, page_count=1))

setattr(list_events_listed_default, '__module__', "dex_connectors.eventbrite.eventbritemock")

def get_attendee_found(value: AttendeeMetadata, failure: Failure | None = None) -> Case[AttendeeMetadata]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_attendee_found, '__module__', "dex_connectors.eventbrite.eventbritemock")

def get_attendee_not_found(value: AttendeeMetadata, failure: Failure | None = None) -> Case[AttendeeMetadata]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_attendee_not_found, '__module__', "dex_connectors.eventbrite.eventbritemock")

def get_attendee_provider_rejected(value: AttendeeMetadata, failure: Failure | None = None) -> Case[AttendeeMetadata]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_attendee_provider_rejected, '__module__', "dex_connectors.eventbrite.eventbritemock")

def get_attendee_invalid_response(value: AttendeeMetadata, failure: Failure | None = None) -> Case[AttendeeMetadata]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_attendee_invalid_response, '__module__', "dex_connectors.eventbrite.eventbritemock")

def get_attendee_defect(value: AttendeeMetadata, failure: Failure | None = None) -> Case[AttendeeMetadata]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_attendee_defect, '__module__', "dex_connectors.eventbrite.eventbritemock")

def get_attendee_retry(failure: Failure, after_seconds: float = 0) -> Case[AttendeeMetadata]:
    return Case.retry(failure, after_seconds)

setattr(get_attendee_retry, '__module__', "dex_connectors.eventbrite.eventbritemock")

def get_attendee_found_default() -> Case[AttendeeMetadata]:
    return Case.branch_result(FOUND, AttendeeMetadata(id='13579', event_id='67890', status='attending', created_at='2026-11-07T10:00:00Z'))

setattr(get_attendee_found_default, '__module__', "dex_connectors.eventbrite.eventbritemock")


class Mock:

    def __init__(self, connection_name: str = "eventbrite") -> None:
        self.list_organizations_script: Query[ListOrganizationsInput, OrganizationsPage] = Query(LIST_ORGANIZATIONS_DEFINITION)
        self.list_organizations_script.default(list_organizations_listed_default())
        self.list_events_script: Query[ListEventsInput, EventsPage] = Query(LIST_EVENTS_DEFINITION)
        self.list_events_script.default(list_events_listed_default())
        self.get_attendee_script: Query[GetAttendeeInput, AttendeeMetadata] = Query(GET_ATTENDEE_DEFINITION)
        self.get_attendee_script.default(get_attendee_found_default())
        self._connection = new_connection_with_operations(
            list_organizations=self.list_organizations_script,
            list_events=self.list_events_script,
            get_attendee=self.get_attendee_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.eventbrite.eventbritemock")

__all__ = [
    "Mock",
    "list_organizations_listed",
    "list_organizations_provider_rejected",
    "list_organizations_invalid_response",
    "list_organizations_defect",
    "list_organizations_retry",
    "list_organizations_listed_default",
    "list_events_listed",
    "list_events_not_found",
    "list_events_provider_rejected",
    "list_events_invalid_response",
    "list_events_defect",
    "list_events_retry",
    "list_events_listed_default",
    "get_attendee_found",
    "get_attendee_not_found",
    "get_attendee_provider_rejected",
    "get_attendee_invalid_response",
    "get_attendee_defect",
    "get_attendee_retry",
    "get_attendee_found_default",
]
