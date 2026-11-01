# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated manifest-backed provider outcomes and scripts."""

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Domain, DomainsOutput, Event, EventsOutput, GetEventsInput, ListDomainsInput
from .definitions import CONNECTOR_ID, DEFECT, GET_EVENTS_DEFINITION, INVALID_RESPONSE, LISTED, LIST_DOMAINS_DEFINITION, PROVIDER_REJECTED

def get_events_listed(value: EventsOutput, failure: Failure | None = None) -> Case[EventsOutput]:
    """Script the listed branch."""
    return Case.branch_result(LISTED, value, failure)

setattr(get_events_listed, '__module__', "dex_connectors.mailgun.mailgunmock")

def get_events_provider_rejected(value: EventsOutput, failure: Failure | None = None) -> Case[EventsOutput]:
    """Script the providerRejected branch."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_events_provider_rejected, '__module__', "dex_connectors.mailgun.mailgunmock")

def get_events_invalid_response(value: EventsOutput, failure: Failure | None = None) -> Case[EventsOutput]:
    """Script the invalidResponse branch."""
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_events_invalid_response, '__module__', "dex_connectors.mailgun.mailgunmock")

def get_events_defect(value: EventsOutput, failure: Failure | None = None) -> Case[EventsOutput]:
    """Script the defect branch."""
    return Case.branch_result(DEFECT, value, failure)

setattr(get_events_defect, '__module__', "dex_connectors.mailgun.mailgunmock")

def get_events_retry(failure: Failure, after_seconds: float = 0) -> Case[EventsOutput]:
    """Script a retry of this provider operation."""
    return Case.retry(failure, after_seconds)

setattr(get_events_retry, '__module__', "dex_connectors.mailgun.mailgunmock")

def get_events_listed_default() -> Case[EventsOutput]:
    """Return the manifest listedDefault outcome."""
    return Case.branch_result(LISTED, EventsOutput(events=(Event(id='event_meridian', event='delivered', timestamp=1793100000),)))

setattr(get_events_listed_default, '__module__', "dex_connectors.mailgun.mailgunmock")

def list_domains_listed(value: DomainsOutput, failure: Failure | None = None) -> Case[DomainsOutput]:
    """Script the listed branch."""
    return Case.branch_result(LISTED, value, failure)

setattr(list_domains_listed, '__module__', "dex_connectors.mailgun.mailgunmock")

def list_domains_provider_rejected(value: DomainsOutput, failure: Failure | None = None) -> Case[DomainsOutput]:
    """Script the providerRejected branch."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_domains_provider_rejected, '__module__', "dex_connectors.mailgun.mailgunmock")

def list_domains_invalid_response(value: DomainsOutput, failure: Failure | None = None) -> Case[DomainsOutput]:
    """Script the invalidResponse branch."""
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_domains_invalid_response, '__module__', "dex_connectors.mailgun.mailgunmock")

def list_domains_defect(value: DomainsOutput, failure: Failure | None = None) -> Case[DomainsOutput]:
    """Script the defect branch."""
    return Case.branch_result(DEFECT, value, failure)

setattr(list_domains_defect, '__module__', "dex_connectors.mailgun.mailgunmock")

def list_domains_retry(failure: Failure, after_seconds: float = 0) -> Case[DomainsOutput]:
    """Script a retry of this provider operation."""
    return Case.retry(failure, after_seconds)

setattr(list_domains_retry, '__module__', "dex_connectors.mailgun.mailgunmock")

def list_domains_listed_default() -> Case[DomainsOutput]:
    """Return the manifest listedDefault outcome."""
    return Case.branch_result(LISTED, DomainsOutput(domains=(Domain(name='example.test', state='active'),), total_count=1))

setattr(list_domains_listed_default, '__module__', "dex_connectors.mailgun.mailgunmock")


class Mock:
    """Script manifest outcomes for this connector."""

    def __init__(self, connection_name: str = "mailgun") -> None:
        self.get_events_script: Query[GetEventsInput, EventsOutput] = Query(GET_EVENTS_DEFINITION)
        self.get_events_script.default(get_events_listed_default())
        self.list_domains_script: Query[ListDomainsInput, DomainsOutput] = Query(LIST_DOMAINS_DEFINITION)
        self.list_domains_script.default(list_domains_listed_default())
        self._connection = new_connection_with_operations(
            get_events=self.get_events_script,
            list_domains=self.list_domains_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        """Return this mock connection for Dex Step registration."""
        return self._connection

setattr(Mock, '__module__', "dex_connectors.mailgun.mailgunmock")

__all__ = [
    "Mock",
    "get_events_listed",
    "get_events_provider_rejected",
    "get_events_invalid_response",
    "get_events_defect",
    "get_events_retry",
    "get_events_listed_default",
    "list_domains_listed",
    "list_domains_provider_rejected",
    "list_domains_invalid_response",
    "list_domains_defect",
    "list_domains_retry",
    "list_domains_listed_default",
]
