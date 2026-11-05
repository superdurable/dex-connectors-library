# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Amazon SQS receive, delete, and visibility operations for Dex connector Steps."""

from ._generated.config import Config, Credentials
from ._generated.definitions import (
    CHANGE_MESSAGE_VISIBILITY_DEFINITION,
    CHANGED,
    CONNECTOR_ID,
    DEFECT,
    DELETE_MESSAGE_DEFINITION,
    DELETED,
    EMPTY,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
    RECEIPT_HANDLE_INVALID,
    RECEIVE_MESSAGES_DEFINITION,
    RECEIVED,
)
from .client import (
    MAXIMUM_MESSAGES_PER_RECEIVE,
    MAXIMUM_VISIBILITY_TIMEOUT_SECONDS,
    MAXIMUM_WAIT_TIME_SECONDS,
    ChangeMessageVisibilityOperation,
    Client,
    DeleteMessageOperation,
    ReceiveMessagesOperation,
    validate_credentials,
)
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import (
    ChangedMessageVisibility,
    ChangeMessageVisibilityInput,
    DeletedMessage,
    DeleteMessageInput,
    Message,
    MessageAttributeValue,
    ReceivedMessages,
    ReceiveMessagesInput,
)
from .steps import new_change_message_visibility_step, new_delete_message_step, new_receive_messages_step

__all__ = [
    "CHANGED",
    "CHANGE_MESSAGE_VISIBILITY_DEFINITION",
    "CONNECTOR_ID",
    "ChangeMessageVisibilityInput",
    "ChangeMessageVisibilityOperation",
    "ChangedMessageVisibility",
    "Client",
    "Config",
    "Connection",
    "Credentials",
    "DEFECT",
    "DELETED",
    "DELETE_MESSAGE_DEFINITION",
    "DeleteMessageInput",
    "DeleteMessageOperation",
    "DeletedMessage",
    "EMPTY",
    "INVALID_RESPONSE",
    "MAXIMUM_MESSAGES_PER_RECEIVE",
    "MAXIMUM_VISIBILITY_TIMEOUT_SECONDS",
    "MAXIMUM_WAIT_TIME_SECONDS",
    "Message",
    "MessageAttributeValue",
    "PROVIDER_REJECTED",
    "RECEIPT_HANDLE_INVALID",
    "RECEIVED",
    "RECEIVE_MESSAGES_DEFINITION",
    "ReceiveMessagesInput",
    "ReceiveMessagesOperation",
    "ReceivedMessages",
    "new_change_message_visibility_step",
    "new_connection",
    "new_connection_with_operations",
    "new_delete_message_step",
    "new_project_connection",
    "new_receive_messages_step",
    "validate_credentials",
]
