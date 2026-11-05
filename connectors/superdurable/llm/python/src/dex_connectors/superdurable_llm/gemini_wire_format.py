# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Gemini generateContent dialect for portable text generation."""

import json
import re

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
    temperature_range,
)

from .wire_common import json_body, object_value, parse_object, string_value, token_count

_POINTERS = (
    "/error/status",
    "/error/details/0/reason",
    "/error/details/1/reason",
    "/error/details/2/reason",
    "/0/error/status",
    "/0/error/details/0/reason",
    "/0/error/details/1/reason",
    "/0/error/details/2/reason",
)
_POLICY = (
    "SAFETY",
    "RECITATION",
    "LANGUAGE",
    "BLOCKLIST",
    "PROHIBITED_CONTENT",
    "SPII",
    "IMAGE_SAFETY",
    "IMAGE_PROHIBITED_CONTENT",
    "IMAGE_RECITATION",
    "IMAGE_OTHER",
    "ESCALATION",
    "PUP_LIMITED_DISABLED",
)
_PROMPT_POLICY = ("SAFETY", "OTHER", "BLOCKLIST", "PROHIBITED_CONTENT", "IMAGE_SAFETY")


def _rules(model: str) -> ModelRequestRules:
    levels: tuple[str, ...] = ("minimal", "low", "medium", "high")
    if re.match(r"^gemini-[12]\.", model):
        levels = ()
    elif (re.match(r"^gemini-3\.[78]-flash|^gemini-3(\.1)?-pro", model) and "-lite" not in model) or re.match(
        r"^gemini-(flash|pro)-latest$", model
    ):
        levels = ("low", "medium", "high")
    return ModelRequestRules(
        temperature_range(0, 2),
        {ReasoningEffort(level): level.upper() for level in levels},
        StructuredOutputRules(StructuredOutputMode.JSON_SCHEMA, ("minLength", "maxLength", "format")),
    )


def _encode(value: EncodeRequestInput) -> EncodedRequest:
    request, rules = value.request, value.rules
    body: dict[str, object] = {
        "contents": [
            {"role": "model" if message.role == "assistant" else "user", "parts": [{"text": message.text}]}
            for message in request.messages
        ]
    }
    if request.instructions:
        body["systemInstruction"] = {"parts": [{"text": request.instructions}]}
    settings: dict[str, object] = {}
    if request.structured_output:
        output = request.structured_output
        schema = dict(output.schema)
        if output.description:
            previous = schema.get("description")
            schema["description"] = output.description + (
                "\n\n" + previous if isinstance(previous, str) and previous else ""
            )
        settings["responseMimeType"] = "application/json"
        settings["responseJsonSchema"] = schema
    if request.temperature is not None:
        settings["temperature"] = request.temperature
    if request.max_output_tokens:
        settings["maxOutputTokens"] = request.max_output_tokens
    if request.reasoning_effort:
        settings["thinkingConfig"] = {
            "thinkingLevel": rules.reasoning_efforts[ReasoningEffort(request.reasoning_effort)]
        }
    if settings:
        body["generationConfig"] = settings
    return EncodedRequest("/models/" + request.model + ":generateContent", json_body(body))


def _decode(body: bytes) -> DecodedResponse:
    response = parse_object(body)
    if response.get("error") is not None:
        raise ProviderReportedError(read_error_tokens(body, _POINTERS))
    usage = object_value(response["usageMetadata"]) if response.get("usageMetadata") is not None else {}
    thought = token_count(usage.get("thoughtsTokenCount"))
    finish = ""
    parts: list[ResponsePart] = []
    feedback = object_value(response["promptFeedback"]) if response.get("promptFeedback") is not None else {}
    block = string_value(feedback.get("blockReason"))
    if block and block != "BLOCK_REASON_UNSPECIFIED":
        if block in _PROMPT_POLICY:
            finish = "blockReason:" + block
        elif re.fullmatch(r"[A-Z][A-Z0-9_]{0,63}", block):
            finish = "blockReason:UNDOCUMENTED"
    else:
        candidates = response.get("candidates")
        if candidates is not None and not isinstance(candidates, list):
            raise ValueError("Gemini candidates are invalid")
        if candidates:
            candidate = object_value(candidates[0])
            finish = string_value(candidate.get("finishReason"))
            content = object_value(candidate["content"]) if candidate.get("content") is not None else {}
            content_parts = content.get("parts")
            if content_parts is not None and not isinstance(content_parts, list):
                raise ValueError("Gemini content parts are invalid")
            for item in content_parts or []:
                part = object_value(item)
                parts.append(ResponsePart(string_value(part.get("text")), part.get("thought") is True))
    return DecodedResponse(
        tuple(parts),
        string_value(response.get("modelVersion")),
        string_value(response.get("responseId")),
        finish,
        False,
        Usage(
            token_count(usage.get("promptTokenCount")),
            token_count(usage.get("cachedContentTokenCount")),
            token_count(usage.get("candidatesTokenCount")) + thought,
            thought,
            token_count(usage.get("totalTokenCount")),
        ),
    )


def _retry_delay(body: bytes) -> float:
    try:
        decoded = json.loads(body)
        envelope = decoded[0] if isinstance(decoded, list) and decoded else decoded
        error = object_value(object_value(envelope).get("error"))
        details = error.get("details")
        if not isinstance(details, list):
            return 0
        delay = 0.0
        for item in details:
            if not isinstance(item, dict) or item.get("@type") != "type.googleapis.com/google.rpc.RetryInfo":
                continue
            value = item.get("retryDelay")
            if isinstance(value, str) and re.fullmatch(r"[0-9]{1,10}(?:\.[0-9]{1,9})?s", value):
                delay = min(float(value[:-1]), 3600)
        return delay
    except (ValueError, TypeError, KeyError):
        return 0


def new_gemini_wire_format() -> WireFormat:
    """Return the Gemini generateContent dialect."""
    reasons = {"STOP": FinishReason.STOP, "MAX_TOKENS": FinishReason.LENGTH}
    reasons.update({reason: FinishReason.CONTENT_POLICY for reason in _POLICY})
    reasons.update(
        {"blockReason:" + reason: FinishReason.CONTENT_POLICY for reason in (*_PROMPT_POLICY, "UNDOCUMENTED")}
    )
    return WireFormat(
        "gemini",
        ModelIDRule.PATH_SEGMENT,
        RequestFeatures(True, True, True, True, True),
        CredentialHeader("x-goog-api-key"),
        _rules,
        _encode,
        _decode,
        finish_reasons=reasons,
        error_rules=(
            ErrorRule(400, "API_KEY_INVALID", provider_rejected_outcome(FailureKind.AUTHENTICATION)),
        ),
        error_token_pointers=_POINTERS,
        read_error_retry_delay=_retry_delay,
        request_id_headers=("X-Goog-Request-Id", "X-Request-Id"),
    )
