# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Content, GenerateContentRequest, GenerateContentResponse, Part, Usage
from .definitions import CONNECTOR_ID, PROVIDER, BLOCKED, DEFECT, GENERATED, GENERATE_CONTENT_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED, TRUNCATED

def generate_content_generated(value: GenerateContentResponse, failure: Failure | None = None) -> Case[GenerateContentResponse]:
    return Case.branch_result(GENERATED, value, failure)

setattr(generate_content_generated, '__module__', "dex_connectors.google_gemini.geminimock")

def generate_content_truncated(value: GenerateContentResponse, failure: Failure | None = None) -> Case[GenerateContentResponse]:
    return Case.branch_result(TRUNCATED, value, failure)

setattr(generate_content_truncated, '__module__', "dex_connectors.google_gemini.geminimock")

def generate_content_blocked(value: GenerateContentResponse, failure: Failure | None = None) -> Case[GenerateContentResponse]:
    return Case.branch_result(BLOCKED, value, failure)

setattr(generate_content_blocked, '__module__', "dex_connectors.google_gemini.geminimock")

def generate_content_provider_rejected(value: GenerateContentResponse, failure: Failure | None = None) -> Case[GenerateContentResponse]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(generate_content_provider_rejected, '__module__', "dex_connectors.google_gemini.geminimock")

def generate_content_invalid_response(value: GenerateContentResponse, failure: Failure | None = None) -> Case[GenerateContentResponse]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(generate_content_invalid_response, '__module__', "dex_connectors.google_gemini.geminimock")

def generate_content_defect(value: GenerateContentResponse, failure: Failure | None = None) -> Case[GenerateContentResponse]:
    return Case.branch_result(DEFECT, value, failure)

setattr(generate_content_defect, '__module__', "dex_connectors.google_gemini.geminimock")

def generate_content_retry(failure: Failure, after_seconds: float = 0) -> Case[GenerateContentResponse]:
    return Case.retry(failure, after_seconds)

setattr(generate_content_retry, '__module__', "dex_connectors.google_gemini.geminimock")

def generate_content_launch_summary() -> Case[GenerateContentResponse]:
    return Case.branch_result(GENERATED, GenerateContentResponse(response_id='resp-launch-summary', model_version='gemini-3.5-flash-lite', text='{"headline":"Connector ships","summary":"It works. Teams can use typed steps.","keyPoints":["typed","durable"]}', finish_reason='STOP', usage=Usage(prompt_tokens=30, candidate_tokens=12, total_tokens=42)))

setattr(generate_content_launch_summary, '__module__', "dex_connectors.google_gemini.geminimock")

def generate_content_output_limit_reached() -> Case[GenerateContentResponse]:
    return Case.branch_result(TRUNCATED, GenerateContentResponse(response_id='resp-truncated', model_version='gemini-3.5-flash-lite', text='The', finish_reason='MAX_TOKENS', usage=Usage(prompt_tokens=30, candidate_tokens=1, total_tokens=31)), Failure(FailureKind.RESPONSE_TOO_LARGE, CONNECTOR_ID, "generateContent", "Gemini stopped at the output token limit."))

setattr(generate_content_output_limit_reached, '__module__', "dex_connectors.google_gemini.geminimock")

def generate_content_unsafe_prompt_blocked() -> Case[GenerateContentResponse]:
    return Case.branch_result(BLOCKED, GenerateContentResponse(response_id='resp-blocked', model_version='gemini-3.5-flash-lite', text='', block_reason='SAFETY', usage=Usage(prompt_tokens=30, total_tokens=30)), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "generateContent", "Gemini blocked the prompt for safety."))

setattr(generate_content_unsafe_prompt_blocked, '__module__', "dex_connectors.google_gemini.geminimock")

def generate_content_model_access_denied() -> Case[GenerateContentResponse]:
    return Case.branch_result(PROVIDER_REJECTED, GenerateContentResponse(usage=Usage()), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "generateContent", "Gemini denied access to the requested model."))

setattr(generate_content_model_access_denied, '__module__', "dex_connectors.google_gemini.geminimock")


class Mock:

    def __init__(self, connection_name: str = "gemini") -> None:
        self.generate_content_script: Query[GenerateContentRequest, GenerateContentResponse] = Query(GENERATE_CONTENT_DEFINITION)
        self.generate_content_script.default(generate_content_launch_summary())
        self._connection = new_connection_with_operations(
            generate_content=self.generate_content_script,
            reference=ConnectionRef(PROVIDER, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.google_gemini.geminimock")

__all__ = [
    "Mock",
    "generate_content_generated",
    "generate_content_truncated",
    "generate_content_blocked",
    "generate_content_provider_rejected",
    "generate_content_invalid_response",
    "generate_content_defect",
    "generate_content_retry",
    "generate_content_launch_summary",
    "generate_content_output_limit_reached",
    "generate_content_unsafe_prompt_blocked",
    "generate_content_model_access_denied",
]
