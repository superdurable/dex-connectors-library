# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Zoho Desk connector for Dex Python Flows."""

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
]
