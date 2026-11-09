# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Chat, GetChatInput, SendMessageInput, SentMessage
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_CHAT_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED, SEND_MESSAGE_DEFINITION, SENT, UNCERTAIN

def send_message_sent(value: SentMessage, failure: Failure | None = None) -> Case[SentMessage]:
    return Case.branch_result(SENT, value, failure)

setattr(send_message_sent, '__module__', "dex_connectors.telegram.telegrammock")

def send_message_provider_rejected(value: SentMessage, failure: Failure | None = None) -> Case[SentMessage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(send_message_provider_rejected, '__module__', "dex_connectors.telegram.telegrammock")

def send_message_uncertain(value: SentMessage, failure: Failure) -> Case[SentMessage]:
    return Case.uncertain(value, failure)

setattr(send_message_uncertain, '__module__', "dex_connectors.telegram.telegrammock")

def send_message_defect(value: SentMessage, failure: Failure | None = None) -> Case[SentMessage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(send_message_defect, '__module__', "dex_connectors.telegram.telegrammock")

def send_message_retry(failure: Failure, after_seconds: float = 0) -> Case[SentMessage]:
    return Case.retry(failure, after_seconds)

setattr(send_message_retry, '__module__', "dex_connectors.telegram.telegrammock")

def send_message_sent_text() -> Case[SentMessage]:
    return Case.branch_result(SENT, SentMessage(chat_id=-1001234567890, message_id=52, date=1794110000))

setattr(send_message_sent_text, '__module__', "dex_connectors.telegram.telegrammock")

def get_chat_found(value: Chat, failure: Failure | None = None) -> Case[Chat]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_chat_found, '__module__', "dex_connectors.telegram.telegrammock")

def get_chat_not_found(value: Chat, failure: Failure | None = None) -> Case[Chat]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_chat_not_found, '__module__', "dex_connectors.telegram.telegrammock")

def get_chat_provider_rejected(value: Chat, failure: Failure | None = None) -> Case[Chat]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_chat_provider_rejected, '__module__', "dex_connectors.telegram.telegrammock")

def get_chat_invalid_response(value: Chat, failure: Failure | None = None) -> Case[Chat]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_chat_invalid_response, '__module__', "dex_connectors.telegram.telegrammock")

def get_chat_defect(value: Chat, failure: Failure | None = None) -> Case[Chat]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_chat_defect, '__module__', "dex_connectors.telegram.telegrammock")

def get_chat_retry(failure: Failure, after_seconds: float = 0) -> Case[Chat]:
    return Case.retry(failure, after_seconds)

setattr(get_chat_retry, '__module__', "dex_connectors.telegram.telegrammock")

def get_chat_accessible_chat() -> Case[Chat]:
    return Case.branch_result(FOUND, Chat(id=-1001234567890, type='supergroup', title='Meridian Team', username='meridian_team'))

setattr(get_chat_accessible_chat, '__module__', "dex_connectors.telegram.telegrammock")


class Mock:

    def __init__(self, connection_name: str = "telegram") -> None:
        self.send_message_script: Mutation[SendMessageInput, SentMessage] = Mutation(SEND_MESSAGE_DEFINITION)
        self.send_message_script.default(send_message_sent_text())
        self.get_chat_script: Query[GetChatInput, Chat] = Query(GET_CHAT_DEFINITION)
        self.get_chat_script.default(get_chat_accessible_chat())
        self._connection = new_connection_with_operations(
            send_message=self.send_message_script,
            get_chat=self.get_chat_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.telegram.telegrammock")

__all__ = [
    "Mock",
    "send_message_sent",
    "send_message_provider_rejected",
    "send_message_uncertain",
    "send_message_defect",
    "send_message_retry",
    "send_message_sent_text",
    "get_chat_found",
    "get_chat_not_found",
    "get_chat_provider_rejected",
    "get_chat_invalid_response",
    "get_chat_defect",
    "get_chat_retry",
    "get_chat_accessible_chat",
]
