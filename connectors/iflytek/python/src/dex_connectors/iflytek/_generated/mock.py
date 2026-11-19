# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ChatCompletion, CreateChatCompletionInput
from .definitions import CONNECTOR_ID, COMPLETED, CREATE_CHAT_COMPLETION_DEFINITION, DEFECT, INVALID_RESPONSE, PROVIDER_REJECTED

def create_chat_completion_completed(value: ChatCompletion, failure: Failure | None = None) -> Case[ChatCompletion]:
    return Case.branch_result(COMPLETED, value, failure)

setattr(create_chat_completion_completed, '__module__', "dex_connectors.iflytek.iflytekmock")

def create_chat_completion_provider_rejected(value: ChatCompletion, failure: Failure | None = None) -> Case[ChatCompletion]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_chat_completion_provider_rejected, '__module__', "dex_connectors.iflytek.iflytekmock")

def create_chat_completion_invalid_response(value: ChatCompletion, failure: Failure | None = None) -> Case[ChatCompletion]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(create_chat_completion_invalid_response, '__module__', "dex_connectors.iflytek.iflytekmock")

def create_chat_completion_defect(value: ChatCompletion, failure: Failure | None = None) -> Case[ChatCompletion]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_chat_completion_defect, '__module__', "dex_connectors.iflytek.iflytekmock")

def create_chat_completion_retry(failure: Failure, after_seconds: float = 0) -> Case[ChatCompletion]:
    return Case.retry(failure, after_seconds)

setattr(create_chat_completion_retry, '__module__', "dex_connectors.iflytek.iflytekmock")

def create_chat_completion_completed_default() -> Case[ChatCompletion]:
    return Case.branch_result(COMPLETED, ChatCompletion(content='你好！', model='lite', prompt_tokens=4, completion_tokens=3, total_tokens=7))

setattr(create_chat_completion_completed_default, '__module__', "dex_connectors.iflytek.iflytekmock")


class Mock:

    def __init__(self, connection_name: str = "iflytek") -> None:
        self.create_chat_completion_script: Query[CreateChatCompletionInput, ChatCompletion] = Query(CREATE_CHAT_COMPLETION_DEFINITION)
        self.create_chat_completion_script.default(create_chat_completion_completed_default())
        self._connection = new_connection_with_operations(
            create_chat_completion=self.create_chat_completion_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.iflytek.iflytekmock")

__all__ = [
    "Mock",
    "create_chat_completion_completed",
    "create_chat_completion_provider_rejected",
    "create_chat_completion_invalid_response",
    "create_chat_completion_defect",
    "create_chat_completion_retry",
    "create_chat_completion_completed_default",
]
