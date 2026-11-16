# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Dex-native Twilio Messaging connector."""

from .client import Client, Config, Credentials
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import (
    GetMessageInput,
    Message,
    MessageReceivedEvent,
    MessageReceivedTriggerConfiguration,
    SendMessageInput,
)
from .operations import GetMessageOperation, SendMessageOperation
from .steps import new_get_message_step, new_send_message_step
from .triggers import (
    INBOUND_MESSAGE_DIRECTION,
    MESSAGE_RECEIVED_TRIGGER_DEFINITION,
    MESSAGE_STATUS_RECEIVED,
    MESSAGE_STATUS_RECEIVING,
    MessageReceivedTriggerRoute,
    ProjectMessageReceivedTriggerRoute,
    decode_message_received_trigger_configuration,
    define_message_received_trigger_binding,
    new_message_received_trigger,
    new_message_trigger_runner,
    new_project_message_trigger_runner,
    validate_message_received_trigger_configuration,
)

__all__ = [
    "INBOUND_MESSAGE_DIRECTION",
    "MESSAGE_RECEIVED_TRIGGER_DEFINITION",
    "MESSAGE_STATUS_RECEIVED",
    "MESSAGE_STATUS_RECEIVING",
    "Client",
    "Config",
    "Credentials",
    "Connection",
    "GetMessageInput",
    "GetMessageOperation",
    "Message",
    "MessageReceivedEvent",
    "MessageReceivedTriggerConfiguration",
    "MessageReceivedTriggerRoute",
    "ProjectMessageReceivedTriggerRoute",
    "SendMessageInput",
    "SendMessageOperation",
    "decode_message_received_trigger_configuration",
    "define_message_received_trigger_binding",
    "new_connection",
    "new_connection_with_operations",
    "new_get_message_step",
    "new_message_received_trigger",
    "new_message_trigger_runner",
    "new_project_connection",
    "new_project_message_trigger_runner",
    "new_send_message_step",
    "validate_message_received_trigger_configuration",
]
