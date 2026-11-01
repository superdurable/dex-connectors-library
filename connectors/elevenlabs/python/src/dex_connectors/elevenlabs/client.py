# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""ElevenLabs voice listing and speech generation with Go-compatible classification."""

import base64
import json
import re
from collections.abc import Callable
from dataclasses import dataclass as dataclass
from datetime import UTC, datetime
from datetime import timedelta as timedelta
from typing import Any, TypeVar
from urllib.parse import urlencode

import httpx
from dex_connectors.sdk import BranchDefinition as BranchDefinition
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
    Receipt,
)
from dex_connectors.sdk import OperationRef as OperationRef
from dex_connectors.sdk import SecretString as SecretString
from dex_connectors.sdk import StepDefaults as StepDefaults
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)
from dex_connectors.sdk.retry_policy import RetryPolicy as RetryPolicy
from dex_connectors.sdk.retry_policy import (
    default_connector_step_execute_retry_policy as default_connector_step_execute_retry_policy,
)

from ._generated.config import (
    Config as Config,
)
from ._generated.config import (
    Credentials as Credentials,
)
from ._generated.definitions import (
    CONNECTOR_ID as CONNECTOR_ID,
)
from ._generated.definitions import (
    CREATE_SPEECH_DEFINITION as CREATE_SPEECH_DEFINITION,
)
from ._generated.definitions import (
    CREATE_SPEECH_OPERATION_ID as CREATE_SPEECH_OPERATION_ID,
)
from ._generated.definitions import (
    CREATED as CREATED,
)
from ._generated.definitions import (
    DEFECT as DEFECT,
)
from ._generated.definitions import (
    INVALID_RESPONSE as INVALID_RESPONSE,
)
from ._generated.definitions import (
    LIST_VOICES_DEFINITION as LIST_VOICES_DEFINITION,
)
from ._generated.definitions import (
    LIST_VOICES_OPERATION_ID as LIST_VOICES_OPERATION_ID,
)
from ._generated.definitions import (
    LISTED as LISTED,
)
from ._generated.definitions import (
    PROVIDER_REJECTED as PROVIDER_REJECTED,
)
from .models import CreateSpeechInput, CreateSpeechOutput, ListVoicesInput, ListVoicesOutput, Voice

DEFAULT_MODEL_ID = "eleven_multilingual_v2"
SPEECH_OUTPUT_FORMAT = "mp3_44100_128"
_DEFAULT_BASE_URL = "https://api.elevenlabs.io"
_DEFAULT_TIMEOUT_SECONDS = 120
_DEFAULT_MAX_RESPONSE_BYTES = 2_097_152
_MAX_PAGE_SIZE = 100
_VOICE_ID = re.compile(r"[A-Za-z0-9_-]{1,128}")
_CREDENTIAL_MESSAGE = "connection credentials are unavailable or invalid"

Output = TypeVar("Output")






class Client:
    """One redirect-safe HTTP client and call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("elevenlabs configuration is required")
        if credentials is None:
            raise ValueError("elevenlabs credential source is required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("elevenlabs timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def list_voices(self) -> "ListVoicesOperation":
        """Bind the voice listing Query to this client."""
        return ListVoicesOperation(self)

    def create_speech(self) -> "CreateSpeechOperation":
        """Bind the billed speech generation Query to this client."""
        return CreateSpeechOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def invoke_query(
        self,
        call: Call,
        operation: str,
        success: str,
        method: str,
        path: str,
        query: dict[str, str],
        payload: bytes | None,
        empty: Output,
        decode: Callable[[bytes], Output],
    ) -> QueryAttempt[Output]:
        """Send one request and classify it exactly like the Go invokeJSONQuery helper."""
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            return _failed(operation, DEFECT, empty, FailureKind.AUTHENTICATION, _CREDENTIAL_MESSAGE)
        key = credentials.api_key.reveal()
        if not key or not is_header_safe_credential(key):
            return _failed(operation, DEFECT, empty, FailureKind.AUTHENTICATION, _CREDENTIAL_MESSAGE)
        endpoint = self.base_url + path
        if query:
            endpoint += "?" + urlencode(sorted(query.items()))
        headers = {
            "xi-api-key": key,
            "Accept": "audio/mpeg" if path.startswith("/v1/text-to-speech/") else "application/json",
        }
        if payload is not None:
            headers["Content-Type"] = "application/json"
        try:
            request = self.http_client.build_request(method, endpoint, content=payload, headers=headers)
            response, _ = await send_traced(self.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(operation, FailureKind.AVAILABILITY, "provider request failed", 0)
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(
                    operation, FailureKind.RATE_LIMIT, "provider rate limited the request", retry_after
                )
            if status == 408 or status >= 500:
                return _retry(
                    operation, FailureKind.AVAILABILITY, "provider is temporarily unavailable", retry_after
                )
            if status != 200:
                return _failed(
                    operation,
                    PROVIDER_REJECTED,
                    empty,
                    FailureKind.PROVIDER_REJECTION,
                    f"provider rejected the request with HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _failed(
                    operation,
                    INVALID_RESPONSE,
                    empty,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "provider response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(operation, FailureKind.AVAILABILITY, "provider response was interrupted", 0)
            try:
                value = decode(body)
            except (ValueError, TypeError, UnicodeDecodeError):
                return _failed(
                    operation,
                    INVALID_RESPONSE,
                    empty,
                    FailureKind.PROTOCOL,
                    "provider returned an invalid response",
                )
            return QueryAttempt.branch_result(success, value, Receipt())
        finally:
            await response.aclose()


class ListVoicesOperation:
    """Retrieve one page of voices and the provider's continuation token."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("elevenlabs client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return LIST_VOICES_DEFINITION

    async def invoke(self, call: Call, input: ListVoicesInput) -> QueryAttempt[ListVoicesOutput]:
        """Validate the page request, list once, then select a branch or safe retry."""
        operation = LIST_VOICES_OPERATION_ID
        if (
            not isinstance(input, ListVoicesInput)
            or type(input.page_size) is not int
            or not 0 <= input.page_size <= _MAX_PAGE_SIZE
            or not isinstance(input.next_page_token, str)
            or "\r" in input.next_page_token
            or "\n" in input.next_page_token
        ):
            return _failed(
                operation, DEFECT, ListVoicesOutput(), FailureKind.VALIDATION, "page size or token is invalid"
            )
        query: dict[str, str] = {}
        if input.page_size > 0:
            query["page_size"] = str(input.page_size)
        if input.next_page_token:
            query["next_page_token"] = input.next_page_token
        return await self.client.invoke_query(
            call, operation, LISTED, "GET", "/v2/voices", query, None, ListVoicesOutput(), _decode_voices
        )


class CreateSpeechOperation:
    """Generate one billed MP3 audio result; each attempt may incur a charge."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("elevenlabs client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and the three-attempt billed retry policy."""
        return CREATE_SPEECH_DEFINITION

    async def invoke(self, call: Call, input: CreateSpeechInput) -> QueryAttempt[CreateSpeechOutput]:
        """Validate the voice and text, synthesize once, then select a branch or bounded retry."""
        operation = CREATE_SPEECH_OPERATION_ID
        if (
            not isinstance(input, CreateSpeechInput)
            or not isinstance(input.voice_id, str)
            or _VOICE_ID.fullmatch(input.voice_id) is None
            or not isinstance(input.text, str)
            or not input.text.strip()
            or not isinstance(input.model_id, str)
        ):
            return _failed(
                operation,
                DEFECT,
                CreateSpeechOutput(),
                FailureKind.VALIDATION,
                "voiceId and text are required",
            )
        payload = json.dumps(
            {"text": input.text, "model_id": input.model_id or DEFAULT_MODEL_ID}, separators=(",", ":")
        ).encode()
        return await self.client.invoke_query(
            call,
            operation,
            CREATED,
            "POST",
            "/v1/text-to-speech/" + input.voice_id,
            {"output_format": SPEECH_OUTPUT_FORMAT},
            payload,
            CreateSpeechOutput(),
            _decode_speech,
        )


def _decode_voices(body: bytes) -> ListVoicesOutput:
    document = json.loads(body)
    if not isinstance(document, dict):
        raise ValueError
    voices = document.get("voices")
    has_more = _optional(document, "has_more", False)
    next_page_token = _optional(document, "next_page_token", "")
    if not isinstance(voices, list) or not isinstance(has_more, bool) or not isinstance(next_page_token, str):
        raise ValueError
    return ListVoicesOutput(tuple(_decode_voice(voice) for voice in voices), has_more, next_page_token)


def _decode_voice(document: Any) -> Voice:
    if not isinstance(document, dict):
        raise ValueError
    voice_id = _optional(document, "voice_id", "")
    name = _optional(document, "name", "")
    if not isinstance(voice_id, str) or not voice_id or not isinstance(name, str):
        raise ValueError
    return Voice(voice_id, name)


def _optional(document: dict[str, Any], name: str, default: Any) -> Any:
    """Return a JSON field, treating absent or null like Go's zero value."""
    value = document.get(name)
    return default if value is None else value


def _decode_speech(body: bytes) -> CreateSpeechOutput:
    if not body:
        raise ValueError
    return CreateSpeechOutput(base64.b64encode(body).decode("ascii"), "audio/mpeg")


def _failed(
    operation: str, branch: str, value: Output, kind: FailureKind, message: str
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(
        branch, value, Receipt(), Failure(kind, CONNECTOR_ID, operation, message)
    )


def _retry(operation: str, kind: FailureKind, message: str, after_seconds: float) -> QueryAttempt[Any]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, operation, message), after_seconds)
