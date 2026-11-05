# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Help Scout provider transport and credential resolution."""

import json
import re
from collections.abc import Mapping
from dataclasses import dataclass, field
from datetime import UTC, datetime
from typing import TYPE_CHECKING, Any

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    ReauthorizationRequiredError,
    Receipt,
)
from dex_connectors.sdk.providerhttp import (
    MAX_ERROR_BODY_BYTES,
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from .json_values import strict_json_loads
from .query_values import encode_query_values

if TYPE_CHECKING:
    from .operations import (
        FindCustomerByEmailOperation,
        GetConversationOperation,
        ReplyToConversationOperation,
        SearchConversationsOperation,
        UpdateConversationOperation,
    )

_API_BASE = "https://api.helpscout.net"


def _is_ascii_decimal(value: str) -> bool:
    return value.isascii() and value.isdecimal()


@dataclass(frozen=True, slots=True)
class Exchange:
    """One provider HTTP exchange or a secret-safe transport classification."""

    status: int = 0
    headers: httpx.Headers = field(default_factory=httpx.Headers)
    body: bytes = b""
    error: str = ""
    token: str = ""


class Client:
    """Resolve an access token per call and send bounded Inbox API requests."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Help Scout configuration and credential source are required")
        if type(config.max_response_bytes) is not int or config.max_response_bytes < 0:
            raise ValueError("Help Scout maxResponseBytes must be positive")
        if type(config.webhook_max_body_bytes) is not int or config.webhook_max_body_bytes < 0:
            raise ValueError("Help Scout webhookMaxBodyBytes must be positive")
        self.config = Config(
            config.max_response_bytes or Config().max_response_bytes,
            config.webhook_max_body_bytes or Config().webhook_max_body_bytes,
        )
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(25, address_policy="publicHTTPS")

    def search_conversations(self) -> "SearchConversationsOperation":
        """Bind the conversation search Query."""
        from .operations import SearchConversationsOperation

        return SearchConversationsOperation(self)

    def get_conversation(self) -> "GetConversationOperation":
        """Bind the conversation detail Query."""
        from .operations import GetConversationOperation

        return GetConversationOperation(self)

    def reply_to_conversation(self) -> "ReplyToConversationOperation":
        """Bind the fenced reply Mutation."""
        from .operations import ReplyToConversationOperation

        return ReplyToConversationOperation(self)

    def update_conversation(self) -> "UpdateConversationOperation":
        """Bind the repeatable update Mutation."""
        from .operations import UpdateConversationOperation

        return UpdateConversationOperation(self)

    def find_customer_by_email(self) -> "FindCustomerByEmailOperation":
        """Bind the customer lookup Query."""
        from .operations import FindCustomerByEmailOperation

        return FindCustomerByEmailOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport after Worker shutdown."""
        await self.http_client.aclose()

    async def resolve_token(self, call: Call) -> tuple[str, str]:
        """Return token and error class: terminal, retry, or empty on success."""
        try:
            credentials = await self.credentials.resolve(call)
        except ReauthorizationRequiredError:
            return "", "terminal"
        except Exception:
            return "", "retry"
        if not isinstance(credentials, Credentials) or not is_header_safe_credential(
            credentials.access_token.reveal()
        ):
            return "", "unusable"
        return credentials.access_token.reveal(), ""

    async def exchange(
        self,
        call: Call,
        token: str,
        method: str,
        path: str,
        query: Mapping[str, str] | None = None,
        payload: object | None = None,
        *,
        allow_refresh: bool = True,
    ) -> Exchange:
        """Send once, refreshing a rejected token once when the source supports it."""
        target = _API_BASE + path
        if query:
            target += "?" + encode_query_values(query)
        body: bytes | None = None
        headers = {"Authorization": f"Bearer {token}", "Accept": "application/json"}
        if payload is not None:
            try:
                body = json.dumps(
                    payload, separators=(",", ":"), ensure_ascii=False, allow_nan=False
                ).encode()
            except (TypeError, ValueError):
                return Exchange(error="notBuilt")
            headers["Content-Type"] = "application/json; charset=UTF-8"
        for attempt in range(2):
            try:
                request = self.http_client.build_request(
                    method, target, headers=headers, content=body, timeout=25
                )
            except (ValueError, httpx.HTTPError):
                return Exchange(error="notBuilt")
            try:
                response, _ = await send_traced(self.http_client, request)
            except ProviderTransportError as error:
                return Exchange(error="beforeSend" if error.provably_not_sent else "afterSend")
            except (httpx.HTTPError, ValueError):
                return Exchange(error="transport")
            limit = (
                self.config.max_response_bytes if 200 <= response.status_code < 300 else MAX_ERROR_BODY_BYTES
            )
            try:
                contents = await read_bounded_body(response, limit)
            except BodyTooLargeError:
                if 200 <= response.status_code < 300:
                    return Exchange(response.status_code, response.headers, error="tooLarge", token=token)
                contents = b""
            except ProviderTransportError:
                if 200 <= response.status_code < 300:
                    return Exchange(response.status_code, response.headers, error="bodyRead", token=token)
                contents = b""
            exchange = Exchange(response.status_code, response.headers, contents, token=token)
            if 200 <= response.status_code < 300 and token.encode() in contents:
                return Exchange(response.status_code, response.headers, error="malformed", token=token)
            if (
                response.status_code != 401
                or not allow_refresh
                or attempt
                or not hasattr(self.credentials, "resolve_after_rejection")
            ):
                return exchange
            try:
                replacement = await self.credentials.resolve_after_rejection(call)
                token = replacement.access_token.reveal()
                if not is_header_safe_credential(token):
                    return exchange
                headers["Authorization"] = f"Bearer {token}"
            except Exception:
                return exchange
        return Exchange(error="transport")

    def receipt(self, call: Call, object_id: int = 0, request_id: str = "") -> Receipt:
        """Keep only stable provider correlation data in a Dex receipt."""
        return Receipt(
            call.id,
            "helpscout",
            datetime.now(UTC),
            call.idempotency_key,
            str(object_id) if object_id > 0 else "",
            request_id,
        )

    def classify_failure(self, operation: str, exchange: Exchange) -> tuple[bool, Failure, float, str]:
        """Classify non-2xx status and safe provider error tokens."""
        status = exchange.status
        log_ref, fields = _error_summary(exchange.body, exchange.token)
        suffix = " [" + ", ".join(fields) + "]" if fields else ""
        retry = False
        delay = 0.0
        if status == 429:
            kind, message, retry = (
                FailureKind.RATE_LIMIT,
                "Help Scout rate limited the request (HTTP 429)",
                True,
            )
            value = exchange.headers.get("X-RateLimit-Retry-After", "").strip()
            delay = (
                min(int(value), 3600)
                if _is_ascii_decimal(value) and int(value) > 0
                else parse_retry_after(exchange.headers.get("Retry-After", ""), datetime.now(UTC))
            )
        elif status == 408 or status >= 500:
            kind, message, retry = (
                FailureKind.AVAILABILITY,
                f"Help Scout could not complete the request yet (HTTP {status})",
                True,
            )
        elif status == 401:
            kind, message = (
                FailureKind.AUTHENTICATION,
                "Help Scout rejected the access token (HTTP 401); check that the app's owner is "
                "an active Help Scout user",
            )
        elif status == 403:
            kind, message = (
                FailureKind.AUTHORIZATION,
                "Help Scout denied access (HTTP 403), for example because the API is not enabled "
                "for the account, payment is missing, or the user cannot access the inbox" + suffix,
            )
        elif status in (404, 410):
            kind, message = FailureKind.NOT_FOUND, f"Help Scout found no such resource (HTTP {status})"
        elif 300 <= status < 400:
            kind, message = (
                FailureKind.PROTOCOL,
                f"Help Scout redirected the request (HTTP {status}), which the connector never follows",
            )
        elif status == 409:
            kind, message = (
                FailureKind.CONFLICT,
                "Help Scout reported a conflicting resource (HTTP 409)" + suffix,
            )
        elif status in (412, 423):
            kind, message = (
                FailureKind.PROVIDER_REJECTION,
                f"Help Scout refused to change a locked conversation or customer (HTTP {status}), "
                "such as one with 100 threads or one too old to update" + suffix,
            )
        elif status in (400, 422, 413, 415):
            kind, message = (
                FailureKind.VALIDATION,
                f"Help Scout rejected the request (HTTP {status})" + suffix,
            )
        else:
            kind, message = (
                FailureKind.PROVIDER_REJECTION,
                f"Help Scout rejected the request (HTTP {status})" + suffix,
            )
        return retry, Failure(kind, "helpscout", operation, message), delay, log_ref


def _error_summary(body: bytes, token: str) -> tuple[str, list[str]]:
    """Keep only validated log references and field/code identifiers."""
    try:
        document: Any = strict_json_loads(body)
        if not isinstance(document, dict):
            return "", []
        log_ref = document.get("logRef", "")
        if (
            not isinstance(log_ref, str)
            or not re.fullmatch(r"[A-Za-z0-9-]{1,64}", log_ref)
            or token
            and token in log_ref
        ):
            log_ref = ""
        embedded = document.get("_embedded")
        raw_errors = embedded.get("errors") if isinstance(embedded, dict) else None
        fields: list[str] = []
        if isinstance(raw_errors, list):
            for item in raw_errors:
                if len(fields) >= 5:
                    break
                if not isinstance(item, dict):
                    continue
                path = item.get("path")
                if (
                    not isinstance(path, str)
                    or not re.fullmatch(r"[A-Za-z][A-Za-z0-9_.\[\]]{0,63}", path)
                    or token
                    and token in path
                ):
                    continue
                code = ""
                links = item.get("_links")
                about = links.get("about") if isinstance(links, dict) else None
                href = about.get("href") if isinstance(about, dict) else None
                if isinstance(href, str):
                    fragment = href.partition("#")[2]
                    if re.fullmatch(r"[A-Za-z][A-Za-z0-9]{0,63}", fragment) and (
                        not token or token not in fragment
                    ):
                        code = "=" + fragment
                fields.append(path + code)
        return log_ref, fields
    except (ValueError, UnicodeError):
        return "", []
