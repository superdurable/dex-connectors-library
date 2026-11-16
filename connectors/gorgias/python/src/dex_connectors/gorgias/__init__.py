# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Dex-native Gorgias ticket and customer operations and the ticketChanged poll Trigger."""

from .client import Client
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import *  # noqa: F403
from .steps import *  # noqa: F403
from .triggers import (
    MAXIMUM_SEEN_EVENT_KEYS,
    TICKET_CHANGED_TRIGGER_DEFINITION,
    TICKET_EVENT_OVERLAP,
    TICKET_EVENT_PAGE_SIZE,
    TICKET_EVENT_TYPES,
    ProjectTicketChangedTriggerRoute,
    ProjectTicketTriggerRunnerConfig,
    TicketChangedTriggerRoute,
    TicketEventType,
    TicketTriggerRunnerConfig,
    decode_ticket_changed_event_payload,
    decode_ticket_changed_trigger_configuration,
    define_ticket_changed_trigger_binding,
    new_project_ticket_trigger_runner,
    new_ticket_changed_trigger,
    new_ticket_trigger_runner,
    validate_ticket_changed_trigger_configuration,
)

__all__ = [
    "MAXIMUM_SEEN_EVENT_KEYS",
    "TICKET_CHANGED_TRIGGER_DEFINITION",
    "TICKET_EVENT_OVERLAP",
    "TICKET_EVENT_PAGE_SIZE",
    "TICKET_EVENT_TYPES",
    "Client",
    "ProjectTicketChangedTriggerRoute",
    "ProjectTicketTriggerRunnerConfig",
    "TicketChangedTriggerRoute",
    "TicketEventType",
    "TicketTriggerRunnerConfig",
    "decode_ticket_changed_event_payload",
    "decode_ticket_changed_trigger_configuration",
    "define_ticket_changed_trigger_binding",
    "new_project_ticket_trigger_runner",
    "new_ticket_changed_trigger",
    "new_ticket_trigger_runner",
    "validate_ticket_changed_trigger_configuration",
    "Connection",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
]
