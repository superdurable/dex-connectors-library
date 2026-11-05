# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed Gemini scripts and mock checks for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..client import (
    BLOCKED,
    CONNECTOR_ID,
    DEFAULT_MODEL,
    GENERATE_CONTENT_DEFINITION,
    GENERATE_CONTENT_OPERATION_ID,
    GENERATED,
    PROVIDER,
    PROVIDER_REJECTED,
    TRUNCATED,
    encode_generate_content_request,
)
from ..connection import Connection, new_connection_with_operations
from ..models import GenerateContentRequest, GenerateContentResponse, Usage

_MODEL_VERSION = "gemini-3.5-flash-lite"


def generate_content_generated(
    value: GenerateContentResponse, failure: Failure | None = None
) -> Case[GenerateContentResponse]:
    """Script the generated route; validate the answer with validate_generate_content_mock_answer."""
    return Case.branch_result(GENERATED, value, failure)


def generate_content_launch_summary() -> Case[GenerateContentResponse]:
    """Return the manifest's default launchSummary answer."""
    return generate_content_generated(
        GenerateContentResponse(
            "resp-launch-summary",
            _MODEL_VERSION,
            '{"headline":"Connector ships","summary":"It works. Teams can use typed steps.",'
            '"keyPoints":["typed","durable"]}',
            "STOP",
            usage=Usage(30, 12, 0, 0, 42),
        )
    )


def generate_content_output_limit_reached() -> Case[GenerateContentResponse]:
    """Return the manifest's outputLimitReached answer."""
    return Case.branch_result(
        TRUNCATED,
        GenerateContentResponse(
            "resp-truncated", _MODEL_VERSION, "The", "MAX_TOKENS", usage=Usage(30, 1, 0, 0, 31)
        ),
        _failure(FailureKind.RESPONSE_TOO_LARGE, "Gemini stopped at the output token limit."),
    )


def generate_content_unsafe_prompt_blocked() -> Case[GenerateContentResponse]:
    """Return the manifest's unsafePromptBlocked answer."""
    return Case.branch_result(
        BLOCKED,
        GenerateContentResponse(
            "resp-blocked", _MODEL_VERSION, "", block_reason="SAFETY", usage=Usage(30, 0, 0, 0, 30)
        ),
        _failure(FailureKind.PROVIDER_REJECTION, "Gemini blocked the prompt for safety."),
    )


def generate_content_model_access_denied() -> Case[GenerateContentResponse]:
    """Return the manifest's modelAccessDenied answer."""
    return Case.branch_result(
        PROVIDER_REJECTED,
        GenerateContentResponse(),
        _failure(FailureKind.AUTHORIZATION, "Gemini denied access to the requested model."),
    )


def validate_generate_content_mock_request(request: GenerateContentRequest) -> Failure | None:
    """Apply the real operation's provider-independent request checks to a scripted call.

    A blank model uses the manifest default. Provider-specific schema acceptance still belongs to Gemini.
    """
    encoded = encode_generate_content_request(request, DEFAULT_MODEL)
    return encoded if isinstance(encoded, Failure) else None


def validate_generate_content_mock_answer(
    request: GenerateContentRequest, answer: GenerateContentResponse
) -> str:
    """Return why a scripted generated answer is unrealistic, or an empty string.

    A generated answer needs nonempty text and, when a finish reason is set, STOP.
    """
    if answer.finish_reason and answer.finish_reason != "STOP":
        return "a generated Gemini answer needs STOP when a finish reason is set"
    if not answer.text:
        return "a generated Gemini answer needs nonempty text"
    return ""


class Mock:
    """Script Gemini generateContent, defaulting to the manifest's launchSummary answer."""

    def __init__(self, connection_name: str = "gemini") -> None:
        self.generate_content_script: Query[GenerateContentRequest, GenerateContentResponse] = Query(
            GENERATE_CONTENT_DEFINITION
        )
        self.generate_content_script.default(generate_content_launch_summary())
        self._connection = new_connection_with_operations(
            self.generate_content_script, ConnectionRef(PROVIDER, connection_name)
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, GENERATE_CONTENT_OPERATION_ID, message)


__all__ = [
    "Mock",
    "generate_content_generated",
    "generate_content_launch_summary",
    "generate_content_output_limit_reached",
    "generate_content_unsafe_prompt_blocked",
    "generate_content_model_access_denied",
    "validate_generate_content_mock_request",
    "validate_generate_content_mock_answer",
]
