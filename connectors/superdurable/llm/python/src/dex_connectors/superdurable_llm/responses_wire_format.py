# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""OpenAI Responses request and SSE response dialect."""

import re
from collections.abc import AsyncIterator, Callable

from dex_connectors.sdk import FailureKind
from dex_connectors.sdk.providerhttp import read_error_tokens
from dex_connectors.sdk.textgen import (
    CredentialHeader,
    DecodedResponse,
    EncodedRequest,
    EncodeRequestInput,
    ErrorRule,
    FinishReason,
    ModelIDRule,
    ModelRequestRules,
    ProviderReportedError,
    ReasoningEffort,
    RequestFeatures,
    ResponsePart,
    StructuredOutputMode,
    StructuredOutputRules,
    Usage,
    WireFormat,
    blocked_outcome,
    provider_rejected_outcome,
    quota_exhausted_outcome,
    retry_outcome,
    temperature_not_accepted,
    temperature_range,
)

from .wire_common import json_body, object_value, parse_object, sse_events, string_value, token_count

_ALL_EFFORTS = ("none", "minimal", "low", "medium", "high", "xhigh", "max")
_SNAPSHOT = r"(?:-\d{4}-\d{2}-\d{2})?"
_FAMILIES: tuple[tuple[str, tuple[str, ...], bool, bool, bool, bool, bool], ...] = (
    (
        r"^gpt-6-astra" + _SNAPSHOT + "$",
        ("low", "medium", "high", "xhigh", "max"),
        True,
        False,
        False,
        False,
        False,
    ),
    (
        r"^gpt-6-(sol|luna)" + _SNAPSHOT + "$",
        ("none", "low", "medium", "high", "xhigh", "max"),
        False,
        True,
        False,
        False,
        False,
    ),
    (
        r"^gpt-5\.6-(sol|terra|luna)" + _SNAPSHOT + "$",
        ("none", "low", "medium", "high", "xhigh", "max"),
        False,
        False,
        False,
        False,
        False,
    ),
    (r"^gpt-5\.5-pro" + _SNAPSHOT + "$", ("medium", "high", "xhigh"), False, False, False, True, False),
    (
        r"^gpt-5\.5" + _SNAPSHOT + "$",
        ("none", "low", "medium", "high", "xhigh"),
        False,
        False,
        False,
        False,
        False,
    ),
    (r"^gpt-5\.[24]-pro" + _SNAPSHOT + "$", ("medium", "high", "xhigh"), False, False, False, False, False),
    (
        r"^gpt-5\.[23]-codex" + _SNAPSHOT + "$",
        ("low", "medium", "high", "xhigh"),
        False,
        False,
        False,
        False,
        False,
    ),
    (
        r"^gpt-5\.(2|4|4-mini|4-nano)" + _SNAPSHOT + "$",
        ("none", "low", "medium", "high", "xhigh"),
        False,
        True,
        True,
        False,
        False,
    ),
    (r"^gpt-5\.1" + _SNAPSHOT + "$", ("none", "low", "medium", "high"), False, False, False, False, False),
    (r"^gpt-5-pro" + _SNAPSHOT + "$", ("high",), False, False, False, False, False),
    (
        r"^gpt-5(-mini|-nano)?" + _SNAPSHOT + "$",
        ("minimal", "low", "medium", "high"),
        False,
        False,
        False,
        False,
        False,
    ),
    (r"^o[13]-pro" + _SNAPSHOT + "$", _ALL_EFFORTS, False, False, False, True, False),
    (r"^ft:.+$", _ALL_EFFORTS, False, False, False, False, True),
)


def _family(model: str) -> tuple[str, tuple[str, ...], bool, bool, bool, bool, bool]:
    return next(
        (item for item in _FAMILIES if re.fullmatch(item[0], model)),
        ("", _ALL_EFFORTS, False, False, False, False, False),
    )


def _rules(model: str) -> ModelRequestRules:
    family = _family(model)
    return ModelRequestRules(
        temperature_not_accepted() if family[2] else temperature_range(0, 2),
        {ReasoningEffort(value): value for value in family[1]},
        StructuredOutputRules(
            StructuredOutputMode.JSON_SCHEMA,
            ("minimum", "maximum", "minLength", "maxLength", "minItems", "maxItems", "format")
            if family[6]
            else (),
        ),
    )


def _encode(value: EncodeRequestInput) -> EncodedRequest:
    request, rules = value.request, value.rules
    family = _family(request.model)
    if (
        request.temperature is not None
        and family[3]
        and request.reasoning_effort != "none"
        and not (not request.reasoning_effort and family[4])
    ):
        raise ValueError("the model accepts a temperature only with reasoning effort none")
    body: dict[str, object] = {
        "model": request.model,
        "input": [{"role": message.role, "content": message.text} for message in request.messages],
        "store": False,
    }
    if request.instructions:
        body["instructions"] = request.instructions
    if request.max_output_tokens:
        body["max_output_tokens"] = request.max_output_tokens
    if request.temperature is not None:
        body["temperature"] = request.temperature
    if request.reasoning_effort:
        body["reasoning"] = {"effort": rules.reasoning_efforts[ReasoningEffort(request.reasoning_effort)]}
    if request.structured_output:
        output = request.structured_output
        format_value: dict[str, object] = {
            "type": "json_schema",
            "name": output.name,
            "schema": output.schema,
            "strict": True,
        }
        if output.description:
            format_value["description"] = output.description
        body["text"] = {"format": format_value}
    if not family[5]:
        body["stream"] = True
    return EncodedRequest("/responses", json_body(body), is_streaming=not family[5])


def _decode(value: dict[str, object], streamed_text: str = "") -> DecodedResponse:
    usage = object_value(value["usage"]) if value.get("usage") is not None else {}
    input_details = (
        object_value(usage["input_tokens_details"]) if usage.get("input_tokens_details") is not None else {}
    )
    output_details = (
        object_value(usage["output_tokens_details"]) if usage.get("output_tokens_details") is not None else {}
    )
    status = string_value(value.get("status"))
    reason = status
    if status == "incomplete":
        incomplete = (
            object_value(value["incomplete_details"]) if value.get("incomplete_details") is not None else {}
        )
        reason = string_value(incomplete.get("reason")) or "incomplete"
    has_content = False
    text = ""
    refusal = False
    outputs = value.get("output")
    if outputs is not None and not isinstance(outputs, list):
        raise ValueError("OpenAI output is invalid")
    for item in outputs or []:
        output = object_value(item)
        if output.get("type") == "reasoning":
            continue
        contents = output.get("content")
        if contents is not None and not isinstance(contents, list):
            raise ValueError("OpenAI content is invalid")
        for item_content in contents or []:
            content = object_value(item_content)
            if content.get("type") == "output_text":
                has_content = True
                text += string_value(content.get("text"))
            elif content.get("type") == "refusal":
                has_content, refusal = True, True
    if not has_content:
        text = streamed_text
    return DecodedResponse(
        (ResponsePart(text),) if text else (),
        string_value(value.get("model")),
        string_value(value.get("id")),
        reason,
        refusal,
        Usage(
            token_count(usage.get("input_tokens")),
            token_count(input_details.get("cached_tokens")),
            token_count(usage.get("output_tokens")),
            token_count(output_details.get("reasoning_tokens")),
            token_count(usage.get("total_tokens")),
        ),
    )


def _decode_response(body: bytes) -> DecodedResponse:
    response = parse_object(body)
    if response.get("error") is not None:
        raise ProviderReportedError(read_error_tokens(body, ("/error/type", "/error/code")))
    return _decode(response)


async def _decode_stream(
    chunks: AsyncIterator[bytes], write_text_delta: Callable[[str], None]
) -> DecodedResponse:
    streamed_text = ""
    async for _, data in sse_events(chunks):
        event = parse_object(data)
        event_type = string_value(event.get("type"))
        if event_type == "response.output_text.delta":
            delta = string_value(event.get("delta"))
            streamed_text += delta
            write_text_delta(delta)
        elif event_type in ("response.completed", "response.incomplete"):
            return _decode(object_value(event.get("response")), streamed_text)
        elif event_type == "response.failed":
            raise ProviderReportedError(read_error_tokens(data, ("/response/error/code",)))
        elif event_type == "error":
            raise ProviderReportedError(read_error_tokens(data, ("/code", "/error/type", "/error/code")))
    raise EOFError("OpenAI response stream ended without completion")


def new_responses_wire_format() -> WireFormat:
    """Return the OpenAI Responses dialect."""
    rules = tuple(
        ErrorRule(429, token, quota_exhausted_outcome())
        for token in (
            "insufficient_quota",
            "credit_balance_exhausted",
            "organization_spend_limit_exceeded",
            "project_spend_limit_exceeded",
            "organization_usage_limit_exceeded",
        )
    )
    rules += tuple(
        ErrorRule(0, token, blocked_outcome())
        for token in ("misalignment_policy_violation", "bio_policy", "cyber_policy")
    )
    rules += (
        ErrorRule(501, "", provider_rejected_outcome(FailureKind.PROVIDER_REJECTION)),
        ErrorRule(0, "server_error", retry_outcome(FailureKind.AVAILABILITY)),
        ErrorRule(0, "rate_limit_exceeded", retry_outcome(FailureKind.RATE_LIMIT)),
    )
    return WireFormat(
        "openai",
        ModelIDRule.BODY,
        RequestFeatures(True, True, True, True, True),
        CredentialHeader("Authorization", "Bearer "),
        _rules,
        _encode,
        _decode_response,
        _decode_stream,
        {
            "completed": FinishReason.STOP,
            "max_output_tokens": FinishReason.LENGTH,
            "content_filter": FinishReason.CONTENT_POLICY,
        },
        rules,
        ("/error/type", "/error/code"),
        None,
        ("x-request-id",),
        (
            "x-ratelimit-limit-requests",
            "x-ratelimit-remaining-requests",
            "x-ratelimit-reset-requests",
            "x-ratelimit-limit-tokens",
            "x-ratelimit-remaining-tokens",
            "x-ratelimit-reset-tokens",
        ),
    )
