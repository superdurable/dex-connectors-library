# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Youdao signed text translation Query."""

import hashlib
import json
import secrets
from datetime import UTC, datetime
from urllib.parse import urlparse

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
from dex_connectors.sdk.providerhttp import new_provider_http_client, parse_retry_after

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import TRANSLATE_TEXT_DEFINITION
from .models import TextTranslation, TranslateTextInput


def _valid_language(code: str) -> bool:
    return (
        isinstance(code, str)
        and 0 < len(code) <= 24
        and all(c.isascii() and (c.isalpha() or c == "-") for c in code)
    )


class Client:
    """Resolve rotated app credentials and sign each translation request."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        now: datetime | None = None,
        salt_bytes: bytes | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or config.max_response_bytes <= 0
        ):
            raise ValueError("Youdao configuration and credentials are required")
        parsed = urlparse(config.base_url)
        if (
            parsed.scheme not in {"https", "http"}
            or not parsed.netloc
            or parsed.path
            or parsed.query
            or parsed.fragment
        ):
            raise ValueError("Youdao base URL must be an origin")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None
        self.now = now
        self.salt_bytes = salt_bytes

    def translate_text(self) -> "TranslateTextOperation":
        """Bind the generated translateText Query."""
        return TranslateTextOperation(self)

    async def aclose(self) -> None:
        """Close the owned HTTP transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class TranslateTextOperation:
    """Translate bounded UTF-8 text to a selected language."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return TRANSLATE_TEXT_DEFINITION

    def _failure(self, branch: str, kind: FailureKind) -> QueryAttempt[TextTranslation]:
        failure = Failure(kind, "youdao", "translateText", "Youdao translation failed")
        if branch == "retry":
            return QueryAttempt.retry(failure)
        return QueryAttempt.branch_result(branch, TextTranslation(), Receipt(), failure)

    async def invoke(self, call: Call, input: TranslateTextInput) -> QueryAttempt[TextTranslation]:
        """Send one signed request and classify provider outcome."""
        if (
            not isinstance(input.text, str)
            or not input.text.strip()
            or len(input.text) > 5000
            or not _valid_language(input.source_language)
            or not _valid_language(input.target_language)
            or input.target_language == "auto"
        ):
            return self._failure("defect", FailureKind.VALIDATION)
        try:
            credentials = await self.client.credentials.resolve(call)
        except (LookupError, ValueError, OSError, AttributeError):
            return self._failure("defect", FailureKind.AUTHENTICATION)
        app_key = credentials.app_key
        app_secret = credentials.app_secret.reveal()
        if (
            not isinstance(app_key, str)
            or not 0 < len(app_key) <= 256
            or not all(0x20 < ord(c) < 0x7F for c in app_key)
            or not app_secret
        ):
            return self._failure("defect", FailureKind.AUTHENTICATION)
        salt = (
            self.client.salt_bytes if self.client.salt_bytes is not None else secrets.token_bytes(16)
        ).hex()
        curtime = str(int((self.client.now or datetime.now(UTC)).timestamp()))
        text = (
            input.text if len(input.text) <= 20 else input.text[:10] + str(len(input.text)) + input.text[-10:]
        )
        digest = hashlib.sha256((app_key + text + salt + curtime + app_secret).encode()).hexdigest()
        form = {
            "q": input.text,
            "from": input.source_language,
            "to": input.target_language,
            "appKey": app_key,
            "salt": salt,
            "curtime": curtime,
            "signType": "v3",
            "sign": digest,
        }
        try:
            response = await self.client.http_client.post(
                self.client.config.base_url.rstrip("/") + "/api",
                data=form,
                headers={"Accept": "application/json", "Content-Type": "application/x-www-form-urlencoded"},
            )
        except httpx.HTTPError:
            return self._failure("retry", FailureKind.TRANSPORT)
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            body = await response.aread()
            if status == 429:
                return QueryAttempt.retry(
                    Failure(
                        FailureKind.RATE_LIMIT, "youdao", "translateText", "Youdao rate limited the request"
                    ),
                    retry_after,
                )
            if status == 408 or status >= 500:
                return QueryAttempt.retry(
                    Failure(FailureKind.AVAILABILITY, "youdao", "translateText", "Youdao unavailable"),
                    retry_after,
                )
            if len(body) > self.client.config.max_response_bytes:
                return self._failure("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
        except httpx.HTTPError:
            return self._failure("retry", FailureKind.TRANSPORT)
        finally:
            await response.aclose()
        if status != 200:
            return self._failure("providerRejected", FailureKind.PROVIDER_REJECTION)
        try:
            wire = json.loads(body.decode("utf-8"))
        except (UnicodeError, ValueError):
            return self._failure("invalidResponse", FailureKind.PROTOCOL)
        if not isinstance(wire, dict) or not isinstance(wire.get("errorCode"), str) or not wire["errorCode"]:
            return self._failure("invalidResponse", FailureKind.PROTOCOL)
        code = wire["errorCode"]
        if code == "0":
            translation = wire.get("translation")
            if (
                not isinstance(translation, list)
                or not 1 <= len(translation) <= 10
                or any(not isinstance(value, str) or not value or len(value) > 65536 for value in translation)
            ):
                return self._failure("invalidResponse", FailureKind.PROTOCOL)
            return QueryAttempt.branch_result(
                "translated",
                TextTranslation(tuple(translation), input.source_language, input.target_language),
                Receipt(),
                None,
            )
        if code in {"411", "412"}:
            return QueryAttempt.retry(
                Failure(FailureKind.RATE_LIMIT, "youdao", "translateText", "Youdao rate limited the request"),
                retry_after,
            )
        if code in {"302", "303", "304"}:
            return QueryAttempt.retry(
                Failure(FailureKind.AVAILABILITY, "youdao", "translateText", "Youdao unavailable"),
                retry_after,
            )
        kind = (
            FailureKind.AUTHENTICATION
            if code in {"108", "111", "202", "203", "206", "401"}
            else FailureKind.PROVIDER_REJECTION
        )
        return self._failure("providerRejected", kind)
