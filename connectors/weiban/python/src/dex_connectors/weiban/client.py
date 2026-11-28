# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Weiban token and tag transport with Go-compatible query decisions."""

import json
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
from ._generated.definitions import LIST_TAGS_DEFINITION
from .models import ListTagsInput, TagMetadata, TagsResult


def _credential(value: object) -> bool:
    return (
        isinstance(value, str)
        and 0 < len(value.encode("utf-8", "replace")) <= 2048
        and value.strip() == value
    )


class Client:
    """Resolve Weiban credentials for each tag query."""

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
            or config.max_response_bytes <= 0
        ):
            raise ValueError("Weiban configuration and credentials are required")
        self.base_url = validate_base_url(config.base_url or "https://open.weibanzhushou.com")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None

    def list_tags(self) -> "ListTagsOperation":
        """Bind the tag-list query."""
        return ListTagsOperation(self)

    async def aclose(self) -> None:
        """Close the owned transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class ListTagsOperation:
    """Acquire an access token and read the first Weiban tag page."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated query definition."""
        return LIST_TAGS_DEFINITION

    def _failure(self, kind: FailureKind) -> Failure:
        return Failure(kind, "weiban", "listTags", "Weiban request failed")

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[TagsResult]:
        return QueryAttempt.branch_result(branch, TagsResult(), Receipt(), self._failure(kind))

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[TagsResult]:
        return QueryAttempt.retry(self._failure(kind), delay)

    async def _response(self, request: httpx.Request) -> bytes | QueryAttempt[TagsResult]:
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
            try:
                body = await read_bounded_body(response, self.client.config.max_response_bytes)
            except BodyTooLargeError:
                return self._branch("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
            except ProviderTransportError:
                return self._retry(FailureKind.AVAILABILITY)
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.VALIDATION
                    if status in (400, 404, 412)
                    else FailureKind.PROVIDER_REJECTION
                )
                return self._branch("providerRejected", kind)
            return body
        finally:
            await response.aclose()

    async def invoke(self, call: Call, input: ListTagsInput) -> QueryAttempt[TagsResult]:
        """Read tag metadata without persisting credentials or access tokens."""
        if not isinstance(input, ListTagsInput):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        if not _credential(credentials.corp_id) or not _credential(credentials.secret.reveal()):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        token_request = self.client.http_client.build_request(
            "POST",
            self.client.base_url + "/open-api/access_token/get",
            content=json.dumps(
                {"corp_id": credentials.corp_id, "secret": credentials.secret.reveal()}, separators=(",", ":")
            ),
            headers={"Accept": "application/json", "Content-Type": "application/json"},
        )
        token_body = await self._response(token_request)
        if isinstance(token_body, QueryAttempt):
            return token_body
        try:
            token_wire = json.loads(token_body)
        except (ValueError, UnicodeDecodeError):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if not isinstance(token_wire, dict) or type(token_wire.get("errcode")) is not int:
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if token_wire["errcode"] != 0:
            if token_wire["errcode"] in (500, 10051, 10052, 10053):
                return self._retry(FailureKind.AVAILABILITY)
            return self._branch("providerRejected", FailureKind.AUTHENTICATION)
        if (
            not _credential(token_wire.get("access_token"))
            or type(token_wire.get("expires_in")) is not int
            or token_wire["expires_in"] <= 0
        ):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        query = urlencode(
            sorted({"access_token": token_wire["access_token"], "limit": 10, "offset": 0}.items())
        )
        request = self.client.http_client.build_request(
            "GET",
            self.client.base_url + "/open-api/tag/list?" + query,
            headers={"Accept": "application/json"},
        )
        body = await self._response(request)
        if isinstance(body, QueryAttempt):
            return body
        try:
            wire = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if not isinstance(wire, dict) or type(wire.get("errcode")) is not int:
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if wire["errcode"] != 0:
            if wire["errcode"] in (500, 10051, 10052, 10053):
                return self._retry(FailureKind.AVAILABILITY)
            return self._branch("providerRejected", FailureKind.PROVIDER_REJECTION)
        total = wire.get("total")
        items = wire.get("tag_list")
        if (
            type(total) is not int
            or total < 0
            or not isinstance(items, list)
            or len(items) > 100
            or len(items) > total
        ):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        tags = []
        for item in items[:10]:
            if not isinstance(item, dict):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            tag_id = item.get("tag_id")
            name = "" if item.get("tag_name") is None else item["tag_name"]
            group = "" if item.get("tag_group") is None else item["tag_group"]
            if (
                not isinstance(tag_id, str)
                or not tag_id
                or len(tag_id.encode("utf-8", "replace")) > 128
                or not isinstance(name, str)
                or len(name.encode("utf-8", "replace")) > 512
                or not isinstance(group, str)
                or len(group.encode("utf-8", "replace")) > 512
            ):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            tags.append(TagMetadata(id=tag_id, name=name, group=group))
        return QueryAttempt.branch_result("listed", TagsResult(total=total, tags=tuple(tags)), Receipt())
