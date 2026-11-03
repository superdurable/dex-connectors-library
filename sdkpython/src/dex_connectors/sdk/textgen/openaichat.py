# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""OpenAI-compatible Chat Completions profile and wire format."""

from __future__ import annotations

import json
import re
from collections.abc import AsyncGenerator, AsyncIterator, Callable
from contextlib import aclosing
from dataclasses import dataclass, field, replace
from decimal import Decimal, InvalidOperation
from enum import StrEnum
from typing import Any, cast

from ..providerhttp import read_error_tokens
from .contracts import (
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
    TemperaturePolicy,
    Usage,
    WireFormat,
)

_FIELD = re.compile(r"^[a-z][a-z0-9_]{0,63}$")
_HEADER = re.compile(r"^[!#$%&'*+.^_`|~0-9A-Za-z-]{1,128}$")


class InstructionsRole(StrEnum):
    """Chat role carrying request instructions."""

    SYSTEM = "system"  # First system message; default.
    DEVELOPER = "developer"  # First developer message.


class MaxTokensField(StrEnum):
    """Provider body field carrying the output limit."""

    MAX_TOKENS = "max_tokens"  # Default output-limit field.
    MAX_COMPLETION_TOKENS = "max_completion_tokens"  # Alternate output-limit field.


class StreamingPolicy(StrEnum):
    """Whether the provider receives a streaming request."""

    NEVER = "never"  # Request a complete JSON response; default.
    ALWAYS = "always"  # Request SSE chunks and emit text deltas.


@dataclass(frozen=True, slots=True)
class ModelRule:
    """First matching model override; prefix and anchored pattern are exclusive.

    Exactly one of ``model_id_prefix`` or ``model_id_pattern`` is required.
    Optional ``temperature``, ``reasoning_efforts``, ``structured_output``,
    ``max_tokens_field``, and ``streaming`` replace Profile values. An empty
    effort map rejects every effort.
    """

    model_id_prefix: str = ""
    model_id_pattern: str = ""
    temperature: TemperaturePolicy | None = None
    reasoning_efforts: dict[ReasoningEffort, str] | None = None
    structured_output: StructuredOutputRules | None = None
    max_tokens_field: MaxTokensField | None = None
    streaming: StreamingPolicy | None = None


@dataclass(frozen=True, slots=True)
class Profile:
    """One provider's Chat Completions dialect without host or secrets.

    ``provider_name`` is required. ``chat_completions_path`` defaults to
    /chat/completions; ``credential_header`` to Authorization with Bearer
    prefix. ``fixed_headers`` holds nonsecret constants. ``instructions_role``
    defaults to system; ``max_tokens_field`` to max_tokens; empty
    ``reasoning_efforts`` rejects effort. ``temperature`` accepts finite
    nonnegative values; ``structured_output`` rejects schema requests by
    default. ``should_send_strict_json_schema`` adds strict: true.
    ``streaming`` defaults to never; ``should_request_stream_usage`` adds
    usage stream options. Nonempty ``allowed_request_fields`` enforces an
    allowlist. ``finish_reasons`` extends stop, length, and content_filter.
    ``error_rules`` classify errors; ``error_token_pointers`` default to type
    and code. ``request_id_headers`` default to x-request-id;
    ``rate_limit_headers`` select safe receipt metadata.
    ``stall_timeout_seconds`` is the between-byte limit, or zero to disable.
    The first matching ``model_rules`` entry overrides model settings.
    """

    provider_name: str
    chat_completions_path: str = "/chat/completions"
    credential_header: CredentialHeader = field(
        default_factory=lambda: CredentialHeader("Authorization", "Bearer ")
    )
    fixed_headers: dict[str, str] = field(default_factory=dict)
    instructions_role: InstructionsRole = InstructionsRole.SYSTEM
    max_tokens_field: MaxTokensField = MaxTokensField.MAX_TOKENS
    reasoning_efforts: dict[ReasoningEffort, str] = field(default_factory=dict)
    temperature: TemperaturePolicy = field(default_factory=TemperaturePolicy)
    structured_output: StructuredOutputRules = field(default_factory=StructuredOutputRules)
    should_send_strict_json_schema: bool = False
    streaming: StreamingPolicy = StreamingPolicy.NEVER
    should_request_stream_usage: bool = False
    allowed_request_fields: tuple[str, ...] = ()
    finish_reasons: dict[str, FinishReason] = field(default_factory=dict)
    error_rules: tuple[ErrorRule, ...] = ()
    error_token_pointers: tuple[str, ...] = ("/error/type", "/error/code")
    request_id_headers: tuple[str, ...] = ("x-request-id",)
    rate_limit_headers: tuple[str, ...] = ()
    stall_timeout_seconds: float = 0
    model_rules: tuple[ModelRule, ...] = ()


def _validate_profile(profile: Profile) -> None:
    if not profile.provider_name.strip():
        raise ValueError("openaichat profile provider name is required")
    if not profile.chat_completions_path.startswith("/") or any(
        char in profile.chat_completions_path for char in "?# "
    ):
        raise ValueError(
            "openaichat profile chat completions path must start with / and hold no query, fragment, or space"
        )
    reserved = {profile.credential_header.name.lower(), "content-type", "content-length", "accept", "host"}
    for name, value in profile.fixed_headers.items():
        if not _HEADER.fullmatch(name) or name.lower() in reserved:
            raise ValueError("openaichat profile fixed header name is invalid or reserved")
        if any(ord(char) < 32 or ord(char) > 126 for char in value):
            raise ValueError("openaichat profile fixed header value must be printable ASCII")
    if not isinstance(profile.instructions_role, InstructionsRole):
        raise ValueError("openaichat profile instructions role is invalid")
    _validate_settings(
        profile.max_tokens_field, profile.streaming, profile.reasoning_efforts, profile.structured_output
    )
    for name in profile.allowed_request_fields:
        if not _FIELD.fullmatch(name):
            raise ValueError("openaichat profile allowed request field is invalid")
    for index, rule in enumerate(profile.model_rules):
        if bool(rule.model_id_prefix) == bool(rule.model_id_pattern):
            raise ValueError(
                f"openaichat profile model rule {index} must set exactly one of prefix and pattern"
            )
        if rule.model_id_pattern:
            if not rule.model_id_pattern.startswith("^") or not rule.model_id_pattern.endswith("$"):
                raise ValueError("openaichat profile model pattern must be anchored with ^ and $")
            try:
                re.compile(rule.model_id_pattern)
            except re.error as error:
                raise ValueError("openaichat profile model pattern is invalid") from error
        _validate_settings(
            rule.max_tokens_field,
            rule.streaming,
            rule.reasoning_efforts,
            rule.structured_output or StructuredOutputRules(),
        )
    if profile.allowed_request_fields:
        required = {"model", "messages"}
        can_stream = profile.streaming == StreamingPolicy.ALWAYS or any(
            rule.streaming == StreamingPolicy.ALWAYS for rule in profile.model_rules
        )
        if can_stream:
            required.add("stream")
            if profile.should_request_stream_usage:
                required.add("stream_options")
        for field_name in sorted(required):
            if field_name not in profile.allowed_request_fields:
                raise ValueError(f'openaichat profile allowed request fields must include "{field_name}"')


def _validate_settings(
    max_tokens_field: MaxTokensField | None,
    streaming: StreamingPolicy | None,
    efforts: dict[ReasoningEffort, str] | None,
    structured: StructuredOutputRules,
) -> None:
    if max_tokens_field is not None and not isinstance(max_tokens_field, MaxTokensField):
        raise ValueError("openaichat profile max tokens field is invalid")
    if streaming is not None and not isinstance(streaming, StreamingPolicy):
        raise ValueError("openaichat profile streaming policy is invalid")
    for effort, wire_value in (efforts or {}).items():
        if not isinstance(effort, ReasoningEffort) or not _FIELD.fullmatch(wire_value):
            raise ValueError("openaichat profile reasoning effort is invalid")
    structured.validate()


class _ChatWireFormat:
    def __init__(self, profile: Profile) -> None:
        self.profile = profile

    def settings(self, model: str) -> tuple[ModelRequestRules, MaxTokensField, StreamingPolicy]:
        profile = self.profile
        temperature = profile.temperature
        efforts = profile.reasoning_efforts
        structured = profile.structured_output
        tokens = profile.max_tokens_field
        streaming = profile.streaming
        for rule in profile.model_rules:
            if (
                rule.model_id_prefix
                and model.startswith(rule.model_id_prefix)
                or rule.model_id_pattern
                and re.search(rule.model_id_pattern, model)
            ):
                temperature = rule.temperature if rule.temperature is not None else temperature
                efforts = rule.reasoning_efforts if rule.reasoning_efforts is not None else efforts
                structured = rule.structured_output if rule.structured_output is not None else structured
                tokens = rule.max_tokens_field or tokens
                streaming = rule.streaming or streaming
                break
        return ModelRequestRules(temperature, efforts, structured), tokens, streaming

    def rules_for_model(self, model: str) -> ModelRequestRules:
        return self.settings(model)[0]

    def encode_request(self, input: EncodeRequestInput) -> EncodedRequest:
        request = input.request
        _, tokens_field, streaming = self.settings(request.model)
        messages = []
        if request.instructions:
            messages.append({"role": self.profile.instructions_role.value, "content": request.instructions})
        messages.extend({"role": item.role, "content": item.text} for item in request.messages)
        body: dict[str, Any] = {"model": request.model, "messages": messages}
        if request.max_output_tokens > 0:
            body[tokens_field.value] = request.max_output_tokens
        if request.temperature is not None:
            body["temperature"] = request.temperature
        if request.reasoning_effort:
            body["reasoning_effort"] = input.rules.reasoning_efforts[
                cast(ReasoningEffort, request.reasoning_effort)
            ]
        if request.structured_output is not None:
            output = request.structured_output
            if input.rules.structured_output.mode == StructuredOutputMode.JSON_SCHEMA:
                schema: dict[str, Any] = {"name": output.name, "schema": output.schema}
                if output.description:
                    schema["description"] = output.description
                if self.profile.should_send_strict_json_schema:
                    schema["strict"] = True
                body["response_format"] = {"type": "json_schema", "json_schema": schema}
            elif input.rules.structured_output.mode == StructuredOutputMode.JSON_OBJECT_WITH_INSTRUCTION:
                body["response_format"] = {"type": "json_object"}
        is_streaming = streaming == StreamingPolicy.ALWAYS
        if is_streaming:
            body["stream"] = True
            if self.profile.should_request_stream_usage:
                body["stream_options"] = {"include_usage": True}
        if self.profile.allowed_request_fields:
            for field_name in sorted(body):
                if field_name not in self.profile.allowed_request_fields:
                    raise ValueError(f'the provider does not accept the "{field_name}" request field')
        try:
            encoded = json.dumps(body, separators=(",", ":"), allow_nan=False).encode()
        except (TypeError, ValueError) as error:
            raise ValueError("the request body is not JSON serializable") from error
        return EncodedRequest(
            self.profile.chat_completions_path, encoded, dict(self.profile.fixed_headers), is_streaming
        )

    def _reported_error(self, body: bytes) -> ProviderReportedError:
        return ProviderReportedError(read_error_tokens(body, self.profile.error_token_pointers))

    def decode_response(self, body: bytes) -> DecodedResponse:
        completion = json.loads(body.decode("utf-8", errors="replace"))
        if not isinstance(completion, dict):
            raise ValueError("chat completion must be an object")
        if completion.get("error") is not None:
            raise self._reported_error(body)
        choice = _first_choice(completion.get("choices"))
        if choice is None:
            raise ValueError("chat completion has no first choice")
        parts, refusal = _parts(choice.get("message"))
        return DecodedResponse(
            tuple(parts),
            _nullable_string(completion.get("model")),
            _nullable_string(completion.get("id")),
            _nullable_string(choice.get("finish_reason")),
            refusal,
            _usage(completion.get("usage")),
        )

    async def decode_stream(
        self, chunks: AsyncIterator[bytes], write_text_delta: Callable[[str], None]
    ) -> DecodedResponse:
        parts: list[ResponsePart] = []
        served_model = ""
        response_id = ""
        finish = ""
        refusal = False
        usage = Usage()
        has_finished = False
        async with aclosing(_events(chunks)) as events:
            async for event in events:
                if event.strip() == b"[DONE]":
                    if not has_finished:
                        raise ValueError("stream ended without finish reason")
                    return DecodedResponse(tuple(parts), served_model, response_id, finish, refusal, usage)
                chunk = json.loads(event.decode("utf-8", errors="replace"))
                if not isinstance(chunk, dict):
                    raise ValueError("chat stream chunk must be an object")
                if chunk.get("error") is not None:
                    raise self._reported_error(event)
                response_id = response_id or _nullable_string(chunk.get("id"))
                served_model = served_model or _nullable_string(chunk.get("model"))
                if chunk.get("usage") is not None:
                    usage = _usage(chunk["usage"])
                choice = _first_choice(chunk.get("choices"))
                if choice is None:
                    continue
                new_parts, new_refusal = _parts(choice.get("delta"))
                refusal |= new_refusal
                for part in new_parts:
                    if parts and parts[-1].is_reasoning == part.is_reasoning:
                        parts[-1] = ResponsePart(parts[-1].text + part.text, part.is_reasoning)
                    else:
                        parts.append(part)
                    if not part.is_reasoning:
                        write_text_delta(part.text)
                finish_reason = _nullable_string(choice.get("finish_reason"))
                if finish_reason:
                    finish = finish_reason
                    has_finished = True
        if not has_finished:
            raise EOFError("stream ended before terminal event")
        return DecodedResponse(tuple(parts), served_model, response_id, finish, refusal, usage)


async def _events(chunks: AsyncIterator[bytes]) -> AsyncGenerator[bytes]:
    buffer = b""
    event_lines: list[bytes] = []
    async for chunk in chunks:
        buffer += chunk
        while b"\n" in buffer:
            line, buffer = buffer.split(b"\n", 1)
            line = line.removesuffix(b"\r")
            if not line:
                if event_lines:
                    yield b"\n".join(event_lines)
                    event_lines = []
                continue
            if line.startswith(b"data:"):
                value = line[5:]
                event_lines.append(value[1:] if value.startswith(b" ") else value)
            elif line == b"data":
                event_lines.append(b"")


def _nullable_string(value: object) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("chat field must be a string")
    return value


def _first_choice(value: object) -> dict[str, Any] | None:
    if value is None:
        return None
    if not isinstance(value, list):
        raise ValueError("chat choices must be an array")
    for item in value:
        if not isinstance(item, dict):
            raise ValueError("chat choice must be an object")
        index = item.get("index")
        if index is not None and (not isinstance(index, int) or isinstance(index, bool)):
            raise ValueError("chat choice index must be an integer")
        if index in (None, 0):
            return item
    return None


def _parts(delta: object) -> tuple[list[ResponsePart], bool]:
    if not isinstance(delta, dict):
        raise ValueError("chat message must be an object")
    parts = []
    reasoning = delta.get("reasoning_content")
    if reasoning is not None and not isinstance(reasoning, str):
        raise ValueError("chat reasoning content must be a string")
    if reasoning:
        parts.append(ResponsePart(reasoning, True))
    content = delta.get("content")
    if isinstance(content, str):
        if content:
            parts.append(ResponsePart(content))
    elif isinstance(content, list):
        for item in content:
            if item is None:
                continue
            if not isinstance(item, dict):
                raise ValueError("chat content part must be an object")
            part_type = _nullable_string(item.get("type"))
            if part_type == "text":
                part_text = _nullable_string(item.get("text"))
                if part_text:
                    parts.append(ResponsePart(part_text))
    elif content is not None:
        raise ValueError("chat content has invalid type")
    refusal = delta.get("refusal")
    if refusal is not None and not isinstance(refusal, str):
        raise ValueError("chat refusal must be a string")
    return parts, bool(refusal)


def _token_count(value: object) -> int:
    if value is None:
        return 0
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise ValueError("token count must be an integer")
    try:
        number = Decimal(str(value))
    except InvalidOperation as error:
        raise ValueError("token count must be an integer") from error
    if number != number.to_integral_value() or abs(number) >= 2**63:
        raise ValueError("token count must be an integer")
    return int(number)


def _usage(value: object) -> Usage:
    if value is None:
        return Usage()
    if not isinstance(value, dict):
        raise ValueError("chat usage must be an object")
    prompt_details = value.get("prompt_tokens_details")
    completion_details = value.get("completion_tokens_details")
    if prompt_details is not None and not isinstance(prompt_details, dict):
        raise ValueError("chat prompt token details must be an object")
    if completion_details is not None and not isinstance(completion_details, dict):
        raise ValueError("chat completion token details must be an object")
    return Usage(
        _token_count(value.get("prompt_tokens")),
        _token_count((prompt_details or {}).get("cached_tokens")),
        _token_count(value.get("completion_tokens")),
        _token_count((completion_details or {}).get("reasoning_tokens")),
        _token_count(value.get("total_tokens")),
    )


def new_wire_format(profile: Profile) -> WireFormat:
    """Validate a profile and create its Chat Completions wire format."""
    if profile is None:
        raise ValueError("openaichat profile is required")
    profile = replace(
        profile,
        chat_completions_path=profile.chat_completions_path or "/chat/completions",
        credential_header=profile.credential_header
        if profile.credential_header.name
        else CredentialHeader("Authorization", "Bearer "),
        instructions_role=profile.instructions_role or InstructionsRole.SYSTEM,
        max_tokens_field=profile.max_tokens_field or MaxTokensField.MAX_TOKENS,
        streaming=profile.streaming or StreamingPolicy.NEVER,
    )
    _validate_profile(profile)
    adapter = _ChatWireFormat(profile)
    finish_reasons = {
        "stop": FinishReason.STOP,
        "length": FinishReason.LENGTH,
        "content_filter": FinishReason.CONTENT_POLICY,
    }
    finish_reasons.update(profile.finish_reasons)
    return WireFormat(
        profile.provider_name,
        ModelIDRule.BODY,
        RequestFeatures(True, True, True, True, True),
        profile.credential_header,
        adapter.rules_for_model,
        adapter.encode_request,
        adapter.decode_response,
        adapter.decode_stream,
        finish_reasons,
        profile.error_rules,
        profile.error_token_pointers,
        None,
        profile.request_id_headers,
        profile.rate_limit_headers,
        profile.stall_timeout_seconds,
    )
