# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Mailgun event and domain Queries for Dex connector Steps."""

from .client import (
    CONNECTOR_ID,
    DEFECT,
    GET_EVENTS_DEFINITION,
    INVALID_RESPONSE,
    LIST_DOMAINS_DEFINITION,
    LISTED,
    PROVIDER_REJECTED,
    Client,
    Config,
    Credentials,
    GetEventsOperation,
    ListDomainsOperation,
)
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import Domain, DomainsOutput, Event, EventsOutput, GetEventsInput, ListDomainsInput
from .steps import new_get_events_step, new_list_domains_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "Domain",
    "DomainsOutput",
    "Event",
    "EventsOutput",
    "GetEventsInput",
    "GetEventsOperation",
    "ListDomainsInput",
    "ListDomainsOperation",
    "new_connection",
    "new_connection_with_operations",
    "new_get_events_step",
    "new_list_domains_step",
    "new_project_connection",
    "CONNECTOR_ID",
    "DEFECT",
    "GET_EVENTS_DEFINITION",
    "INVALID_RESPONSE",
    "LIST_DOMAINS_DEFINITION",
    "LISTED",
    "PROVIDER_REJECTED",
]
