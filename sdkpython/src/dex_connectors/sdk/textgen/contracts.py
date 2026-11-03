# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Portable text generation values and provider dialect contracts."""

from __future__ import annotations

import re
from collections.abc import AsyncIterator, Awaitable, Callable
from dataclasses import dataclass, field
from enum import StrEnum

from ..errors import FailureKind

TEXT_GENERATION_OPERATION_ID = "generateText"
GENERATED_BRANCH_ID = "generated"
TRUNCATED_BRANCH_ID = "truncated"
BLOCKED_BRANCH_ID = "blocked"
PROVIDER_REJECTED_BRANCH_ID = "providerRejected"
INVALID_RESPONSE_BRANCH_ID = "invalidResponse"
DEFECT_BRANCH_ID = "defect"


class MessageRole(StrEnum):
    """Conversation author; only user and assistant are portable."""

    USER = "user"
    ASSISTANT = "assistant"


@dataclass(frozen=True, slots=True)
class Message:
    """One text-only conversation turn."""

    role: MessageRole
    text: str


@dataclass(frozen=True, slots=True)
class StructuredOutput:
    """Named JSON object schema checked locally after generation."""

    name: str
    schema: dict[str, object]
    description: str = field(default="", metadata={"dex_json_omit_default": True})


class ReasoningEffort(StrEnum):
    """Portable effort choices; a model may accept only a subset."""

    NONE = "none"
    MINIMAL = "minimal"
    LOW = "low"
    MEDIUM = "medium"
    HIGH = "high"
    EXTRA_HIGH = "xhigh"
    MAX = "max"


@dataclass(frozen=True, slots=True)
class TextGenerationRequest:
    """Provider-neutral input; optional values are rejected unless supported."""

    messages: tuple[Message, ...]
    model: str = field(default="", metadata={"dex_json_omit_default": True})
    instructions: str = field(default="", metadata={"dex_json_omit_default": True})
    structured_output: StructuredOutput | None = field(
        default=None, metadata={"dex_json_name": "structuredOutput", "dex_json_omit_default": True}
    )
    max_output_tokens: int = field(
        default=0, metadata={"dex_json_name": "maxOutputTokens", "dex_json_omit_default": True}
    )
    temperature: float | None = field(default=None, metadata={"dex_json_omit_default": True})
    reasoning_effort: ReasoningEffort | str = field(
        default="", metadata={"dex_json_name": "reasoningEffort", "dex_json_omit_default": True}
    )


class FinishReason(StrEnum):
    """Portable reason generation stopped."""

    STOP = "stop"
    LENGTH = "length"
    CONTENT_POLICY = "contentPolicy"
    REFUSAL = "refusal"


@dataclass(frozen=True, slots=True)
class Usage:
    """Provider-reported token counts; absent counts are zero."""

    input_tokens: int = field(default=0, metadata={"dex_json_name": "inputTokens"})
    cached_input_tokens: int = field(
        default=0, metadata={"dex_json_name": "cachedInputTokens", "dex_json_omit_default": True}
    )
    output_tokens: int = field(default=0, metadata={"dex_json_name": "outputTokens"})
    reasoning_tokens: int = field(
        default=0, metadata={"dex_json_name": "reasoningTokens", "dex_json_omit_default": True}
    )
    total_tokens: int = field(default=0, metadata={"dex_json_name": "totalTokens"})


@dataclass(frozen=True, slots=True)
class TextGenerationResponse:
    """Provider-neutral result; only generated and truncated carry text."""

    text: str = ""
    requested_model: str = field(
        default="", metadata={"dex_json_name": "requestedModel", "dex_json_omit_default": True}
    )
    served_model: str = field(
        default="", metadata={"dex_json_name": "servedModel", "dex_json_omit_default": True}
    )
    response_id: str = field(
        default="", metadata={"dex_json_name": "responseId", "dex_json_omit_default": True}
    )
    finish_reason: FinishReason | str = field(
        default="", metadata={"dex_json_name": "finishReason", "dex_json_omit_default": True}
    )
    provider_finish_reason: str = field(
        default="", metadata={"dex_json_name": "providerFinishReason", "dex_json_omit_default": True}
    )
    usage: Usage = field(default_factory=Usage)


class ModelIDRule(StrEnum):
    """Validate a model for a JSON body or a URL path segment."""

    BODY = "body"
    PATH_SEGMENT = "pathSegment"

    def validate_model_id(self, value: str) -> str:
        """Return a canonical model ID without exposing invalid input in errors."""
        model = value.strip()
        if self is ModelIDRule.BODY:
            if not 1 <= len(model.encode()) <= 256 or not all(33 <= ord(char) <= 126 for char in model):
                raise ValueError("model ID must be 1 to 256 printable ASCII characters without spaces")
            return model
        if model.startswith("models/"):
            model = model[7:]
        if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]{0,127}", model):
            raise ValueError(
                "model ID must start with a letter or digit and contain at most 128 "
                'letters, digits, ".", "_", or "-"'
            )
        return model


def resolve_model(request_model: str, connection_model: str) -> str:
    """Prefer the trimmed request model over the connection default."""
    return request_model.strip() or connection_model


@dataclass(frozen=True, slots=True)
class RequestFeatures:
    """Optional fields the wire format actually sends."""

    supports_instructions: bool = False
    supports_structured_output: bool = False
    supports_max_output_tokens: bool = False
    supports_temperature: bool = False
    supports_reasoning_effort: bool = False


@dataclass(frozen=True, slots=True)
class CredentialHeader:
    """Header name and prefix used only at dispatch time."""

    name: str
    prefix: str = ""


@dataclass(frozen=True, slots=True)
class TemperaturePolicy:
    """Accepted sampling range; none means any finite nonnegative value."""

    accepted: bool = True
    minimum: float | None = None
    maximum: float | None = None


def temperature_accepted() -> TemperaturePolicy:
    """Accept any finite nonnegative temperature."""
    return TemperaturePolicy()


def temperature_range(minimum: float, maximum: float) -> TemperaturePolicy:
    """Accept the inclusive interval, including a single fixed value."""
    return TemperaturePolicy(True, minimum, maximum)


def temperature_not_accepted() -> TemperaturePolicy:
    """Reject a temperature for fixed-sampling models."""
    return TemperaturePolicy(False)


class StructuredOutputMode(StrEnum):
    """Provider request form for an application schema."""

    NONE = ""
    JSON_SCHEMA = "jsonSchema"
    JSON_OBJECT_WITH_INSTRUCTION = "jsonObjectWithInstruction"


@dataclass(frozen=True, slots=True)
class StructuredOutputRules:
    """Provider schema transforms; local validation retains the original."""

    mode: StructuredOutputMode = StructuredOutputMode.NONE
    keywords_moved_to_description: tuple[str, ...] = ()
    keywords_removed: tuple[str, ...] = ()

    def validate(self) -> None:
        """Reject unsupported modes and schema transformation keywords."""
        movable = {"minimum", "maximum", "minLength", "maxLength", "minItems", "maxItems", "format"}
        if self.mode not in StructuredOutputMode:
            raise ValueError("structured output mode is invalid")
        if any(keyword not in movable for keyword in self.keywords_moved_to_description):
            raise ValueError("schema keyword cannot be moved to a description")
        if any(
            keyword not in movable | {"title", "additionalProperties"} for keyword in self.keywords_removed
        ):
            raise ValueError("schema keyword cannot be removed")


@dataclass(frozen=True, slots=True)
class ModelRequestRules:
    """Sampling, effort, and structured-output rules for one model."""

    temperature: TemperaturePolicy = field(default_factory=temperature_accepted)
    reasoning_efforts: dict[ReasoningEffort, str] = field(default_factory=dict)
    structured_output: StructuredOutputRules = field(default_factory=StructuredOutputRules)


@dataclass(frozen=True, slots=True)
class EncodeRequestInput:
    """Validated request and selected model rules passed to the dialect."""

    request: TextGenerationRequest
    rules: ModelRequestRules


@dataclass(frozen=True, slots=True)
class EncodedRequest:
    """One provider request without credentials."""

    path: str
    body: bytes
    headers: dict[str, str] = field(default_factory=dict)
    is_streaming: bool = False


@dataclass(frozen=True, slots=True)
class ResponsePart:
    """One text or reasoning fragment from the provider."""

    text: str
    is_reasoning: bool = False


@dataclass(frozen=True, slots=True)
class DecodedResponse:
    """Dialect-decoded provider response before outcome classification."""

    parts: tuple[ResponsePart, ...] = ()
    served_model: str = ""
    response_id: str = ""
    provider_finish_reason: str = ""
    is_refusal: bool = False
    usage: Usage = field(default_factory=Usage)


class ProviderReportedError(Exception):
    """Provider error object in a 2xx response; tokens never include prose."""

    def __init__(self, error_tokens: tuple[str, ...] = ()) -> None:
        self.error_tokens = error_tokens
        super().__init__("provider reported an error inside a 2xx response")


@dataclass(frozen=True, slots=True)
class ErrorOutcome:
    """Classification selected by an ordered provider error rule."""

    disposition: str
    kind: FailureKind


def retry_outcome(kind: FailureKind) -> ErrorOutcome:
    """Retry a safe-to-repeat failure with optional provider delay."""
    return ErrorOutcome("retry", kind)


def provider_rejected_outcome(kind: FailureKind) -> ErrorOutcome:
    """Select the conclusive providerRejected branch."""
    return ErrorOutcome("providerRejected", kind)


def quota_exhausted_outcome() -> ErrorOutcome:
    """Select providerRejected for exhausted credit or billing."""
    return provider_rejected_outcome(FailureKind.QUOTA_EXHAUSTED)


def blocked_outcome() -> ErrorOutcome:
    """Select blocked for a provider content-policy error."""
    return ErrorOutcome("blocked", FailureKind.PROVIDER_REJECTION)


def invalid_response_outcome() -> ErrorOutcome:
    """Select invalidResponse for a provider contract violation."""
    return ErrorOutcome("invalidResponse", FailureKind.PROTOCOL)


@dataclass(frozen=True, slots=True)
class ErrorRule:
    """Ordered HTTP status and safe token match; zero status matches 2xx errors."""

    status_code: int
    error_token: str
    outcome: ErrorOutcome


StreamDecoder = Callable[[AsyncIterator[bytes], Callable[[str], None]], Awaitable[DecodedResponse]]


@dataclass(frozen=True, slots=True)
class WireFormat:
    """Declarative provider adapter with pure encoding and decoding callbacks."""

    provider_name: str
    model_id_rule: ModelIDRule
    features: RequestFeatures
    credential_header: CredentialHeader
    rules_for_model: Callable[[str], ModelRequestRules]
    encode_request: Callable[[EncodeRequestInput], EncodedRequest]
    decode_response: Callable[[bytes], DecodedResponse] | None
    decode_stream: StreamDecoder | None = None
    finish_reasons: dict[str, FinishReason] = field(default_factory=dict)
    error_rules: tuple[ErrorRule, ...] = ()
    error_token_pointers: tuple[str, ...] = ()
    read_error_retry_delay: Callable[[bytes], float] | None = None
    request_id_headers: tuple[str, ...] = ()
    rate_limit_headers: tuple[str, ...] = ()
    stall_timeout_seconds: float = 0
