# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""JINGdigital ordinary tag transport and Go-compatible decisions."""

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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_TAGS_DEFINITION
from .models import ListTagsInput, ListTagsOutput, TagSummary


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListTagsOutput]:
    return QueryAttempt.branch_result(
        branch, ListTagsOutput(), Receipt(), Failure(kind, "jingdigital", "listTags", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListTagsOutput]:
    return QueryAttempt.retry(Failure(kind, "jingdigital", "listTags", message), delay)


def _is_valid_credential(value: str) -> bool:
    return bool(value) and len(value) <= 2048 and all(0x21 <= ord(char) <= 0x7E for char in value)


class Client:
    """Own a redirect-safe JINGdigital transport and resolve credentials per Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("JINGdigital configuration and credential source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("JINGdigital timeout or response limit is invalid")
        self.base_url = validate_base_url(config.base_url or "https://jingopenapi.jingsocial.com")
        if self.base_url != "https://jingopenapi.jingsocial.com":
            raise ValueError("JINGdigital base URL must be the documented HTTPS production origin")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_tags(self) -> "ListTagsOperation":
        """Bind the tag-list Query to this client."""
        return ListTagsOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class ListTagsOperation:
    """Read one page of ordinary tag metadata without persisting token material."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("JINGdigital client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated tag-list Query identity."""
        return LIST_TAGS_DEFINITION

    async def invoke(self, call: Call, input: ListTagsInput) -> QueryAttempt[ListTagsOutput]:
        """Exchange ephemeral credentials and read one bounded ordinary-tag page."""
        try:
            credentials = await self.client.credentials.resolve(call)
            app_id = credentials.app_id.reveal()
            app_secret = credentials.app_secret.reveal()
            if not _is_valid_credential(app_id) or not _is_valid_credential(app_secret):
                raise ValueError("invalid credential")
        except (ValueError, LookupError, OSError):
            return _branch("defect", FailureKind.AUTHENTICATION, "JINGdigital credentials unavailable")
        page = input.page or 1
        page_size = input.page_size or 20
        if not 1 <= page <= 1000000 or not 1 <= page_size <= 100:
            return _branch(
                "defect", FailureKind.VALIDATION, "page or pageSize is outside the supported range"
            )
        auth_request = self.client.http_client.build_request(
            "POST",
            self.client.base_url + "/v1/oauth2/token",
            headers={"Content-Type": "application/json", "Accept": "application/json"},
            json={"app_id": app_id, "app_secret": app_secret},
        )
        auth_result = await self._send(auth_request, "authorization")
        if isinstance(auth_result, QueryAttempt):
            return auth_result
        auth_wire = _decode_one(auth_result)
        if not isinstance(auth_wire, dict) or type(auth_wire.get("code")) is not int:
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "JINGdigital returned invalid authorization JSON"
            )
        if auth_wire["code"] != 0:
            return _provider_failure(auth_wire["code"], "authorization")
        auth_data = auth_wire.get("data")
        if not isinstance(auth_data, dict) or not isinstance(auth_data.get("access_token"), str):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "JINGdigital returned invalid authorization data"
            )
        token = auth_data["access_token"]
        if not _is_valid_credential(token):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "JINGdigital returned invalid authorization data"
            )
        tag_request = self.client.http_client.build_request(
            "POST",
            self.client.base_url + "/v1/tag/select/list",
            headers={
                "Content-Type": "application/json",
                "Accept": "application/json",
                "app_id": app_id,
                "access_token": token,
            },
            json={"tag_ids": [], "level": 1, "page": page, "perPage": page_size},
        )
        tag_result = await self._send(tag_request, "tag")
        if isinstance(tag_result, QueryAttempt):
            return tag_result
        return _decode_tags(tag_result, page, page_size)

    async def _send(self, request: httpx.Request, stage: str) -> bytes | QueryAttempt[ListTagsOutput]:
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, f"JINGdigital {stage} transport failed")
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, f"JINGdigital {stage} request rate limited", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, f"JINGdigital {stage} temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    f"JINGdigital {stage} response too large",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, f"JINGdigital {stage} response interrupted")
            if status != 200:
                kind = FailureKind.AUTHENTICATION if status in {401, 403} else FailureKind.PROVIDER_REJECTION
                return _branch("providerRejected", kind, f"JINGdigital rejected {stage} with HTTP {status}")
            return body
        finally:
            await response.aclose()


def _decode_one(body: bytes) -> Any:
    try:
        return json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return None


def _provider_failure(code: int, stage: str) -> QueryAttempt[ListTagsOutput]:
    if code in {20000, 40005}:
        return _retry(FailureKind.AVAILABILITY, f"JINGdigital {stage} temporarily unavailable")
    if code == 20002:
        return _retry(FailureKind.RATE_LIMIT, f"JINGdigital {stage} request rate limited")
    kind = FailureKind.AUTHENTICATION if code in {20001, 40006} else FailureKind.PROVIDER_REJECTION
    return _branch("providerRejected", kind, f"JINGdigital rejected {stage} with code {code}")


def _decode_tags(body: bytes, page: int, page_size: int) -> QueryAttempt[ListTagsOutput]:
    wire = _decode_one(body)
    if not isinstance(wire, dict) or type(wire.get("code")) is not int:
        return _branch("invalidResponse", FailureKind.PROTOCOL, "JINGdigital returned invalid tag JSON")
    if wire["code"] != 0:
        return _provider_failure(wire["code"], "tag")
    data = wire.get("data")
    if not isinstance(data, dict):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "JINGdigital returned invalid tag page")
    items, meta = data.get("items"), data.get("_meta")
    if not isinstance(items, list) or not isinstance(meta, dict) or len(items) > page_size:
        return _branch("invalidResponse", FailureKind.PROTOCOL, "JINGdigital returned invalid tag page")
    total = 0 if meta.get("totalCount") is None else meta["totalCount"]
    current_page = meta.get("currentPage")
    per_page = meta.get("perPage")
    if (
        type(total) is not int
        or total < 0
        or type(current_page) is not int
        or current_page != page
        or type(per_page) is not int
        or per_page < 1
    ):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "JINGdigital returned invalid tag page")
    tags: list[TagSummary] = []
    for item in items:
        if not isinstance(item, dict):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "JINGdigital returned invalid tag metadata"
            )
        tag_id, name, level = item.get("id"), item.get("name"), item.get("level")
        if level is None:
            level = 0
        if (
            type(tag_id) is not int
            or tag_id <= 0
            or not isinstance(name, str)
            or not name
            or len(name.encode("utf-8", "replace")) > 512
            or type(level) is not int
            or not 0 <= level <= 10
        ):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "JINGdigital returned invalid tag metadata"
            )
        tags.append(TagSummary(id=tag_id, name=name, level=level))
    next_page = 0
    if page * per_page < total:
        if not tags or page == 1000000:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "JINGdigital pagination cannot advance")
        next_page = page + 1
    return QueryAttempt.branch_result(
        "listed", ListTagsOutput(tags=tuple(tags), total=total, next_page=next_page), Receipt()
    )
