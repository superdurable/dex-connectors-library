# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Gmail operations and the messageReceived and replyReceived Triggers for Dex Flows."""

from .connection import Connection, new_connection, new_connection_with_operations
from .definitions import (
    CONNECTOR_ID,
    GET_MESSAGE_DEFINITION,
    PROVIDER,
    REPLY_TO_MESSAGE_DEFINITION,
    SEND_MESSAGE_DEFINITION,
)
from .models import (
    Config,
    Credentials,
    GetMessageInput,
    Message,
    MessageEvent,
    MessageMatcher,
    MessageReceivedTriggerConfiguration,
    ReplyReceivedTriggerConfiguration,
    ReplyToMessageInput,
    SendMessageInput,
    SendMessageOutput,
)
from .operations import GetMessageOperation, ReplyToMessageOperation, SendMessageOperation
from .project import new_project_connection, new_project_provider
from .project_credentials import decode_credentials, encode_credentials
from .provider import GmailProvider
from .refresh import CredentialRefreshDriver
from .steps import (
    new_get_message_step,
    new_reply_to_message_step,
    new_send_message_step,
)
from .triggers import (
    MESSAGE_RECEIVED_TRIGGER_DEFINITION,
    REPLY_RECEIVED_TRIGGER_DEFINITION,
    MessageReceivedTriggerRoute,
    MessageTriggerRunner,
    ProjectMessageReceivedTriggerRoute,
    ProjectReplyReceivedTriggerRoute,
    ReplyReceivedTriggerRoute,
    decode_message_event_payload,
    decode_message_received_trigger_configuration,
    decode_reply_received_trigger_configuration,
    define_message_received_trigger_binding,
    define_reply_received_trigger_binding,
    matches_message_event,
    new_message_received_trigger,
    new_message_trigger_runner,
    new_project_message_trigger_runner,
    new_reply_received_trigger,
    validate_message_received_trigger_configuration,
    validate_reply_received_trigger_configuration,
)

__all__ = [
    "CONNECTOR_ID",
    "PROVIDER",
    "Config",
    "Connection",
    "Credentials",
    "CredentialRefreshDriver",
    "GetMessageInput",
    "GetMessageOperation",
    "GmailProvider",
    "Message",
    "ReplyToMessageInput",
    "ReplyToMessageOperation",
    "SendMessageInput",
    "SendMessageOperation",
    "SendMessageOutput",
    "GET_MESSAGE_DEFINITION",
    "SEND_MESSAGE_DEFINITION",
    "REPLY_TO_MESSAGE_DEFINITION",
    "decode_credentials",
    "encode_credentials",
    "new_connection",
    "new_connection_with_operations",
    "new_get_message_step",
    "new_project_connection",
    "new_project_provider",
    "new_reply_to_message_step",
    "new_send_message_step",
    "MESSAGE_RECEIVED_TRIGGER_DEFINITION",
    "REPLY_RECEIVED_TRIGGER_DEFINITION",
    "MessageEvent",
    "MessageMatcher",
    "MessageReceivedTriggerConfiguration",
    "MessageReceivedTriggerRoute",
    "MessageTriggerRunner",
    "ProjectMessageReceivedTriggerRoute",
    "ProjectReplyReceivedTriggerRoute",
    "ReplyReceivedTriggerConfiguration",
    "ReplyReceivedTriggerRoute",
    "decode_message_event_payload",
    "decode_message_received_trigger_configuration",
    "decode_reply_received_trigger_configuration",
    "define_message_received_trigger_binding",
    "define_reply_received_trigger_binding",
    "matches_message_event",
    "new_message_received_trigger",
    "new_message_trigger_runner",
    "new_project_message_trigger_runner",
    "new_reply_received_trigger",
    "validate_message_received_trigger_configuration",
    "validate_reply_received_trigger_configuration",
]
