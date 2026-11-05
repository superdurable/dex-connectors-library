# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider specific Chat Completions profiles for text generation."""

from collections.abc import AsyncIterator, Callable
from dataclasses import replace

from dex_connectors.sdk import FailureKind
from dex_connectors.sdk.textgen import (
    DecodedResponse,
    ErrorOutcome,
    ErrorRule,
    FinishReason,
    ReasoningEffort,
    StructuredOutputMode,
    StructuredOutputRules,
    WireFormat,
    blocked_outcome,
    provider_rejected_outcome,
    quota_exhausted_outcome,
    retry_outcome,
    temperature_not_accepted,
    temperature_range,
)
from dex_connectors.sdk.textgen.openaichat import (
    InstructionsRole,
    MaxTokensField,
    ModelRule,
    Profile,
    StreamingPolicy,
    new_wire_format,
)


def _efforts(*values: str) -> dict[ReasoningEffort, str]:
    return {ReasoningEffort(value): value for value in values}


def _rule(token: str, outcome: ErrorOutcome, status: int = 0) -> ErrorRule:
    return ErrorRule(status, token, outcome)


_SCHEMA = StructuredOutputRules(StructuredOutputMode.JSON_SCHEMA)
_JSON_OBJECT = StructuredOutputRules(StructuredOutputMode.JSON_OBJECT_WITH_INSTRUCTION)
_STREAM = StreamingPolicy.ALWAYS
_COMPLETION_TOKENS = MaxTokensField.MAX_COMPLETION_TOKENS


def new_chat_wire_format(provider: str) -> WireFormat:
    """Build the Go-equivalent profile for a Chat Completions provider."""
    profiles = {
        "openai-compatible": Profile(
            provider,
            temperature=temperature_range(0, 2),
            structured_output=_JSON_OBJECT,
            streaming=_STREAM,
        ),
        "qwen": Profile(
            provider,
            temperature=temperature_range(0, 2 - 2**-52),
            structured_output=_JSON_OBJECT,
            streaming=_STREAM,
            should_request_stream_usage=True,
            error_rules=(
                _rule("data_inspection_failed", blocked_outcome()),
                _rule("DataInspectionFailed", blocked_outcome()),
                _rule("Arrearage", quota_exhausted_outcome(), 400),
                _rule("AllocationQuota.FreeTierOnly", quota_exhausted_outcome(), 403),
                *tuple(
                    _rule(token, quota_exhausted_outcome(), 429)
                    for token in (
                        "CommodityNotPurchased",
                        "PrepaidBillOverdue",
                        "PostpaidBillOverdue",
                        "BudgetLimitExceeded",
                    )
                ),
            ),
            model_rules=(
                ModelRule(
                    model_id_pattern=r"^qwen3\.8-(?:max|max-0902|flash)$",
                    reasoning_efforts=_efforts("none", "low", "medium", "xhigh"),
                    max_tokens_field=_COMPLETION_TOKENS,
                ),
                ModelRule(
                    model_id_pattern=r"^qwen3\.8-27b$",
                    reasoning_efforts=_efforts("none", "low", "medium", "xhigh"),
                ),
                ModelRule(
                    model_id_pattern=r"^qwen3\.8-2\.4t-a95b$",
                    reasoning_efforts=_efforts("low", "medium", "xhigh"),
                ),
                ModelRule(
                    model_id_pattern=r"^qwen3\.[5-9]-(?:plus|flash)(?:-\d{4}-\d{2}-\d{2})?$",
                    max_tokens_field=_COMPLETION_TOKENS,
                ),
                ModelRule(
                    model_id_pattern=r"^qwen3\.[7-9]-max(?:-preview|-\d{4}-\d{2}-\d{2})?$",
                    max_tokens_field=_COMPLETION_TOKENS,
                ),
            ),
        ),
        "deepseek": Profile(
            provider,
            temperature=temperature_not_accepted(),
            structured_output=_JSON_OBJECT,
            reasoning_efforts=_efforts("none", "low", "high", "max"),
            streaming=_STREAM,
            request_id_headers=("x-ds-trace-id",),
            stall_timeout_seconds=300,
        ),
        "meta": Profile(
            provider,
            chat_completions_path="/v1/chat/completions",
            instructions_role=InstructionsRole.DEVELOPER,
            max_tokens_field=_COMPLETION_TOKENS,
            temperature=temperature_range(0, 2),
            reasoning_efforts=_efforts("minimal", "low", "medium", "high", "xhigh"),
            structured_output=_SCHEMA,
            should_send_strict_json_schema=True,
            streaming=_STREAM,
            should_request_stream_usage=True,
            error_rules=(
                _rule("content_policy_violation", blocked_outcome(), 400),
                _rule("", provider_rejected_outcome(FailureKind.PROVIDER_REJECTION), 501),
                *tuple(
                    _rule(token, retry_outcome(FailureKind.AVAILABILITY))
                    for token in (
                        "server_error",
                        "server_shutting_down",
                        "service_overloaded",
                        "backend_unavailable",
                    )
                ),
                _rule("rate_limit_error", retry_outcome(FailureKind.RATE_LIMIT)),
                _rule("rate_limit_exceeded", retry_outcome(FailureKind.RATE_LIMIT)),
            ),
            rate_limit_headers=(
                "x-ratelimit-limit-requests",
                "x-ratelimit-remaining-requests",
                "x-ratelimit-limit-tokens",
                "x-ratelimit-remaining-tokens",
            ),
            model_rules=(
                ModelRule(
                    model_id_pattern=r"^muse-spark-1\.3$",
                    reasoning_efforts=_efforts("minimal", "low", "medium", "high", "xhigh", "max"),
                ),
            ),
        ),
        "mistral": Profile(
            provider,
            temperature=temperature_range(0, 1.5),
            structured_output=_SCHEMA,
            should_send_strict_json_schema=True,
            streaming=_STREAM,
            allowed_request_fields=(
                "model",
                "messages",
                "max_tokens",
                "temperature",
                "reasoning_effort",
                "response_format",
                "stream",
            ),
            finish_reasons={"model_length": FinishReason.LENGTH},
            error_token_pointers=("/type", "/code"),
            request_id_headers=("mistral-correlation-id",),
            rate_limit_headers=("x-ratelimit-remaining",),
            model_rules=(
                ModelRule(
                    model_id_pattern=r"^mistral-small-(2603|latest)$",
                    reasoning_efforts=_efforts("none", "high"),
                ),
                ModelRule(
                    model_id_pattern=r"^mistral-medium-(3|3-5|latest)$",
                    reasoning_efforts=_efforts("none", "high"),
                ),
            ),
        ),
        "kimi": Profile(
            provider,
            max_tokens_field=_COMPLETION_TOKENS,
            temperature=temperature_not_accepted(),
            structured_output=StructuredOutputRules(
                StructuredOutputMode.JSON_SCHEMA,
                ("minimum", "maximum", "minLength", "maxLength", "minItems", "maxItems", "format"),
                ("title",),
            ),
            should_send_strict_json_schema=True,
            streaming=_STREAM,
            should_request_stream_usage=True,
            error_rules=(
                _rule("", provider_rejected_outcome(FailureKind.PROVIDER_REJECTION), 501),
                _rule("content_filter", blocked_outcome()),
                _rule("exceeded_current_quota_error", quota_exhausted_outcome()),
                *tuple(
                    _rule(token, retry_outcome(FailureKind.AVAILABILITY))
                    for token in (
                        "engine_overloaded_error",
                        "server_error",
                        "unexpected_output",
                        "server_unavailable",
                    )
                ),
                _rule("rate_limit_reached_error", retry_outcome(FailureKind.RATE_LIMIT)),
            ),
            model_rules=(
                ModelRule(model_id_prefix="kimi-k3", reasoning_efforts=_efforts("low", "high", "max")),
            ),
        ),
        "xai": Profile(
            provider,
            max_tokens_field=_COMPLETION_TOKENS,
            reasoning_efforts=_efforts("low", "medium", "high", "xhigh"),
            temperature=temperature_range(0, 2),
            structured_output=_SCHEMA,
            should_send_strict_json_schema=True,
            streaming=_STREAM,
            should_request_stream_usage=True,
            finish_reasons={"end_turn": FinishReason.STOP},
            error_token_pointers=("/code",),
            model_rules=(
                ModelRule(
                    model_id_pattern=r"^grok-4\.3(-latest)?$",
                    reasoning_efforts=_efforts("none", "low", "medium", "high", "xhigh"),
                ),
                ModelRule(
                    model_id_pattern=r"^(grok-4\.5|grok-4\.5-latest|grok-build-latest)$",
                    reasoning_efforts=_efforts("low", "medium", "high"),
                ),
                ModelRule(model_id_pattern=r"^grok-4\.20(-.+)?$", reasoning_efforts={}),
                ModelRule(
                    model_id_pattern=r"^(grok-build-0\.1|grok-code-fast|grok-code-fast-1|grok-code-fast-1-0825)$",
                    reasoning_efforts={},
                ),
            ),
        ),
    }
    if provider not in profiles:
        raise ValueError("provider does not use Chat Completions")
    wire = new_wire_format(profiles[provider])
    if provider != "xai":
        return wire

    def count_reasoning(response: DecodedResponse) -> DecodedResponse:
        usage = response.usage
        if usage.reasoning_tokens > 0 and usage.total_tokens != usage.input_tokens + usage.output_tokens:
            return replace(
                response, usage=replace(usage, output_tokens=usage.output_tokens + usage.reasoning_tokens)
            )
        return response

    original_response = wire.decode_response
    original_stream = wire.decode_stream

    def decode_response(body: bytes) -> DecodedResponse:
        assert original_response is not None
        return count_reasoning(original_response(body))

    async def decode_stream(
        chunks: AsyncIterator[bytes], write_text_delta: Callable[[str], None]
    ) -> DecodedResponse:
        assert original_stream is not None
        return count_reasoning(await original_stream(chunks, write_text_delta))

    return replace(wire, decode_response=decode_response, decode_stream=decode_stream)
