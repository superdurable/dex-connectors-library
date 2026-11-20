# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Udesk connector for Dex Flows."""

from .client import Client, Config, Credentials, ListTicketStatusesOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import ListTicketStatusesInput, ListTicketStatusesOutput, TicketStatus
from .steps import new_list_ticket_statuses_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "TicketStatus",
    "ListTicketStatusesInput",
    "ListTicketStatusesOperation",
    "ListTicketStatusesOutput",
    "new_connection",
    "new_connection_with_operations",
    "new_list_ticket_statuses_step",
    "new_project_connection",
]
