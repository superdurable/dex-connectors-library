# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Gemini models.generateContent with Go-compatible request checks and branch classification."""

import ipaddress
import json
import math
import re
from dataclasses import dataclass, replace
from datetime import UTC, datetime, timedelta
from email.utils import parsedate_to_datetime
from typing import Any
from urllib.parse import quote, urlsplit

import httpx
from dex_connectors.sdk import (
    BranchDefinition,
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    OperationRef,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
    Receipt,
    SecretString,
    StepDefaults,
)
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    new_provider_http_client,
    read_bounded_body,
    send_traced,
)
from dex_connectors.sdk.retry_policy import RetryPolicy

from .models import Content, GenerateContentRequest, GenerateContentResponse, Usage

CONNECTOR_ID = "gemini"
PROVIDER = "google"
GENERATE_CONTENT_OPERATION_ID = "generateContent"
GENERATED = "generated"
TRUNCATED = "truncated"
BLOCKED = "blocked"
PROVIDER_REJECTED = "providerRejected"
INVALID_RESPONSE = "invalidResponse"
DEFECT = "defect"
UI_UNIT_MODEL_PICKER = "modelPicker"
UI_MODEL_PICKER_PORT_MODEL = "model"
DEFAULT_MODEL = "gemini-3.5-flash-lite"
DEFAULT_ENDPOINT = "https://generativelanguage.googleapis.com/v1beta"
DEFAULT_MAX_RESPONSE_BYTES = 8_388_608
JSON_MIME_TYPE = "application/json"
_REQUEST_TIMEOUT_SECONDS = 270
_MAX_ERROR_RESPONSE_BYTES = 64 << 10
_MAX_PROVIDER_RETRY_DELAY_SECONDS = 3600.0
_MODEL_ID = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{0,127}")
_PROVIDER_ENUM = re.compile(r"[A-Z][A-Z0-9_]{0,63}")
_PROTO_DURATION = re.compile(r"[0-9]{1,10}(\.[0-9]{1,9})?s")
_CONTENT_POLICY_FINISH_REASONS = frozenset(
    {
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
    }
)
GENERATE_CONTENT_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GENERATE_CONTENT_OPERATION_ID),
    (
        BranchDefinition(GENERATED, "Gemini finished the first candidate normally and returned its text."),
        BranchDefinition(
            TRUNCATED,
            "Gemini stopped the first candidate at the output token limit and returned any partial text.",
            True,
        ),
        BranchDefinition(
            BLOCKED,
            "Gemini blocked the prompt or stopped the candidate for safety, recitation, "
            "or another content policy.",
            True,
        ),
        BranchDefinition(
            PROVIDER_REJECTED,
            "Gemini conclusively rejected the request, such as an invalid argument, a denied permission, "
            "or an unknown model.",
            True,
        ),
        BranchDefinition(
            INVALID_RESPONSE,
            "Gemini returned a malformed, oversized, or unusable response, such as no candidate "
            "or an unrecognized finish reason.",
            True,
        ),
        BranchDefinition(
            DEFECT, "Local input, connection configuration, or connector definition is invalid.", True
        ),
    ),
    StepDefaults(
        timedelta(seconds=300),
        1,
        heartbeat_timeout=timedelta(seconds=300),
        execute_retry=RetryPolicy(timedelta(seconds=2), 2, timedelta(seconds=60), 3, timedelta(minutes=10)),
    ),
)


@dataclass(frozen=True, slots=True)
class Config:
    """Default model, API endpoint, and response size limit; blank or zero values use manifest defaults."""

    model: str = DEFAULT_MODEL
    endpoint: str = DEFAULT_ENDPOINT
    max_response_bytes: int = DEFAULT_MAX_RESPONSE_BYTES


@dataclass(frozen=True, slots=True)
class Credentials:
    """Gemini API key, resolved anew for every provider attempt."""

    api_key: SecretString


@dataclass(frozen=True, slots=True)
class _GoogleErrorDetails:
    status: str = ""
    reason: str = ""
    retry_delay_seconds: float = 0


class Client:
    """One redirect-safe HTTP client with a 270-second request timeout and a call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("Gemini configuration is required")
        model = trim_model_prefix_and_validate(config.model.strip() or DEFAULT_MODEL)
        if model is None:
            raise ValueError("Gemini model must be a Gemini model ID such as gemini-3.5-flash-lite")
        if type(config.max_response_bytes) is not int or config.max_response_bytes < 0:
            raise ValueError("Gemini maxResponseBytes must be positive")
        if credentials is None:
            raise ValueError("credential provider is required")
        self.endpoint = validate_and_trim_endpoint(config.endpoint or DEFAULT_ENDPOINT)
        self.model = model
        self.max_response_bytes = config.max_response_bytes or DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(_REQUEST_TIMEOUT_SECONDS)

    def __repr__(self) -> str:
        """Keep the endpoint and credentials out of logs."""
        return "gemini.Client{[REDACTED]}"

    def generate_content(self) -> "GenerateContentOperation":
        """Bind the generateContent Query to this client."""
        return GenerateContentOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()


class GenerateContentOperation:
    """Generate text or JSON Schema structured output; each attempt may be billed."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Gemini client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and the billed three-attempt retry policy."""
        return GENERATE_CONTENT_DEFINITION

    async def invoke(
        self, call: Call, input: GenerateContentRequest
    ) -> QueryAttempt[GenerateContentResponse]:
        """Validate, generate once, then select a branch or bounded retry."""
        encoded = encode_generate_content_request(input, self.client.model)
        if isinstance(encoded, Failure):
            return QueryAttempt.branch_result(DEFECT, GenerateContentResponse(), Receipt(), encoded)
        model, body = encoded
        request = await self._build_request(call, model, body)
        if isinstance(request, Failure):
            return QueryAttempt.branch_result(DEFECT, GenerateContentResponse(), Receipt(), request)
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return QueryAttempt.retry(
                _failure(FailureKind.TRANSPORT, "provider request failed before a response was received")
            )
        try:
            request_id = response.headers.get("X-Goog-Request-Id") or response.headers.get("X-Request-Id", "")
            if not 200 <= response.status_code <= 299:
                return await _status_attempt(call, response, request_id)
            try:
                response_body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return QueryAttempt.branch_result(
                    INVALID_RESPONSE,
                    GenerateContentResponse(),
                    _receipt(call, request_id, ""),
                    _failure(
                        FailureKind.RESPONSE_TOO_LARGE, "provider response exceeds the configured size limit"
                    ),
                )
            except ProviderTransportError:
                return QueryAttempt.retry(
                    _failure(FailureKind.TRANSPORT, "provider response could not be read")
                )
        finally:
            await response.aclose()
        try:
            document = _decode_wire_response(json.loads(response_body))
        except (ValueError, TypeError, UnicodeDecodeError):
            return QueryAttempt.branch_result(
                INVALID_RESPONSE,
                GenerateContentResponse(),
                _receipt(call, request_id, ""),
                _failure(FailureKind.PROTOCOL, "provider response is not a valid generateContent response"),
            )
        branch, value, failure = classify_generate_content_response(document)
        return QueryAttempt.branch_result(
            branch, value, _receipt(call, request_id, value.response_id), failure
        )

    async def _build_request(self, call: Call, model: str, body: bytes) -> httpx.Request | Failure:
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            return _failure(FailureKind.AUTHENTICATION, "connection credentials are unavailable")
        api_key = credentials.api_key.reveal()
        if not is_valid_api_key(api_key):
            return _failure(FailureKind.AUTHENTICATION, "connection credentials are invalid")
        target = f"{self.client.endpoint}/models/{quote(model, safe='$&+:=@')}:generateContent"
        try:
            return self.client.http_client.build_request(
                "POST",
                target,
                content=body,
                headers={"x-goog-api-key": api_key, "Content-Type": JSON_MIME_TYPE, "Accept": JSON_MIME_TYPE},
            )
        except (httpx.HTTPError, ValueError):
            return _failure(FailureKind.LOCAL_DEFECT, "request could not be built")


def encode_generate_content_request(
    input: GenerateContentRequest, connection_model: str
) -> tuple[str, bytes] | Failure:
    """Return the target model and wire body, or the validation Failure the Go connector reports."""
    if not isinstance(input, GenerateContentRequest):
        return _failure(FailureKind.VALIDATION, "request is not JSON serializable")
    model = connection_model
    if input.model:
        trimmed = trim_model_prefix_and_validate(input.model)
        if trimmed is None:
            return _failure(
                FailureKind.VALIDATION, "model must be a Gemini model ID such as gemini-3.5-flash-lite"
            )
        model = trimmed
    if not input.contents:
        return _failure(FailureKind.VALIDATION, "at least one content is required")
    contents: list[dict[str, Any]] = []
    for content in input.contents:
        if not isinstance(content, Content) or content.role not in ("", "user", "model"):
            return _failure(FailureKind.VALIDATION, "content role must be user, model, or empty")
        if not content.parts:
            return _failure(FailureKind.VALIDATION, "every content requires at least one part")
        if any(not part.text for part in content.parts):
            return _failure(FailureKind.VALIDATION, "every part requires text")
        wire_content: dict[str, Any] = {"role": content.role} if content.role else {}
        wire_content["parts"] = [{"text": part.text} for part in content.parts]
        contents.append(wire_content)
    payload: dict[str, Any] = {"contents": contents}
    if input.system_instruction:
        payload["systemInstruction"] = {"parts": [{"text": input.system_instruction}]}
    generation_config: dict[str, Any] = {}
    if input.response_mime_type:
        generation_config["responseMimeType"] = input.response_mime_type
    if input.response_json_schema is not None:
        generation_config.setdefault("responseMimeType", JSON_MIME_TYPE)
        if generation_config["responseMimeType"] != JSON_MIME_TYPE:
            return _failure(
                FailureKind.VALIDATION, "responseJsonSchema requires the application/json response MIME type"
            )
        try:
            json.dumps(input.response_json_schema, allow_nan=False)
        except (TypeError, ValueError):
            return _failure(FailureKind.VALIDATION, "responseJsonSchema must be JSON serializable")
        generation_config["responseJsonSchema"] = input.response_json_schema
    if input.temperature is not None:
        if (
            type(input.temperature) not in (int, float)
            or math.isnan(input.temperature)
            or not 0 <= input.temperature <= 2
        ):
            return _failure(FailureKind.VALIDATION, "temperature must be between 0 and 2")
        generation_config["temperature"] = input.temperature
    if type(input.max_output_tokens) is not int or input.max_output_tokens < 0:
        return _failure(FailureKind.VALIDATION, "maxOutputTokens cannot be negative")
    if input.max_output_tokens:
        generation_config["maxOutputTokens"] = input.max_output_tokens
    if input.thinking_budget is not None:
        if type(input.thinking_budget) is not int or input.thinking_budget < -1:
            return _failure(
                FailureKind.VALIDATION, "thinkingBudget must be -1, 0, or a positive token budget"
            )
        generation_config["thinkingConfig"] = {"thinkingBudget": input.thinking_budget}
    if generation_config:
        payload["generationConfig"] = generation_config
    try:
        return model, json.dumps(payload, separators=(",", ":"), allow_nan=False).encode()
    except (TypeError, ValueError):
        return _failure(FailureKind.VALIDATION, "request is not JSON serializable")


def classify_generate_content_response(
    document: dict[str, Any],
) -> tuple[str, GenerateContentResponse, Failure | None]:
    """Select the branch, value, and Failure for a decoded generateContent response."""
    usage = document["usageMetadata"]
    value = GenerateContentResponse(
        response_id=document["responseId"],
        model_version=document["modelVersion"],
        usage=Usage(
            usage["promptTokenCount"],
            usage["candidatesTokenCount"],
            usage["thoughtsTokenCount"],
            usage["cachedContentTokenCount"],
            usage["totalTokenCount"],
        ),
    )
    block_reason = document["blockReason"]
    if block_reason and block_reason != "BLOCK_REASON_UNSPECIFIED":
        if _PROVIDER_ENUM.fullmatch(block_reason) is None:
            return _invalid(value, "provider returned an unrecognized block reason")
        return (
            BLOCKED,
            replace(value, block_reason=block_reason),
            _failure(FailureKind.PROVIDER_REJECTION, "provider blocked the prompt: " + block_reason),
        )
    candidates = document["candidates"]
    if not candidates:
        return _invalid(value, "provider returned no candidates")
    finish_reason, parts = candidates[0]
    if not finish_reason:
        return _invalid(value, "provider candidate has no finish reason")
    if _PROVIDER_ENUM.fullmatch(finish_reason) is None:
        return _invalid(value, "provider returned an unrecognized finish reason")
    value = replace(value, finish_reason=finish_reason)
    text = "".join(part_text for part_text, is_thought in parts if not is_thought)
    if finish_reason == "STOP":
        if not text:
            return _invalid(value, "provider candidate finished without text")
        return GENERATED, replace(value, text=text), None
    if finish_reason == "MAX_TOKENS":
        return (
            TRUNCATED,
            replace(value, text=text),
            _failure(
                FailureKind.RESPONSE_TOO_LARGE, "provider stopped the candidate at the output token limit"
            ),
        )
    if finish_reason in _CONTENT_POLICY_FINISH_REASONS:
        return (
            BLOCKED,
            value,
            _failure(FailureKind.PROVIDER_REJECTION, "provider stopped the candidate: " + finish_reason),
        )
    return _invalid(value, "provider stopped the candidate without a usable result: " + finish_reason)


def trim_model_prefix_and_validate(value: str) -> str | None:
    """Strip a models/ prefix and return the model ID, or None when it is not a Gemini model ID."""
    if not isinstance(value, str):
        return None
    model = value.removeprefix("models/")
    return model if _MODEL_ID.fullmatch(model) else None


def validate_and_trim_endpoint(value: str) -> str:
    """Require an absolute HTTPS URL, or HTTP on loopback, without user info, query, or fragment."""
    try:
        endpoint = urlsplit(value)
        hostname = endpoint.hostname or ""
    except ValueError as error:
        raise ValueError("Gemini endpoint must be an absolute URL") from error
    if not endpoint.scheme or not hostname:
        raise ValueError("Gemini endpoint must be an absolute URL")
    if "@" in endpoint.netloc or endpoint.query or endpoint.fragment or value.endswith("?") or "?" in value:
        raise ValueError("Gemini endpoint cannot contain user info, a query, or a fragment")
    if endpoint.scheme == "http":
        if not _is_loopback(hostname):
            raise ValueError("Gemini endpoint must use HTTPS unless it is a loopback test server")
    elif endpoint.scheme != "https":
        raise ValueError("Gemini endpoint must use HTTPS")
    return value.rstrip("/")


def is_valid_api_key(key: str) -> bool:
    """Accept a nonempty key of printable, non-space ASCII characters."""
    return bool(key) and all("!" <= character <= "~" for character in key)


def _is_loopback(host: str) -> bool:
    if host.lower() == "localhost":
        return True
    try:
        return ipaddress.ip_address(host).is_loopback
    except ValueError:
        return False


async def _status_attempt(
    call: Call, response: httpx.Response, request_id: str
) -> QueryAttempt[GenerateContentResponse]:
    status = response.status_code
    try:
        error_body = await read_bounded_body(response, _MAX_ERROR_RESPONSE_BYTES)
    except (BodyTooLargeError, ProviderTransportError):
        error_body = b""
    details = _parse_google_error_details(error_body)
    message = f"provider returned HTTP {status}" + (f" {details.status}" if details.status else "")
    failure = _failure(_status_failure_kind(status, details.reason), message)
    if status in (408, 429) or (status >= 500 and status != 501):
        delay = details.retry_delay_seconds or _retry_after_delay(response.headers.get("Retry-After", ""))
        return QueryAttempt.retry(failure, delay)
    return QueryAttempt.branch_result(
        PROVIDER_REJECTED, GenerateContentResponse(), _receipt(call, request_id, ""), failure
    )


def _parse_google_error_details(error_body: bytes) -> _GoogleErrorDetails:
    try:
        document = json.loads(error_body)
    except (ValueError, UnicodeDecodeError):
        return _GoogleErrorDetails()
    if isinstance(document, list):
        document = document[0] if document else None
    if not isinstance(document, dict) or not isinstance(document.get("error") or {}, dict):
        return _GoogleErrorDetails()
    error = document.get("error") or {}
    status = error.get("status")
    details = error.get("details") or []
    if not isinstance(details, list):
        return _GoogleErrorDetails()
    reason = ""
    retry_delay_seconds = 0.0
    for detail in details:
        if not isinstance(detail, dict):
            return _GoogleErrorDetails()
        if detail.get("@type") == "type.googleapis.com/google.rpc.ErrorInfo":
            candidate = detail.get("reason")
            if isinstance(candidate, str) and _PROVIDER_ENUM.fullmatch(candidate):
                reason = candidate
        elif detail.get("@type") == "type.googleapis.com/google.rpc.RetryInfo":
            retry_delay_seconds = _parse_proto_duration(detail.get("retryDelay"))
    status = status if isinstance(status, str) and _PROVIDER_ENUM.fullmatch(status) else ""
    return _GoogleErrorDetails(status, reason, retry_delay_seconds)


def _parse_proto_duration(value: object) -> float:
    if not isinstance(value, str) or _PROTO_DURATION.fullmatch(value) is None:
        return 0
    seconds = float(value[:-1])
    return min(seconds, _MAX_PROVIDER_RETRY_DELAY_SECONDS) if seconds > 0 else 0


def _retry_after_delay(value: str) -> float:
    value = value.strip()
    if not value:
        return 0
    if re.fullmatch(r"[+-]?[0-9]+", value):
        seconds = int(value)
        return 0 if seconds <= 0 else min(float(seconds), _MAX_PROVIDER_RETRY_DELAY_SECONDS)
    try:
        retry_at = parsedate_to_datetime(value)
    except (TypeError, ValueError):
        return 0
    if retry_at.tzinfo is None:
        retry_at = retry_at.replace(tzinfo=UTC)
    delay = (retry_at - datetime.now(UTC)).total_seconds()
    return min(delay, _MAX_PROVIDER_RETRY_DELAY_SECONDS) if delay > 0 else 0


def _status_failure_kind(status: int, reason: str) -> FailureKind:
    if status == 401 or reason == "API_KEY_INVALID":
        return FailureKind.AUTHENTICATION
    if status == 403:
        return FailureKind.AUTHORIZATION
    if status == 404:
        return FailureKind.NOT_FOUND
    if status == 409:
        return FailureKind.CONFLICT
    if status == 429:
        return FailureKind.RATE_LIMIT
    if status == 408 or (status >= 500 and status != 501):
        return FailureKind.AVAILABILITY
    return FailureKind.PROVIDER_REJECTION


def _decode_wire_response(document: Any) -> dict[str, Any]:
    """Decode the fields Go reads, with Go's null-as-zero handling and strict types."""
    document = _object(document)
    usage = _object(document.get("usageMetadata"))
    candidates = []
    for candidate in _list(document.get("candidates")):
        candidate = _object(candidate)
        parts = []
        for part in _list(_object(candidate.get("content")).get("parts")):
            part = _object(part)
            parts.append((_typed(part.get("text"), str, ""), _typed(part.get("thought"), bool, False)))
        candidates.append((_typed(candidate.get("finishReason"), str, ""), parts))
    return {
        "candidates": candidates,
        "blockReason": _typed(_object(document.get("promptFeedback")).get("blockReason"), str, ""),
        "usageMetadata": {
            name: _typed(usage.get(name), int, 0)
            for name in (
                "promptTokenCount",
                "cachedContentTokenCount",
                "candidatesTokenCount",
                "thoughtsTokenCount",
                "totalTokenCount",
            )
        },
        "modelVersion": _typed(document.get("modelVersion"), str, ""),
        "responseId": _typed(document.get("responseId"), str, ""),
    }


def _object(value: Any) -> dict[str, Any]:
    if value is None:
        return {}
    if not isinstance(value, dict):
        raise ValueError
    return value


def _list(value: Any) -> list[Any]:
    if value is None:
        return []
    if not isinstance(value, list):
        raise ValueError
    return value


def _typed(value: Any, expected: type, default: Any) -> Any:
    if value is None:
        return default
    if type(value) is not expected:
        raise ValueError
    return value


def _invalid(value: GenerateContentResponse, message: str) -> tuple[str, GenerateContentResponse, Failure]:
    return INVALID_RESPONSE, value, _failure(FailureKind.PROTOCOL, message)


def _receipt(call: Call, request_id: str, response_id: str) -> Receipt:
    return Receipt(
        call_id=call.id,
        provider=CONNECTOR_ID,
        observed_at=datetime.now(UTC),
        provider_object_id=response_id,
        provider_request_id=request_id,
    )


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, GENERATE_CONTENT_OPERATION_ID, message)
