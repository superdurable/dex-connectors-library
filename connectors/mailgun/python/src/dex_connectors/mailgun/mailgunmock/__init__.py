# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed Mailgun scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure
from dex_connectors.sdk.connectormock import Case, Query

from ..client import CONNECTOR_ID, GET_EVENTS_DEFINITION, LIST_DOMAINS_DEFINITION, LISTED
from ..connection import Connection, new_connection_with_operations
from ..models import Domain, DomainsOutput, Event, EventsOutput, GetEventsInput, ListDomainsInput


def get_events_listed(value: EventsOutput, failure: Failure | None = None) -> Case[EventsOutput]:
    """Script the successful listed route."""
    return Case.branch_result(LISTED, value, failure)


def get_events_default_answer() -> Case[EventsOutput]:
    """Return the manifest's getEvents default answer."""
    return get_events_listed(EventsOutput((Event("event_meridian", "delivered", 1793100000),)))


def list_domains_listed(value: DomainsOutput, failure: Failure | None = None) -> Case[DomainsOutput]:
    """Script the successful listed route."""
    return Case.branch_result(LISTED, value, failure)


def list_domains_default_answer() -> Case[DomainsOutput]:
    """Return the manifest's listDomains default answer."""
    return list_domains_listed(DomainsOutput((Domain("example.test", "active"),), 1))


class Mock:
    """Script Mailgun Queries, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "mailgun") -> None:
        self.get_events_script: Query[GetEventsInput, EventsOutput] = Query(GET_EVENTS_DEFINITION)
        self.get_events_script.default(get_events_default_answer())
        self.list_domains_script: Query[ListDomainsInput, DomainsOutput] = Query(LIST_DOMAINS_DEFINITION)
        self.list_domains_script.default(list_domains_default_answer())
        self._connection = new_connection_with_operations(
            self.get_events_script, self.list_domains_script, ConnectionRef(CONNECTOR_ID, connection_name)
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "get_events_listed",
    "get_events_default_answer",
    "list_domains_listed",
    "list_domains_default_answer",
]
