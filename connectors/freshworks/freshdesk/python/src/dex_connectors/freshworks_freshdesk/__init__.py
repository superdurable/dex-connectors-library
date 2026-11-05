# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Freshdesk ticket operations for Dex Flows."""

from ._generated.config import Config, Credentials
from .client import Client
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import (
    AddNoteInput,
    AddNoteOutput,
    CreateTicketInput,
    CreateTicketOutput,
    GetTicketInput,
    SearchTicketsInput,
    SearchTicketsOutput,
    Ticket,
    TicketConversation,
    TicketDetails,
    TicketRequester,
    TicketRequesterInput,
    UpdateTicketInput,
    UpdateTicketOutput,
)
from .operations import (
    AddNoteOperation,
    CreateTicketOperation,
    GetTicketOperation,
    SearchTicketsOperation,
    UpdateTicketOperation,
)
from .steps import (
    new_add_note_step,
    new_create_ticket_step,
    new_get_ticket_step,
    new_search_tickets_step,
    new_update_ticket_step,
)

__all__ = [
    "Config",
    "Credentials",
    "Client",
    "Connection",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "AddNoteOperation",
    "CreateTicketOperation",
    "GetTicketOperation",
    "SearchTicketsOperation",
    "UpdateTicketOperation",
    "new_add_note_step",
    "new_create_ticket_step",
    "new_get_ticket_step",
    "new_search_tickets_step",
    "new_update_ticket_step",
    "AddNoteInput",
    "AddNoteOutput",
    "CreateTicketInput",
    "CreateTicketOutput",
    "GetTicketInput",
    "SearchTicketsInput",
    "SearchTicketsOutput",
    "Ticket",
    "TicketConversation",
    "TicketDetails",
    "TicketRequester",
    "TicketRequesterInput",
    "UpdateTicketInput",
    "UpdateTicketOutput",
]
