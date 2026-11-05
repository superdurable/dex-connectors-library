# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Claude Messages request and streamed response dialect."""

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
    provider_rejected_outcome,
    quota_exhausted_outcome,
    retry_outcome,
    temperature_range,
)

from .wire_common import json_body, object_value, parse_object, sse_events, string_value, token_count

_POINTERS = ("/error/type", "/error/details/error_code")
_OUTPUT_RULES = StructuredOutputRules(
    StructuredOutputMode.JSON_SCHEMA,
    ("minimum", "maximum", "minLength", "maxLength", "minItems", "maxItems"),
)


def _rules(model: str) -> ModelRequestRules:
    minimum = 1
    efforts: tuple[str, ...] = ("low", "medium", "high", "xhigh", "max")
    if re.fullmatch(r"claude-(opus|sonnet)-4-6", model):
        minimum, efforts = 0, ("low", "medium", "high", "max")
    elif model == "claude-mythos-preview":
        efforts = ("low", "medium", "high", "max")
    elif re.fullmatch(r"claude-opus-4-5(?:-\d{8})?", model):
        minimum, efforts = 0, ("low", "medium", "high")
    elif re.fullmatch(r"claude-(sonnet|haiku)-4-5(?:-\d{8})?", model):
        minimum, efforts = 0, ()
    return ModelRequestRules(
        temperature_range(minimum, 1), {ReasoningEffort(item): item for item in efforts}, _OUTPUT_RULES
    )


def _encode(value: EncodeRequestInput, workspace_id: str) -> EncodedRequest:
    request, rules = value.request, value.rules
    body: dict[str, object] = {
        "model": request.model,
        "max_tokens": request.max_output_tokens or 16000,
        "messages": [{"role": message.role, "content": message.text} for message in request.messages],
        "stream": True,
    }
    if request.instructions:
        body["system"] = request.instructions
    if request.temperature is not None:
        body["temperature"] = request.temperature
    output_config: dict[str, object] = {}
    if request.reasoning_effort:
        output_config["effort"] = rules.reasoning_efforts[ReasoningEffort(request.reasoning_effort)]
    if request.structured_output:
        output = request.structured_output
        schema = dict(output.schema)
        if output.description and not schema.get("description"):
            schema["description"] = output.description
        output_config["format"] = {"type": "json_schema", "schema": schema}
    if output_config:
        body["output_config"] = output_config
    headers = {"anthropic-version": "2023-06-01"}
    if workspace_id:
        headers["anthropic-workspace-id"] = workspace_id
    return EncodedRequest("/v1/messages", json_body(body), headers, True)


def _usage(value: dict[str, object]) -> Usage:
    cached = token_count(value.get("cache_read_input_tokens"))
    input_tokens = (
        token_count(value.get("input_tokens"))
        + token_count(value.get("cache_creation_input_tokens"))
        + cached
    )
    output_tokens = token_count(value.get("output_tokens"))
    details = (
        object_value(value["output_tokens_details"]) if value.get("output_tokens_details") is not None else {}
    )
    return Usage(
        input_tokens,
        cached,
        output_tokens,
        token_count(details.get("thinking_tokens")),
        input_tokens + output_tokens,
    )


def _merge_usage(target: dict[str, object], value: object) -> None:
    if value is None:
        return
    update = object_value(value)
    for name in (
        "input_tokens",
        "cache_creation_input_tokens",
        "cache_read_input_tokens",
        "output_tokens",
        "output_tokens_details",
    ):
        if update.get(name) is not None:
            target[name] = update[name]


def _decode(body: bytes) -> DecodedResponse:
    message = parse_object(body)
    if message.get("type") == "error":
        raise ProviderReportedError(read_error_tokens(body, _POINTERS))
    if message.get("type") != "message":
        raise ValueError("Claude response is not a message")
    parts: list[ResponsePart] = []
    contents = message.get("content")
    if contents is not None and not isinstance(contents, list):
        raise ValueError("Claude content is invalid")
    for item in contents or []:
        block = object_value(item)
        if block.get("type") == "text":
            parts.append(ResponsePart(string_value(block.get("text"))))
        elif block.get("type") == "thinking":
            parts.append(ResponsePart(string_value(block.get("thinking")), True))
    return DecodedResponse(
        tuple(parts),
        string_value(message.get("model")),
        string_value(message.get("id")),
        string_value(message.get("stop_reason")),
        False,
        _usage(object_value(message["usage"]) if message.get("usage") is not None else {}),
    )


async def _stream(chunks: AsyncIterator[bytes], write_text_delta: Callable[[str], None]) -> DecodedResponse:
    started = False
    blocks: dict[int, str] = {}
    usage: dict[str, object] = {}
    parts: list[ResponsePart] = []
    response_id = served_model = finish = ""
    async for event_name, data in sse_events(chunks):
        event = parse_object(data)
        kind = string_value(event.get("type"))
        if event_name == "error" or kind == "error":
            raise ProviderReportedError(read_error_tokens(data, _POINTERS))
        if kind == "message_start":
            if started:
                raise ValueError("Claude message_start repeated")
            message = object_value(event.get("message"))
            started = True
            response_id, served_model = string_value(message.get("id")), string_value(message.get("model"))
            _merge_usage(usage, message.get("usage"))
        elif kind in (
            "content_block_start",
            "content_block_delta",
            "content_block_stop",
            "message_delta",
            "message_stop",
        ):
            if not started:
                raise ValueError("Claude event arrived before message_start")
            if kind == "content_block_start":
                index = event.get("index")
                if isinstance(index, bool) or not isinstance(index, int):
                    raise ValueError("Claude content block index is invalid")
                block = object_value(event.get("content_block"))
                blocks[index] = string_value(block.get("type"))
                value = (
                    string_value(block.get("text"))
                    if blocks[index] == "text"
                    else string_value(block.get("thinking"))
                )
                if value:
                    is_reasoning = blocks[index] == "thinking"
                    parts.append(ResponsePart(value, is_reasoning))
                    if not is_reasoning:
                        write_text_delta(value)
            elif kind == "content_block_delta":
                index = event.get("index")
                if isinstance(index, bool) or not isinstance(index, int):
                    raise ValueError("Claude content block index is invalid")
                delta = object_value(event.get("delta"))
                delta_type = string_value(delta.get("type"))
                if delta_type in ("text_delta", "thinking_delta"):
                    is_reasoning = delta_type == "thinking_delta"
                    if blocks.get(index) != ("thinking" if is_reasoning else "text"):
                        raise ValueError("Claude delta has no matching block")
                    value = string_value(delta.get("thinking" if is_reasoning else "text"))
                    if value:
                        parts.append(ResponsePart(value, is_reasoning))
                        if not is_reasoning:
                            write_text_delta(value)
            elif kind == "message_delta":
                delta = object_value(event.get("delta"))
                finish = string_value(delta.get("stop_reason")) or finish
                _merge_usage(usage, event.get("usage"))
            elif kind == "message_stop":
                return DecodedResponse(tuple(parts), served_model, response_id, finish, False, _usage(usage))
    raise EOFError("Claude response stream ended without message_stop")


def new_claude_wire_format(workspace_id: str) -> WireFormat:
    """Return the Claude Messages dialect for one workspace."""
    error_rules = (
        ErrorRule(0, "enforced_spend_limit_reached", quota_exhausted_outcome()),
        ErrorRule(0, "billing_error", quota_exhausted_outcome()),
        ErrorRule(501, "", provider_rejected_outcome(FailureKind.PROVIDER_REJECTION)),
        *tuple(
            ErrorRule(0, token, retry_outcome(FailureKind.AVAILABILITY))
            for token in ("overloaded_error", "api_error", "timeout_error")
        ),
        ErrorRule(0, "rate_limit_error", retry_outcome(FailureKind.RATE_LIMIT)),
    )
    return WireFormat(
        "anthropic",
        ModelIDRule.BODY,
        RequestFeatures(True, True, True, True, True),
        CredentialHeader("Authorization", "Bearer "),
        _rules,
        lambda value: _encode(value, workspace_id),
        _decode,
        _stream,
        {
            "end_turn": FinishReason.STOP,
            "stop_sequence": FinishReason.STOP,
            "max_tokens": FinishReason.LENGTH,
            "model_context_window_exceeded": FinishReason.LENGTH,
            "refusal": FinishReason.REFUSAL,
        },
        error_rules,
        _POINTERS,
        None,
        ("request-id",),
        (
            "anthropic-ratelimit-requests-limit",
            "anthropic-ratelimit-requests-remaining",
            "anthropic-ratelimit-tokens-limit",
            "anthropic-ratelimit-tokens-remaining",
            "anthropic-ratelimit-input-tokens-limit",
            "anthropic-ratelimit-input-tokens-remaining",
            "anthropic-ratelimit-output-tokens-limit",
            "anthropic-ratelimit-output-tokens-remaining",
        ),
    )
