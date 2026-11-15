# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Zoho Desk ticket operations and the ticketChanged poll Trigger for Dex Python Flows."""

from .client import Client as Client
from .client import Config as Config
from .client import Credentials as Credentials
from .connection import Connection as Connection
from .connection import new_connection as new_connection
from .connection import new_project_connection as new_project_connection
from .models import (
    AddCommentInput,
    AddCommentOutput,
    CreateTicketInput,
    CreateTicketOutput,
    GetTicketInput,
    SearchTicketsInput,
    SearchTicketsOutput,
    Ticket,
    TicketChangedEvent,
    TicketChangedTriggerConfiguration,
    TicketComment,
    TicketContact,
    TicketContactInput,
    TicketDetails,
    TicketThread,
    UpdateTicketInput,
    UpdateTicketOutput,
)
from .steps import (
    new_add_comment_step,
    new_create_ticket_step,
    new_get_ticket_step,
    new_search_tickets_step,
    new_update_ticket_step,
)
from .ticket_change_fetcher import (
    TICKET_POLL_PAGE_LIMIT,
    TicketChangeCursorInvalidError,
    ticket_change_event_id,
)
from .triggers import (
    TICKET_CHANGE_CREATED,
    TICKET_CHANGE_UPDATED,
    TICKET_CHANGED_TRIGGER_DEFINITION,
    TICKET_SEARCH_INDEX_LOOKBACK,
    ProjectTicketChangedTriggerRoute,
    ProjectTicketTriggerRunnerConfig,
    TicketChangedTriggerRoute,
    TicketChangeFetcher,
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
    "Client",
    "Config",
    "Credentials",
    "Connection",
    "new_connection",
    "new_project_connection",
    "SearchTicketsInput",
    "Ticket",
    "SearchTicketsOutput",
    "GetTicketInput",
    "TicketContact",
    "TicketThread",
    "TicketComment",
    "TicketDetails",
    "TicketContactInput",
    "CreateTicketInput",
    "CreateTicketOutput",
    "UpdateTicketInput",
    "UpdateTicketOutput",
    "AddCommentInput",
    "AddCommentOutput",
    "new_search_tickets_step",
    "new_get_ticket_step",
    "new_create_ticket_step",
    "new_update_ticket_step",
    "new_add_comment_step",
    "TicketChangedEvent",
    "TicketChangedTriggerConfiguration",
    "TICKET_CHANGE_CREATED",
    "TICKET_CHANGE_UPDATED",
    "TICKET_CHANGED_TRIGGER_DEFINITION",
    "TICKET_POLL_PAGE_LIMIT",
    "TICKET_SEARCH_INDEX_LOOKBACK",
    "ProjectTicketChangedTriggerRoute",
    "ProjectTicketTriggerRunnerConfig",
    "TicketChangeCursorInvalidError",
    "TicketChangeFetcher",
    "TicketChangedTriggerRoute",
    "TicketTriggerRunnerConfig",
    "decode_ticket_changed_event_payload",
    "decode_ticket_changed_trigger_configuration",
    "define_ticket_changed_trigger_binding",
    "new_project_ticket_trigger_runner",
    "new_ticket_changed_trigger",
    "new_ticket_trigger_runner",
    "ticket_change_event_id",
    "validate_ticket_changed_trigger_configuration",
]
