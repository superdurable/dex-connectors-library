# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Postmark outbound message operations for Dex connector Steps."""

from .client import (
    CONNECTOR_ID,
    DEFECT,
    FOUND,
    GET_MESSAGE_DETAILS_DEFINITION,
    INVALID_RESPONSE,
    LIST_OUTBOUND_MESSAGES_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
    Client,
    Config,
    Credentials,
    GetMessageDetailsOperation,
    ListOutboundMessagesOperation,
)
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import GetMessageDetailsInput, ListOutboundMessagesInput, MessageDetails, MessagePage
from .steps import new_get_message_details_step, new_list_outbound_messages_step

__all__ = [
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "GetMessageDetailsInput",
    "GetMessageDetailsOperation",
    "ListOutboundMessagesInput",
    "ListOutboundMessagesOperation",
    "MessageDetails",
    "MessagePage",
    "new_connection",
    "new_connection_with_operations",
    "new_get_message_details_step",
    "new_list_outbound_messages_step",
    "new_project_connection",
    "CONNECTOR_ID",
    "DEFECT",
    "FOUND",
    "GET_MESSAGE_DETAILS_DEFINITION",
    "INVALID_RESPONSE",
    "LIST_OUTBOUND_MESSAGES_DEFINITION",
    "LISTED",
    "NOT_FOUND",
    "PROVIDER_REJECTED",
]
