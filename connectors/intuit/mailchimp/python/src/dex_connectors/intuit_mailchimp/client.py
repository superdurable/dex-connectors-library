# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded, redirect-safe Mailchimp Marketing API client."""

import json
import re
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import TYPE_CHECKING, Any
from urllib.parse import urlencode

import httpx
from dex_connectors.sdk import Call, CredentialProvider, Failure, FailureKind, Receipt
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

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials

if TYPE_CHECKING:
    from .operations import (
        GetMemberOperation,
        ListMembersOperation,
        SendCampaignOperation,
        UpdateMemberTagsOperation,
        UpsertMemberOperation,
    )

_API_KEY = re.compile(r"[A-Za-z0-9]{8,128}-([a-z]{2,10}[0-9]{1,4})\Z")
_REQUEST_ID = re.compile(r"[A-Za-z0-9._:-]{1,128}\Z")
_ERROR_TITLE = re.compile(r"[A-Za-z][A-Za-z ]{0,78}[A-Za-z]\Z")
_ERROR_FIELD = re.compile(r"[A-Za-z0-9_.\[\]-]{1,64}\Z")


@dataclass(frozen=True, slots=True)
class Exchange:
    """One classified provider request and its secret-safe metadata."""

    outcome: str
    failure: Failure | None = None
    retry_after: float = 0
    status: int = 0
    request_id: str = ""
    body: bytes = b""


def failure(kind: FailureKind, operation: str, message: str) -> Failure:
    """Create a secret-safe Mailchimp operation failure."""
    return Failure(kind, "mailchimp", operation, message)


class Client:
    """Call Mailchimp with a fresh private key for every request.

    A supplied HTTP client remains caller owned. The connector closes only its
    internally created client after all Steps stop.
    """

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        api_base_url: str = "",
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Mailchimp configuration and credentials are required")
        if config.max_response_bytes < 1:
            raise ValueError("Mailchimp response limit must be positive")
        self.config = config
        self._credentials = credentials
        self._api_base_url = validate_base_url(api_base_url) if api_base_url else ""
        self._http_client = http_client or new_provider_http_client(25)
        self._owns_http_client = http_client is None
        self._now = now or (lambda: datetime.now(UTC))

    async def aclose(self) -> None:
        """Close the connector-owned transport after all calls finish."""
        if self._owns_http_client:
            await self._http_client.aclose()

    def get_member(self) -> "GetMemberOperation":
        """Bind the getMember Query."""
        from .operations import GetMemberOperation

        return GetMemberOperation(self)

    def list_members(self) -> "ListMembersOperation":
        """Bind the listMembers Query."""
        from .operations import ListMembersOperation

        return ListMembersOperation(self)

    def upsert_member(self) -> "UpsertMemberOperation":
        """Bind the upsertMember Mutation."""
        from .operations import UpsertMemberOperation

        return UpsertMemberOperation(self)

    def update_member_tags(self) -> "UpdateMemberTagsOperation":
        """Bind the updateMemberTags Mutation."""
        from .operations import UpdateMemberTagsOperation

        return UpdateMemberTagsOperation(self)

    def send_campaign(self) -> "SendCampaignOperation":
        """Bind the sendCampaign Mutation."""
        from .operations import SendCampaignOperation

        return SendCampaignOperation(self)

    async def resolve_credentials(self, call: Call, operation: str) -> tuple[str, str] | Failure:
        """Resolve and validate a Mailchimp key without returning it in failures."""
        try:
            credentials = await self._credentials.resolve(call)
            key = credentials.api_key.reveal()
            match = _API_KEY.fullmatch(key)
            if not is_header_safe_credential(key) or match is None:
                raise ValueError("invalid Mailchimp API key")
            return key, self._api_base_url or f"https://{match.group(1)}.api.mailchimp.com/3.0"
        except Exception:
            return failure(FailureKind.AUTHENTICATION, operation, "connection credentials are unavailable")

    async def exchange(
        self,
        call: Call,
        operation: str,
        connection: tuple[str, str],
        method: str,
        path: str,
        *,
        query: dict[str, str] | None = None,
        payload: dict[str, Any] | None = None,
    ) -> Exchange:
        """Issue one request and classify transport, status, and bounded body."""
        key, base_url = connection
        target = base_url + path
        if query:
            target += "?" + urlencode(sorted(query.items()))
        try:
            content = (
                json.dumps(payload, separators=(",", ":"), ensure_ascii=False).encode()
                if payload is not None
                else None
            )
            headers = {"Authorization": "Bearer " + key, "Accept": "application/json"}
            if content is not None:
                headers["Content-Type"] = "application/json"
            request = self._http_client.build_request(method, target, headers=headers, content=content)
            response, _ = await send_traced(self._http_client, request)
            status = response.status_code
            response_headers = response.headers.copy()
            try:
                body = await read_bounded_body(response, self.config.max_response_bytes)
            except BodyTooLargeError:
                if 200 <= status < 300:
                    return Exchange(
                        "invalid",
                        failure(
                            FailureKind.RESPONSE_TOO_LARGE,
                            operation,
                            "Mailchimp response exceeds the configured maxResponseBytes limit",
                        ),
                        status=status,
                    )
                body = b""
            request_id = response_headers.get("X-Request-Id", "")
            request_id = request_id if _REQUEST_ID.fullmatch(request_id) else ""
            if 200 <= status < 300:
                if key.encode() in body:
                    return Exchange(
                        "invalid",
                        failure(
                            FailureKind.PROTOCOL,
                            operation,
                            "Mailchimp response reflected the connection credential",
                        ),
                        status=status,
                        request_id=request_id,
                    )
                return Exchange("succeeded", status=status, request_id=request_id, body=body)
            return self._classify_status(operation, status, body, response_headers, request_id, key)
        except ProviderTransportError as error:
            outcome = "notSent" if error.provably_not_sent else "unavailable"
            message = (
                "Mailchimp could not be reached; no request was sent"
                if error.provably_not_sent
                else "Mailchimp request failed before a response arrived"
            )
            return Exchange(outcome, failure(FailureKind.TRANSPORT, operation, message))
        except (ValueError, TypeError):
            return Exchange(
                "defect", failure(FailureKind.LOCAL_DEFECT, operation, "Mailchimp request could not be built")
            )

    def _classify_status(
        self,
        operation: str,
        status: int,
        body: bytes,
        headers: httpx.Headers,
        request_id: str,
        key: str,
    ) -> Exchange:
        title = ""
        fields: list[str] = []
        has_document = False
        try:
            document = json.loads(body)
            if isinstance(document, dict) and isinstance(document.get("title"), str):
                has_document = True
                candidate = document["title"]
                if _ERROR_TITLE.fullmatch(candidate) and key not in candidate:
                    title = candidate
                if isinstance(document.get("errors"), list):
                    for error in document["errors"]:
                        if isinstance(error, dict) and isinstance(error.get("field"), str):
                            field = error["field"]
                            if _ERROR_FIELD.fullmatch(field) and key not in field:
                                fields.append(field)
        except (ValueError, UnicodeDecodeError):
            pass
        summary = []
        if title:
            summary.append(title)
        if fields:
            summary.append("fields: " + ", ".join(sorted(fields)[:5]))
        suffix = " [" + "; ".join(summary) + "]" if summary else ""
        retry_after = parse_retry_after(headers.get("Retry-After", ""), self._now())
        if status == 429:
            return Exchange(
                "rateLimited",
                failure(
                    FailureKind.RATE_LIMIT, operation, "Mailchimp throttled the request (HTTP 429)" + suffix
                ),
                retry_after,
                status,
                request_id,
            )
        if status == 403 and not has_document:
            return Exchange(
                "rateLimited",
                failure(
                    FailureKind.RATE_LIMIT,
                    operation,
                    "Mailchimp throttled the request (HTTP 403 without an error document)",
                ),
                retry_after,
                status,
                request_id,
            )
        if status == 408 or status >= 500:
            return Exchange(
                "unavailable",
                failure(
                    FailureKind.AVAILABILITY,
                    operation,
                    f"Mailchimp could not complete the request (HTTP {status})" + suffix,
                ),
                retry_after,
                status,
                request_id,
            )
        if status == 404:
            return Exchange(
                "notFound",
                failure(
                    FailureKind.NOT_FOUND, operation, "Mailchimp found no such resource (HTTP 404)" + suffix
                ),
                status=status,
                request_id=request_id,
            )
        if 300 <= status < 400:
            return Exchange(
                "rejected",
                failure(
                    FailureKind.PROTOCOL,
                    operation,
                    f"Mailchimp redirected the request (HTTP {status}); redirects are never followed",
                ),
                status=status,
                request_id=request_id,
            )
        kind = {
            401: FailureKind.AUTHENTICATION,
            403: FailureKind.AUTHORIZATION,
            409: FailureKind.CONFLICT,
            400: FailureKind.VALIDATION,
        }.get(status, FailureKind.PROVIDER_REJECTION)
        return Exchange(
            "rejected",
            failure(kind, operation, f"Mailchimp rejected the request (HTTP {status})" + suffix),
            status=status,
            request_id=request_id,
        )

    def receipt(self, call: Call, exchange: Exchange, object_id: str) -> Receipt:
        """Correlate one call and provider object without persisting credential data."""
        return Receipt(
            call_id=call.id,
            idempotency_key=call.idempotency_key,
            provider="mailchimp",
            provider_object_id=object_id,
            provider_request_id=exchange.request_id,
            observed_at=self._now().astimezone(UTC),
        )
