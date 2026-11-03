# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Portable text generation values and provider dialect contracts."""

from __future__ import annotations

import re
from collections.abc import AsyncIterator, Awaitable, Callable
from dataclasses import dataclass, field
from enum import StrEnum

from ..errors import FailureKind

TEXT_GENERATION_OPERATION_ID = "generateText"  # Stable provider-neutral operation.
GENERATED_BRANCH_ID = "generated"  # Required normal completion with text.
TRUNCATED_BRANCH_ID = "truncated"  # Optional partial output at a limit.
BLOCKED_BRANCH_ID = "blocked"  # Optional policy stop or refusal.
PROVIDER_REJECTED_BRANCH_ID = "providerRejected"  # Optional conclusive rejection.
INVALID_RESPONSE_BRANCH_ID = "invalidResponse"  # Optional unusable provider response.
DEFECT_BRANCH_ID = "defect"  # Optional local input or configuration error.


class MessageRole(StrEnum):
    """Conversation author; only user and assistant are portable."""

    USER = "user"  # Human or application input.
    ASSISTANT = "assistant"  # Prior model output.


@dataclass(frozen=True, slots=True)
class Message:
    """One text-only turn; ``role`` is user or assistant and ``text`` is nonempty."""

    role: MessageRole
    text: str


@dataclass(frozen=True, slots=True)
class StructuredOutput:
    """Named JSON object schema checked locally after generation.

    ``name`` is an ASCII identifier; ``schema`` uses the portable subset;
    optional ``description`` supplies provider-facing guidance.
    """

    name: str
    schema: dict[str, object]
    description: str = field(default="", metadata={"dex_json_omit_default": True})


class ReasoningEffort(StrEnum):
    """Portable effort choices; a model may accept only a subset."""

    NONE = "none"  # No model reasoning budget.
    MINIMAL = "minimal"  # Minimal model reasoning budget.
    LOW = "low"  # Low model reasoning budget.
    MEDIUM = "medium"  # Medium model reasoning budget.
    HIGH = "high"  # High model reasoning budget.
    EXTRA_HIGH = "xhigh"  # Extra-high model reasoning budget.
    MAX = "max"  # Maximum model reasoning budget.


@dataclass(frozen=True, slots=True)
class TextGenerationRequest:
    """Provider-neutral input; optional values are rejected unless supported.

    ``messages`` is ordered and nonempty. ``model`` overrides the connection
    default; ``instructions`` precedes messages. ``structured_output`` is a
    schema to validate locally. Zero ``max_output_tokens`` omits the limit;
    ``temperature=None`` and empty ``reasoning_effort`` omit sampling options.
    """

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

    STOP = "stop"  # Normal completion.
    LENGTH = "length"  # Output limit reached.
    CONTENT_POLICY = "contentPolicy"  # Provider policy stop.
    REFUSAL = "refusal"  # Model refusal.


@dataclass(frozen=True, slots=True)
class Usage:
    """Provider-reported token counts; absent counts are zero.

    ``input_tokens`` includes ``cached_input_tokens``; ``output_tokens``
    includes ``reasoning_tokens``. ``total_tokens`` defaults to input plus
    output when the provider omits it. Negative values make the response
    invalid.
    """

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
    """Provider-neutral result; only generated and truncated carry ``text``.

    ``requested_model`` is the validated selection, ``served_model`` and
    ``response_id`` identify the provider output, ``finish_reason`` is portable,
    ``provider_finish_reason`` is the safe provider token, and ``usage`` holds
    provider token counts. Identifiers remain available on a usage error.
    """

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

    BODY = "body"  # Printable ASCII model ID for a JSON body.
    PATH_SEGMENT = "pathSegment"  # Restricted model ID for a URL segment.

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
    """Five false-by-default predicates for optional request fields.

    The fields ``supports_instructions``, ``supports_structured_output``,
    ``supports_max_output_tokens``, ``supports_temperature``, and
    ``supports_reasoning_effort`` must be true before those values are sent.
    """

    supports_instructions: bool = False
    supports_structured_output: bool = False
    supports_max_output_tokens: bool = False
    supports_temperature: bool = False
    supports_reasoning_effort: bool = False


@dataclass(frozen=True, slots=True)
class CredentialHeader:
    """``name`` identifies the credential header; ``prefix`` precedes the secret."""

    name: str
    prefix: str = ""


@dataclass(frozen=True, slots=True)
class TemperaturePolicy:
    """Sampling policy with ``accepted`` and optional inclusive bounds.

    Both ``minimum`` and ``maximum`` must be set for a range. Their values may
    be infinite but not NaN; request temperatures must be finite and nonnegative.
    """

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

    NONE = ""  # Reject structured-output requests.
    JSON_SCHEMA = "jsonSchema"  # Send provider-enforced JSON Schema.
    JSON_OBJECT_WITH_INSTRUCTION = "jsonObjectWithInstruction"  # Add schema text to instructions.


@dataclass(frozen=True, slots=True)
class StructuredOutputRules:
    """Provider schema transforms; local validation retains the original.

    ``mode`` selects the request form. ``keywords_moved_to_description``
    describes unsupported constraints; ``keywords_removed`` drops listed
    provider-facing keywords without changing the local schema.
    """

    mode: StructuredOutputMode = StructuredOutputMode.NONE
    keywords_moved_to_description: tuple[str, ...] = ()
    keywords_removed: tuple[str, ...] = ()

    def validate(self) -> None:
        """Reject unsupported modes and schema transformation keywords."""
        movable = {"minimum", "maximum", "minLength", "maxLength", "minItems", "maxItems", "format"}
        if not isinstance(self.mode, StructuredOutputMode):
            raise ValueError("structured output mode is invalid")
        if any(keyword not in movable for keyword in self.keywords_moved_to_description):
            raise ValueError("schema keyword cannot be moved to a description")
        if any(
            keyword not in movable | {"title", "additionalProperties"} for keyword in self.keywords_removed
        ):
            raise ValueError("schema keyword cannot be removed")


@dataclass(frozen=True, slots=True)
class ModelRequestRules:
    """One model's ``temperature``, ``reasoning_efforts``, and ``structured_output`` rules."""

    temperature: TemperaturePolicy = field(default_factory=temperature_accepted)
    reasoning_efforts: dict[ReasoningEffort, str] = field(default_factory=dict)
    structured_output: StructuredOutputRules = field(default_factory=StructuredOutputRules)


@dataclass(frozen=True, slots=True)
class EncodeRequestInput:
    """Validated ``request`` and selected model ``rules`` passed to the encoder."""

    request: TextGenerationRequest
    rules: ModelRequestRules


@dataclass(frozen=True, slots=True)
class EncodedRequest:
    """Credential-free provider request.

    ``path`` joins the HTTPS base URL; ``body`` contains bytes; ``headers``
    contains safe fixed headers; ``is_streaming`` requests SSE when true.
    """

    path: str
    body: bytes
    headers: dict[str, str] = field(default_factory=dict)
    is_streaming: bool = False


@dataclass(frozen=True, slots=True)
class ResponsePart:
    """One ``text`` fragment; ``is_reasoning`` excludes it from result text."""

    text: str
    is_reasoning: bool = False


@dataclass(frozen=True, slots=True)
class DecodedResponse:
    """Dialect-decoded provider response before outcome classification.

    ``parts`` keeps output order. ``served_model`` and ``response_id`` are
    optional identifiers; ``provider_finish_reason`` maps to a portable finish;
    ``is_refusal`` overrides that mapping; ``usage`` holds token counts.
    """

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
    """Selected ``disposition`` (retry or branch) and ``kind`` of failure."""

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
    """Ordered status and safe token match selecting an ``outcome``.

    ``status_code`` zero matches any status and reported 2xx errors;
    ``error_token`` empty matches any token. The first matching rule wins.
    Unmatched 2xx errors and 3xx select invalidResponse; 408, 429, and 5xx
    except 501 retry; 401, 403, 404, 409, 402, and other statuses reject.
    """

    status_code: int
    error_token: str
    outcome: ErrorOutcome


# A decoder consumes raw bytes and writes each non-reasoning text delta in order.
StreamDecoder = Callable[[AsyncIterator[bytes], Callable[[str], None]], Awaitable[DecodedResponse]]


@dataclass(frozen=True, slots=True)
class WireFormat:
    """Declarative provider adapter with concurrent-safe callbacks.

    ``provider_name`` labels failures and receipts; ``model_id_rule`` validates
    IDs; ``features`` declares optional fields; ``credential_header`` is the
    sole secret destination. ``rules_for_model`` selects model rules and
    ``encode_request`` builds credential-free bytes. ``decode_response`` reads
    complete bodies; optional ``decode_stream`` reads raw byte chunks and writes
    text deltas. Both may raise ProviderReportedError, EOFError, HTTPError, or
    ValueError to signal reported, interrupted, transport, or malformed output.
    ``finish_reasons`` maps safe tokens; ``error_rules`` are ordered;
    ``error_token_pointers`` locate machine tokens. Optional
    ``read_error_retry_delay`` overrides Retry-After when positive.
    ``request_id_headers`` and ``rate_limit_headers`` select receipt metadata.
    ``stall_timeout_seconds`` is a between-byte limit; zero disables it.
    """

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
