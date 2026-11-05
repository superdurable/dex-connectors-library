# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GenerateTextRequest, GenerateTextResponse, textgenMessage, textgenStructuredOutput, textgenUsage
from .definitions import CONNECTOR_ID, BLOCKED, DEFECT, GENERATED, GENERATE_TEXT_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED, TRUNCATED

def generate_text_generated(value: GenerateTextResponse, failure: Failure | None = None) -> Case[GenerateTextResponse]:
    return Case.branch_result(GENERATED, value, failure)

setattr(generate_text_generated, '__module__', "dex_connectors.superdurable_llm.llmmock")

def generate_text_truncated(value: GenerateTextResponse, failure: Failure | None = None) -> Case[GenerateTextResponse]:
    return Case.branch_result(TRUNCATED, value, failure)

setattr(generate_text_truncated, '__module__', "dex_connectors.superdurable_llm.llmmock")

def generate_text_blocked(value: GenerateTextResponse, failure: Failure | None = None) -> Case[GenerateTextResponse]:
    return Case.branch_result(BLOCKED, value, failure)

setattr(generate_text_blocked, '__module__', "dex_connectors.superdurable_llm.llmmock")

def generate_text_provider_rejected(value: GenerateTextResponse, failure: Failure | None = None) -> Case[GenerateTextResponse]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(generate_text_provider_rejected, '__module__', "dex_connectors.superdurable_llm.llmmock")

def generate_text_invalid_response(value: GenerateTextResponse, failure: Failure | None = None) -> Case[GenerateTextResponse]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(generate_text_invalid_response, '__module__', "dex_connectors.superdurable_llm.llmmock")

def generate_text_defect(value: GenerateTextResponse, failure: Failure | None = None) -> Case[GenerateTextResponse]:
    return Case.branch_result(DEFECT, value, failure)

setattr(generate_text_defect, '__module__', "dex_connectors.superdurable_llm.llmmock")

def generate_text_retry(failure: Failure, after_seconds: float = 0) -> Case[GenerateTextResponse]:
    return Case.retry(failure, after_seconds)

setattr(generate_text_retry, '__module__', "dex_connectors.superdurable_llm.llmmock")

def generate_text_short_summary() -> Case[GenerateTextResponse]:
    return Case.branch_result(GENERATED, GenerateTextResponse(text='The connector shipped on time.', requested_model='mock-model', served_model='mock-model-2026-01', response_id='resp-mock-1', finish_reason='stop', usage=textgenUsage(input_tokens=30, output_tokens=7, total_tokens=37)))

setattr(generate_text_short_summary, '__module__', "dex_connectors.superdurable_llm.llmmock")

def generate_text_structured_answer() -> Case[GenerateTextResponse]:
    return Case.branch_result(GENERATED, GenerateTextResponse(text='{"status":"shipped","confidence":0.9}', requested_model='mock-model', served_model='mock-model', response_id='resp-mock-2', finish_reason='stop', usage=textgenUsage(input_tokens=48, output_tokens=12, total_tokens=60)))

setattr(generate_text_structured_answer, '__module__', "dex_connectors.superdurable_llm.llmmock")

def generate_text_output_token_limit() -> Case[GenerateTextResponse]:
    return Case.branch_result(TRUNCATED, GenerateTextResponse(text='The connector shipped on', requested_model='mock-model', served_model='mock-model', response_id='resp-mock-3', finish_reason='length', provider_finish_reason='max_tokens', usage=textgenUsage(input_tokens=30, output_tokens=5, total_tokens=35)), Failure(FailureKind.RESPONSE_TOO_LARGE, CONNECTOR_ID, "generateText", "The model stopped at the output token limit."))

setattr(generate_text_output_token_limit, '__module__', "dex_connectors.superdurable_llm.llmmock")

def generate_text_content_policy() -> Case[GenerateTextResponse]:
    return Case.branch_result(BLOCKED, GenerateTextResponse(text='', requested_model='mock-model', finish_reason='contentPolicy', provider_finish_reason='content_filter', usage=textgenUsage(input_tokens=30, output_tokens=0, total_tokens=30)), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "generateText", "The provider blocked the response for a content policy."))

setattr(generate_text_content_policy, '__module__', "dex_connectors.superdurable_llm.llmmock")

def generate_text_invalid_key() -> Case[GenerateTextResponse]:
    return Case.branch_result(PROVIDER_REJECTED, GenerateTextResponse(usage=textgenUsage()), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "generateText", "The provider rejected the API key."))

setattr(generate_text_invalid_key, '__module__', "dex_connectors.superdurable_llm.llmmock")

def generate_text_quota_exhausted() -> Case[GenerateTextResponse]:
    return Case.branch_result(PROVIDER_REJECTED, GenerateTextResponse(usage=textgenUsage()), Failure(FailureKind.QUOTA_EXHAUSTED, CONNECTOR_ID, "generateText", "The provider account has exhausted its quota."))

setattr(generate_text_quota_exhausted, '__module__', "dex_connectors.superdurable_llm.llmmock")

def generate_text_schema_mismatch() -> Case[GenerateTextResponse]:
    return Case.branch_result(INVALID_RESPONSE, GenerateTextResponse(text='', requested_model='mock-model', finish_reason='stop', usage=textgenUsage(input_tokens=48, output_tokens=9, total_tokens=57)), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "generateText", "The structured output does not match the schema at /status."))

setattr(generate_text_schema_mismatch, '__module__', "dex_connectors.superdurable_llm.llmmock")

def generate_text_unsupported_field() -> Case[GenerateTextResponse]:
    return Case.branch_result(DEFECT, GenerateTextResponse(usage=textgenUsage()), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "generateText", "The model does not accept a temperature."))

setattr(generate_text_unsupported_field, '__module__', "dex_connectors.superdurable_llm.llmmock")

def generate_text_rate_limited() -> Case[GenerateTextResponse]:
    return Case.retry(Failure(FailureKind.RATE_LIMIT, CONNECTOR_ID, "generateText", "The provider rate limit was reached."), 2)

setattr(generate_text_rate_limited, '__module__', "dex_connectors.superdurable_llm.llmmock")


class Mock:

    def __init__(self, connection_name: str = "llm") -> None:
        self.generate_text_script: Query[GenerateTextRequest, GenerateTextResponse] = Query(GENERATE_TEXT_DEFINITION)
        self.generate_text_script.default(generate_text_short_summary())
        self._connection = new_connection_with_operations(
            generate_text=self.generate_text_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.superdurable_llm.llmmock")

__all__ = [
    "Mock",
    "generate_text_generated",
    "generate_text_truncated",
    "generate_text_blocked",
    "generate_text_provider_rejected",
    "generate_text_invalid_response",
    "generate_text_defect",
    "generate_text_retry",
    "generate_text_short_summary",
    "generate_text_structured_answer",
    "generate_text_output_token_limit",
    "generate_text_content_policy",
    "generate_text_invalid_key",
    "generate_text_quota_exhausted",
    "generate_text_schema_mismatch",
    "generate_text_unsupported_field",
    "generate_text_rate_limited",
]
