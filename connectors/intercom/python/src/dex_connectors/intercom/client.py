# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Intercom API transport and response classification."""

from __future__ import annotations

import json
import re
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import TYPE_CHECKING, Any
from urllib.parse import quote

import httpx
from dex_connectors.sdk import Call, CredentialProvider, Failure, FailureKind, Receipt
from dex_connectors.sdk.providerhttp import (
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials

if TYPE_CHECKING:
    from .operations import (
        FindContactByEmailOperation,
        GetConversationOperation,
        ReplyToConversationOperation,
        SearchConversationsOperation,
        UpdateConversationStateOperation,
    )

_ENDPOINTS = {
    "us": "https://api.intercom.io",
    "eu": "https://api.eu.intercom.io",
    "au": "https://api.au.intercom.io",
}


@dataclass(frozen=True)
class Exchange:
    """One classified Intercom request with its safe response metadata."""

    outcome: str
    body: bytes = b""
    failure: Failure | None = None
    retry_after: timedelta = timedelta()
    request_id: str = ""


class Client:
    """Regional Intercom client with fresh credentials on each operation attempt."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
        endpoint: str | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Intercom configuration and credential provider are required")
        if config.region not in _ENDPOINTS:
            raise ValueError("Intercom region must be us, eu, or au")
        if config.max_response_bytes <= 0 or config.webhook_max_body_bytes <= 0:
            raise ValueError("Intercom response and webhook body limits must be positive")
        self.base_url = validate_base_url(endpoint or _ENDPOINTS[config.region])
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(9)
        self.now = now or (lambda: datetime.now(UTC))

    def search_conversations(self) -> SearchConversationsOperation:
        """Bind the conversation search Query."""
        from .operations import SearchConversationsOperation

        return SearchConversationsOperation(self)

    def get_conversation(self) -> GetConversationOperation:
        """Bind the conversation read Query."""
        from .operations import GetConversationOperation

        return GetConversationOperation(self)

    def reply_to_conversation(self) -> ReplyToConversationOperation:
        """Bind the reply Mutation."""
        from .operations import ReplyToConversationOperation

        return ReplyToConversationOperation(self)

    def update_conversation_state(self) -> UpdateConversationStateOperation:
        """Bind the state change Mutation."""
        from .operations import UpdateConversationStateOperation

        return UpdateConversationStateOperation(self)

    def find_contact_by_email(self) -> FindContactByEmailOperation:
        """Bind the contact lookup Query."""
        from .operations import FindContactByEmailOperation

        return FindContactByEmailOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport after Worker shutdown."""
        await self.http_client.aclose()

    def failure(self, operation: str, kind: FailureKind, message: str) -> Failure:
        """Construct a secret-safe failure with stable identity."""
        return Failure(kind, "intercom", operation, message)

    def receipt(self, call: Call, object_id: str = "", request_id: str = "") -> Receipt:
        """Record provider identity and observation time without response contents."""
        return Receipt(
            call_id=call.id,
            provider="intercom",
            observed_at=self.now().astimezone(UTC),
            idempotency_key=call.idempotency_key,
            provider_object_id=object_id,
            provider_request_id=request_id,
        )

    async def resolve_credentials(
        self, call: Call, operation: str
    ) -> tuple[Credentials | None, Failure | None]:
        """Resolve and validate the access token for one attempt."""
        try:
            credentials = await self.credentials.resolve(call)
            if not is_header_safe_credential(credentials.access_token.reveal()):
                raise ValueError("invalid token")
            return credentials, None
        except (ValueError, LookupError, OSError, AttributeError, TypeError):
            return None, self.failure(
                operation, FailureKind.AUTHENTICATION, "connection credentials are unavailable"
            )

    async def exchange(
        self,
        call: Call,
        credentials: Credentials,
        operation: str,
        method: str,
        path: str,
        payload: Any = None,
        query: dict[str, str] | None = None,
    ) -> Exchange:
        """Send once; classify status separately from each operation's retry policy."""
        try:
            headers = {
                "Authorization": "Bearer " + credentials.access_token.reveal(),
                "Accept": "application/json",
                "Intercom-Version": "2.16",
            }
            if payload is not None:
                headers["Content-Type"] = "application/json"
            request = self.http_client.build_request(
                method,
                self.base_url + path,
                params=query,
                content=json.dumps(payload, separators=(",", ":"), ensure_ascii=False).encode()
                if payload is not None
                else None,
                headers=headers,
            )
        except (TypeError, ValueError, httpx.HTTPError):
            return Exchange(
                "defect",
                failure=self.failure(
                    operation, FailureKind.LOCAL_DEFECT, "Intercom request could not be built"
                ),
            )
        try:
            response = await self.http_client.send(request, stream=True)
        except httpx.ConnectError:
            return Exchange(
                "notSent",
                failure=self.failure(
                    operation, FailureKind.TRANSPORT, "Intercom could not be reached; no request was sent"
                ),
            )
        except httpx.HTTPError:
            return Exchange(
                "unconfirmed",
                failure=self.failure(
                    operation, FailureKind.TRANSPORT, "Intercom request failed before a response arrived"
                ),
            )
        try:
            chunks = bytearray()
            async for chunk in response.aiter_bytes(chunk_size=min(self.max_response_bytes + 1, 65536)):
                remaining = self.max_response_bytes + 1 - len(chunks)
                chunks.extend(chunk[:remaining])
                if len(chunks) > self.max_response_bytes:
                    break
            body = bytes(chunks)
        except httpx.HTTPError:
            return Exchange(
                "unconfirmed",
                failure=self.failure(operation, FailureKind.TRANSPORT, "Intercom response could not be read"),
            )
        finally:
            await response.aclose()
        status = response.status_code
        if 200 <= status < 300:
            if len(body) > self.max_response_bytes:
                return Exchange(
                    "invalid",
                    failure=self.failure(
                        operation,
                        FailureKind.RESPONSE_TOO_LARGE,
                        "Intercom response exceeds the configured maxResponseBytes limit",
                    ),
                )
            if credentials.access_token.reveal().encode() in body:
                return Exchange(
                    "invalid",
                    failure=self.failure(
                        operation,
                        FailureKind.PROTOCOL,
                        "Intercom response reflected the connection credential",
                    ),
                )
            return Exchange("succeeded", body=body)
        body = body[: self.max_response_bytes]
        summary, request_id = _error_summary(body)

        def with_summary(message: str) -> str:
            return message + (f" [{summary}]" if summary else "")

        if status == 429:
            retry_after = timedelta(
                seconds=parse_retry_after(response.headers.get("Retry-After", ""), self.now())
            )
            if retry_after <= timedelta():
                try:
                    reset = int(response.headers.get("X-RateLimit-Reset", ""))
                    retry_after = (
                        max(timedelta(seconds=1), datetime.fromtimestamp(reset, UTC) - self.now())
                        if reset > 0
                        else timedelta()
                    )
                except (ValueError, OverflowError, OSError):
                    retry_after = timedelta()
            return Exchange(
                "rateLimited",
                failure=self.failure(
                    operation,
                    FailureKind.RATE_LIMIT,
                    with_summary("Intercom rate limited the request (HTTP 429)"),
                ),
                retry_after=min(retry_after, timedelta(minutes=1)),
                request_id=request_id,
            )
        if status == 408 or status >= 500:
            return Exchange(
                "unconfirmed",
                failure=self.failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    with_summary(f"Intercom could not complete the request (HTTP {status})"),
                ),
                request_id=request_id,
            )
        if status in (404, 410):
            return Exchange(
                "notFound",
                failure=self.failure(
                    operation,
                    FailureKind.NOT_FOUND,
                    with_summary(f"Intercom found no such resource (HTTP {status})"),
                ),
                request_id=request_id,
            )
        if 300 <= status < 400:
            kind = FailureKind.PROTOCOL
            message = f"Intercom redirected the request (HTTP {status}); check the region configuration"
        elif status == 401:
            kind = FailureKind.AUTHENTICATION
            message = with_summary(
                "Intercom rejected the access token (HTTP 401); check the token and that the region "
                "matches the workspace"
            )
        else:
            kind = {
                403: FailureKind.AUTHORIZATION,
                409: FailureKind.CONFLICT,
                400: FailureKind.VALIDATION,
                422: FailureKind.VALIDATION,
            }.get(status, FailureKind.PROVIDER_REJECTION)
            message = with_summary(f"Intercom rejected the request (HTTP {status})")
        return Exchange("rejected", failure=self.failure(operation, kind, message), request_id=request_id)

    async def read_conversation(
        self, call: Call, credentials: Credentials, operation: str, conversation_id: str
    ) -> Exchange:
        """Read one conversation with its parts rendered as plain text."""
        return await self.exchange(
            call,
            credentials,
            operation,
            "GET",
            "/conversations/" + quote(conversation_id, safe="-._~$&+=:@"),
            query={"display_as": "plaintext"},
        )


def _error_summary(body: bytes) -> tuple[str, str]:
    try:
        document = json.loads(body)
    except (UnicodeDecodeError, ValueError):
        return "", ""
    if not isinstance(document, dict):
        return "", ""
    request_id = document.get("request_id", "")
    entries = document.get("errors")
    if not isinstance(request_id, str) or (entries is not None and not isinstance(entries, list)):
        return "", ""
    if not re.fullmatch(r"[A-Za-z0-9-]{1,64}", request_id):
        request_id = ""
    codes: list[str] = []
    fields: list[str] = []
    for entry in entries or []:
        if len(codes) == 3:
            break
        if entry is None:
            entry = {}
        if not isinstance(entry, dict):
            return "", ""
        code = entry.get("code", "")
        field = entry.get("field", "")
        if not isinstance(code, str) or not isinstance(field, str):
            return "", ""
        if re.fullmatch(r"[a-z][a-z0-9_.]{0,63}", code):
            codes.append(code)
        if re.fullmatch(r"[a-z][a-z0-9_.]{0,63}", field):
            fields.append(field)
    pieces = []
    if codes:
        pieces.append(", ".join(codes))
    if fields:
        pieces.append("fields: " + ", ".join(fields))
    return "; ".join(pieces), request_id
