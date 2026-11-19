# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Niutrans provider transport and Go-compatible Query decisions."""

import json
import re
from datetime import UTC, datetime
from urllib.parse import urlencode

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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import TRANSLATE_TEXT_DEFINITION
from .models import TranslateTextInput, TranslateTextOutput


def _opaque(value: str, limit: int) -> bool:
    return (
        isinstance(value, str) and 0 < len(value) <= limit and all(0x20 < ord(char) < 0x7F for char in value)
    )


class Client:
    """Resolve credentials for each translateText request."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or not 0 < config.max_response_bytes <= 16 << 20
        ):
            raise ValueError("Niutrans configuration and credentials are required")
        validate_base_url(config.base_url)
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None

    def translate_text(self) -> "TranslateTextOperation":
        """Bind the translateText Query."""
        return TranslateTextOperation(self)

    async def aclose(self) -> None:
        """Close the owned HTTP transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class TranslateTextOperation:
    """Query Niutrans and classify the provider outcome."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return TRANSLATE_TEXT_DEFINITION

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[TranslateTextOutput]:
        return QueryAttempt.branch_result(
            branch,
            TranslateTextOutput(),
            Receipt(),
            Failure(kind, "niutrans", "translateText", "Niutrans request failed"),
        )

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[TranslateTextOutput]:
        return QueryAttempt.retry(
            Failure(kind, "niutrans", "translateText", "Niutrans request failed"), delay
        )

    def _request(
        self, input: TranslateTextInput, credentials: Credentials
    ) -> httpx.Request | QueryAttempt[TranslateTextOutput]:
        if (
            not input.text.strip()
            or len(input.text) > 1000
            or not re.fullmatch(r"[A-Za-z][A-Za-z-]{1,11}", input.from_)
            or not re.fullmatch(r"[A-Za-z][A-Za-z-]{1,11}", input.to)
            or input.from_ == input.to
        ):
            return self._branch("defect", FailureKind.VALIDATION)
        token = credentials.api_key.reveal()
        if not _opaque(token, 512):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        form = urlencode({"from": input.from_, "to": input.to, "apikey": token, "src_text": input.text})
        return self.client.http_client.build_request(
            "POST",
            self.client.config.base_url.rstrip("/") + "/NiuTransServer/translation",
            content=form,
            headers={"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json"},
        )

    def _decode(
        self, wire: object, input: TranslateTextInput, credentials: Credentials
    ) -> TranslateTextOutput | QueryAttempt[TranslateTextOutput] | None:
        if not isinstance(wire, dict):
            return None
        error_code = wire.get("error_code", "")
        if error_code:
            if error_code == "10001":
                return self._retry(FailureKind.RATE_LIMIT)
            if error_code == "13008":
                return self._retry(FailureKind.AVAILABILITY)
            return self._branch("providerRejected", FailureKind.PROVIDER_REJECTION)
        translated = wire.get("tgt_text")
        if (
            not isinstance(translated, str)
            or not translated
            or len(translated.encode()) > self.client.config.max_response_bytes
        ):
            return None
        return TranslateTextOutput(translated_text=translated)

    async def invoke(self, call: Call, input: TranslateTextInput) -> QueryAttempt[TranslateTextOutput]:
        """Read one bounded provider response without persisting secrets."""
        if not isinstance(input, TranslateTextInput):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        request = self._request(input, credentials)
        if isinstance(request, QueryAttempt):
            return request
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return self._retry(FailureKind.AVAILABILITY)
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return self._retry(FailureKind.RATE_LIMIT, delay)
            if status == 408 or status >= 500:
                return self._retry(FailureKind.AVAILABILITY, delay)
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return self._branch("providerRejected", kind)
            try:
                body = await read_bounded_body(response, self.client.config.max_response_bytes)
            except BodyTooLargeError:
                return self._branch("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
            except ProviderTransportError:
                return self._retry(FailureKind.AVAILABILITY)
        finally:
            await response.aclose()
        try:
            wire = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        result = self._decode(wire, input, credentials)
        if isinstance(result, QueryAttempt):
            return result
        if result is None:
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        return QueryAttempt.branch_result("translated", result, Receipt())
