# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Gmail operations for Dex Flows."""

from .connection import Connection, new_connection
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
    ReplyToMessageInput,
    SendMessageInput,
    SendMessageOutput,
)
from .operations import GetMessageOperation, ReplyToMessageOperation, SendMessageOperation
from .project import new_project_provider
from .project_credentials import decode_credentials, encode_credentials
from .provider import GmailProvider
from .refresh import CredentialRefreshDriver
from .steps import (
    new_get_message_step,
    new_reply_to_message_step,
    new_send_message_step,
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
    "new_get_message_step",
    "new_project_provider",
    "new_reply_to_message_step",
    "new_send_message_step",
]
