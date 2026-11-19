# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetConversationInput, GetConversationOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_CONVERSATION_DEFINITION, INVALID_RESPONSE, NO_DATA, PROVIDER_REJECTED

def get_conversation_found(value: GetConversationOutput, failure: Failure | None = None) -> Case[GetConversationOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_conversation_found, '__module__', "dex_connectors.meiqia.meiqiamock")

def get_conversation_no_data(value: GetConversationOutput, failure: Failure | None = None) -> Case[GetConversationOutput]:
    return Case.branch_result(NO_DATA, value, failure)

setattr(get_conversation_no_data, '__module__', "dex_connectors.meiqia.meiqiamock")

def get_conversation_provider_rejected(value: GetConversationOutput, failure: Failure | None = None) -> Case[GetConversationOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_conversation_provider_rejected, '__module__', "dex_connectors.meiqia.meiqiamock")

def get_conversation_invalid_response(value: GetConversationOutput, failure: Failure | None = None) -> Case[GetConversationOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_conversation_invalid_response, '__module__', "dex_connectors.meiqia.meiqiamock")

def get_conversation_defect(value: GetConversationOutput, failure: Failure | None = None) -> Case[GetConversationOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_conversation_defect, '__module__', "dex_connectors.meiqia.meiqiamock")

def get_conversation_retry(failure: Failure, after_seconds: float = 0) -> Case[GetConversationOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_conversation_retry, '__module__', "dex_connectors.meiqia.meiqiamock")

def get_conversation_sample_conversation() -> Case[GetConversationOutput]:
    return Case.branch_result(FOUND, GetConversationOutput(conversation_id=123, started_at='2026-11-13 10:00:00', ended_at='2026-11-13 10:20:00', main_channel='web'))

setattr(get_conversation_sample_conversation, '__module__', "dex_connectors.meiqia.meiqiamock")

def get_conversation_no_conversation() -> Case[GetConversationOutput]:
    return Case.branch_result(NO_DATA, GetConversationOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getConversation", "Meiqia did not find the conversation."))

setattr(get_conversation_no_conversation, '__module__', "dex_connectors.meiqia.meiqiamock")


class Mock:

    def __init__(self, connection_name: str = "meiqia") -> None:
        self.get_conversation_script: Query[GetConversationInput, GetConversationOutput] = Query(GET_CONVERSATION_DEFINITION)
        self.get_conversation_script.default(get_conversation_sample_conversation())
        self._connection = new_connection_with_operations(
            get_conversation=self.get_conversation_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.meiqia.meiqiamock")

__all__ = [
    "Mock",
    "get_conversation_found",
    "get_conversation_no_data",
    "get_conversation_provider_rejected",
    "get_conversation_invalid_response",
    "get_conversation_defect",
    "get_conversation_retry",
    "get_conversation_sample_conversation",
    "get_conversation_no_conversation",
]
