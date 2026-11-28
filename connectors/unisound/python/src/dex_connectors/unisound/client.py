# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider transport and Go-compatible Query decisions."""

import json
from datetime import UTC, datetime
from typing import Any

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_SYSTEM_VOICES_DEFINITION
from ._generated.models import SystemVoice
from .models import ListSystemVoicesInput, ListSystemVoicesOutput

_PROVIDER = "unisound"
_OPERATION = "listSystemVoices"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListSystemVoicesOutput]:
    return QueryAttempt.branch_result(
        branch, ListSystemVoicesOutput(), Receipt(), Failure(kind, _PROVIDER, _OPERATION, message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListSystemVoicesOutput]:
    return QueryAttempt.retry(Failure(kind, _PROVIDER, _OPERATION, message), delay)


class Client:
    """Own the provider HTTP transport and resolve rotating credentials per Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Unisound configuration and credentials are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("Unisound configuration is invalid")

        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_system_voices(self) -> "ListSystemVoicesOperation":
        """Bind the listSystemVoices Query."""
        return ListSystemVoicesOperation(self)

    async def aclose(self) -> None:
        """Close this client's own transport."""
        if self._owns_client:
            await self.http_client.aclose()


class ListSystemVoicesOperation:
    """Run the listSystemVoices provider Query."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Unisound client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity."""
        return LIST_SYSTEM_VOICES_DEFINITION

    async def invoke(self, call: Call, input: ListSystemVoicesInput) -> QueryAttempt[ListSystemVoicesOutput]:
        """Send one request and classify its provider outcome."""
        prepared = await _prepare(self.client, call, input)
        if isinstance(prepared, QueryAttempt):
            return prepared
        try:
            response, _ = await send_traced(self.client.http_client, prepared)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Unisound voice request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Unisound rate limited the voice request", delay)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "Unisound voice service is temporarily unavailable", delay
                )
            if status != 200 and not False:
                return _http_rejection(status)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Unisound response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Unisound response was interrupted")
            if status != 200:
                return _http_rejection(status)
        finally:
            await response.aclose()
        return _decode(body, input, delay)


def _valid_key(value: str) -> bool:
    return bool(value) and len(value) <= 1024 and all(33 <= ord(c) <= 126 for c in value)


async def _prepare(
    client: Client, call: Call, input: ListSystemVoicesInput
) -> httpx.Request | QueryAttempt[ListSystemVoicesOutput]:
    try:
        credentials = await client.credentials.resolve(call)
        key = credentials.api_key.reveal()
        if not _valid_key(key):
            raise ValueError("invalid key")
    except (ValueError, LookupError, OSError):
        return _branch("defect", FailureKind.AUTHENTICATION, "Unisound API Key is unavailable or invalid")
    try:
        return client.http_client.build_request(
            "POST",
            "https://maas-api.unisound.com/v1/audio/voices/query",
            content=b'{"voice_type":"system"}',
            headers={
                "Authorization": "Bearer " + key,
                "Content-Type": "application/json",
                "Accept": "application/json",
            },
        )
    except (httpx.HTTPError, ValueError):
        return _branch("defect", FailureKind.VALIDATION, "Unisound voice request is invalid")


def _http_rejection(status: int) -> QueryAttempt[ListSystemVoicesOutput]:
    kind = FailureKind.PROVIDER_REJECTION
    if status == 401:
        kind = FailureKind.AUTHENTICATION
    elif status == 403:
        kind = FailureKind.AUTHORIZATION
    return _branch("providerRejected", kind, f"Unisound rejected voice request with HTTP {status}")


def _decode(body: bytes, input: ListSystemVoicesInput, delay: float) -> QueryAttempt[ListSystemVoicesOutput]:
    try:
        document: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        document = None
    if (
        not isinstance(document, dict)
        or not isinstance(document.get("base_resp"), dict)
        or type(document["base_resp"].get("status_code")) is not int
    ):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Unisound returned invalid voice data")
    code = document["base_resp"]["status_code"]
    if code == 100501:
        return _retry(FailureKind.RATE_LIMIT, "Unisound rate limited the voice request")
    if code == 100999:
        return _retry(FailureKind.AVAILABILITY, "Unisound voice service is temporarily unavailable")
    if code != 0:
        return _branch(
            "providerRejected",
            FailureKind.PROVIDER_REJECTION,
            f"Unisound rejected the voice request with code {code}",
        )
    wire_voices = document.get("system_voice")
    if not isinstance(wire_voices, list) or len(wire_voices) > 1000:
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Unisound returned invalid system voices")
    voices: list[SystemVoice] = []
    for voice in wire_voices[:25]:
        if not isinstance(voice, dict):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Unisound returned invalid system voice")
        name = "" if voice.get("voice_name") is None else voice["voice_name"]
        if (
            not isinstance(voice.get("voice_id"), str)
            or not voice["voice_id"]
            or len(voice["voice_id"].encode("utf-8", "replace")) > 128
            or voice["voice_id"].strip() != voice["voice_id"]
            or not isinstance(name, str)
            or len(name.encode("utf-8", "replace")) > 200
            or not isinstance(voice.get("language"), list)
            or len(voice["language"]) > 20
            or any(language is not None and not isinstance(language, str) for language in voice["language"])
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Unisound returned invalid system voice")
        languages = tuple("" if language is None else language for language in voice["language"])
        voices.append(SystemVoice(voice["voice_id"], name, languages))
    return QueryAttempt.branch_result("found", ListSystemVoicesOutput(tuple(voices)), Receipt())
