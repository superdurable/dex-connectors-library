# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GenerateTextInput, GenerateTextOutput, TokenUsage
from .definitions import CONNECTOR_ID, BLOCKED, DEFECT, GENERATED, GENERATE_TEXT_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED, TRUNCATED

def generate_text_generated(value: GenerateTextOutput, failure: Failure | None = None) -> Case[GenerateTextOutput]:
    return Case.branch_result(GENERATED, value, failure)

setattr(generate_text_generated, '__module__', "dex_connectors.zhipu.zhipumock")

def generate_text_truncated(value: GenerateTextOutput, failure: Failure | None = None) -> Case[GenerateTextOutput]:
    return Case.branch_result(TRUNCATED, value, failure)

setattr(generate_text_truncated, '__module__', "dex_connectors.zhipu.zhipumock")

def generate_text_blocked(value: GenerateTextOutput, failure: Failure | None = None) -> Case[GenerateTextOutput]:
    return Case.branch_result(BLOCKED, value, failure)

setattr(generate_text_blocked, '__module__', "dex_connectors.zhipu.zhipumock")

def generate_text_provider_rejected(value: GenerateTextOutput, failure: Failure | None = None) -> Case[GenerateTextOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(generate_text_provider_rejected, '__module__', "dex_connectors.zhipu.zhipumock")

def generate_text_invalid_response(value: GenerateTextOutput, failure: Failure | None = None) -> Case[GenerateTextOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(generate_text_invalid_response, '__module__', "dex_connectors.zhipu.zhipumock")

def generate_text_defect(value: GenerateTextOutput, failure: Failure | None = None) -> Case[GenerateTextOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(generate_text_defect, '__module__', "dex_connectors.zhipu.zhipumock")

def generate_text_retry(failure: Failure, after_seconds: float = 0) -> Case[GenerateTextOutput]:
    return Case.retry(failure, after_seconds)

setattr(generate_text_retry, '__module__', "dex_connectors.zhipu.zhipumock")

def generate_text_short_answer() -> Case[GenerateTextOutput]:
    return Case.branch_result(GENERATED, GenerateTextOutput(text='The connector shipped on time.', model='glm-4.7-flash', response_id='chatcmpl-mock-1', finish_reason='stop', usage=TokenUsage(prompt_tokens=12, completion_tokens=7, total_tokens=19)))

setattr(generate_text_short_answer, '__module__', "dex_connectors.zhipu.zhipumock")


class Mock:

    def __init__(self, connection_name: str = "zhipu") -> None:
        self.generate_text_script: Query[GenerateTextInput, GenerateTextOutput] = Query(GENERATE_TEXT_DEFINITION)
        self.generate_text_script.default(generate_text_short_answer())
        self._connection = new_connection_with_operations(
            generate_text=self.generate_text_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.zhipu.zhipumock")

__all__ = [
    "Mock",
    "generate_text_generated",
    "generate_text_truncated",
    "generate_text_blocked",
    "generate_text_provider_rejected",
    "generate_text_invalid_response",
    "generate_text_defect",
    "generate_text_retry",
    "generate_text_short_answer",
]
