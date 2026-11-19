# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""PingCAP TiDB Cloud project-list Query with HTTP Digest auth."""

from __future__ import annotations

import hashlib
import json
import os
import re
from collections.abc import Callable
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
from ._generated.definitions import LIST_PROJECTS_DEFINITION
from .models import ListProjectsInput, ListProjectsOutput, ProjectInfo


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListProjectsOutput]:
    return QueryAttempt.branch_result(
        branch, ListProjectsOutput(), Receipt(), Failure(kind, "pingcap", "listProjects", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListProjectsOutput]:
    return QueryAttempt.retry(Failure(kind, "pingcap", "listProjects", message), delay)


def _valid(value: str) -> bool:
    return (
        bool(value) and len(value) <= 512 and value.strip() == value and all(32 < ord(c) < 127 for c in value)
    )


def _parse_challenge(header: str) -> dict[str, str]:
    if len(header) > 8192 or not header.lower().startswith("digest "):
        raise ValueError("invalid Digest challenge")
    values: dict[str, str] = {}
    text = header[7:]
    pattern = re.compile(r'\s*([A-Za-z_-]+)\s*=\s*(?:"((?:\\.|[^"\\])*)"|([^,\s]+))\s*(?:,|$)')
    while text:
        match = pattern.match(text)
        if match is None:
            raise ValueError("invalid Digest parameter")
        name = match.group(1).lower()
        value = re.sub(r"\\(.)", r"\1", match.group(2)) if match.group(2) is not None else match.group(3)
        if (
            not value
            or len(value) > 2048
            or any(ord(c) < 32 or ord(c) == 127 for c in value)
            or name in values
        ):
            raise ValueError("invalid Digest value")
        values[name] = value
        text = text[match.end() :]
    if not values.get("realm") or not values.get("nonce") or values.get("stale", "").lower() == "true":
        raise ValueError("incomplete Digest challenge")
    algorithm = values.get("algorithm", "MD5")
    if algorithm.upper() not in ("MD5", "MD5-SESS", "SHA-256", "SHA-256-SESS") or "auth" not in values.get(
        "qop", ""
    ).split(","):
        raise ValueError("unsupported Digest challenge")
    values["algorithm"] = algorithm
    return values


def _authorization(
    challenge: dict[str, str], username: str, password: str, uri: str, nonce_source: Callable[[int], bytes]
) -> str:
    random_bytes = nonce_source(16)
    if len(random_bytes) != 16:
        raise ValueError("invalid Digest client nonce")
    cnonce = random_bytes.hex()
    algorithm = challenge["algorithm"]

    def digest(value: str) -> str:
        data = value.encode()
        return (
            hashlib.sha256(data).hexdigest()
            if algorithm.upper().startswith("SHA-256")
            else hashlib.md5(data).hexdigest()
        )

    realm, nonce = challenge["realm"], challenge["nonce"]
    ha1 = digest(f"{username}:{realm}:{password}")
    if algorithm.upper().endswith("-SESS"):
        ha1 = digest(f"{ha1}:{nonce}:{cnonce}")
    ha2 = digest(f"GET:{uri}")
    response = digest(f"{ha1}:{nonce}:00000001:{cnonce}:auth:{ha2}")

    def quoted(value: str) -> str:
        return '"' + value.replace("\\", "\\\\").replace('"', '\\"') + '"'

    header = (
        f"Digest username={quoted(username)}, realm={quoted(realm)}, nonce={quoted(nonce)}, "
        f"uri={quoted(uri)}, response={quoted(response)}, algorithm={algorithm}, "
        f"qop=auth, nc=00000001, cnonce={quoted(cnonce)}"
    )
    if challenge.get("opaque"):
        header += f", opaque={quoted(challenge['opaque'])}"
    return header


class Client:
    """Own TiDB Cloud settings and resolve credentials per Query attempt."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        nonce_source: Callable[[int], bytes] | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or config.max_response_bytes <= 0
        ):
            raise ValueError("valid PingCAP configuration and credentials are required")
        self.base_url = validate_base_url(config.base_url)
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.nonce_source = nonce_source or os.urandom

    def list_projects(self) -> ListProjectsOperation:
        """Bind the project-list Query to this client."""
        return ListProjectsOperation(self)

    async def aclose(self) -> None:
        """Close the owned transport."""
        if self._owns_client:
            await self.http_client.aclose()


class ListProjectsOperation:
    """Read a bounded page of TiDB Cloud projects."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("PingCAP client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and retry policy."""
        return LIST_PROJECTS_DEFINITION

    async def invoke(self, call: Call, input: ListProjectsInput) -> QueryAttempt[ListProjectsOutput]:
        """Complete one Digest challenge and classify the provider response."""
        page, page_size = input.page or 1, input.page_size or 10
        if page < 1 or page_size < 1 or page_size > 100:
            return _branch("defect", FailureKind.VALIDATION, "page or pageSize is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            username, password = credentials.public_key.reveal(), credentials.private_key.reveal()
            if not _valid(username) or not _valid(password):
                raise ValueError("invalid credentials")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "TiDB Cloud credentials are invalid")
        endpoint = f"{self.client.base_url}/api/v1beta/projects"
        params = {"page": str(page), "page_size": str(page_size)}
        try:
            request = self.client.http_client.build_request(
                "GET", endpoint, params=params, headers={"Accept": "application/json"}
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (httpx.HTTPError, ValueError, ProviderTransportError):
            return _retry(FailureKind.AVAILABILITY, "TiDB Cloud request failed")
        if response.status_code == 401:
            challenge_header = response.headers.get("WWW-Authenticate", "")
            await response.aclose()
            try:
                challenge = _parse_challenge(challenge_header)
            except ValueError:
                return _branch("providerRejected", FailureKind.AUTHENTICATION, "unsupported Digest challenge")
            try:
                authorization = _authorization(
                    challenge,
                    username,
                    password,
                    str(request.url.raw_path.decode()),
                    self.client.nonce_source,
                )
            except (ValueError, OSError):
                return _branch("defect", FailureKind.AVAILABILITY, "Digest authorization unavailable")
            try:
                request = self.client.http_client.build_request(
                    "GET",
                    endpoint,
                    params=params,
                    headers={"Accept": "application/json", "Authorization": authorization},
                )
                response, _ = await send_traced(self.client.http_client, request)
            except (httpx.HTTPError, ValueError, ProviderTransportError):
                return _retry(FailureKind.AVAILABILITY, "TiDB Cloud request failed")
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "TiDB Cloud rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "TiDB Cloud is unavailable", delay)
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch("providerRejected", kind, f"TiDB Cloud rejected HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "TiDB Cloud response exceeded the limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "TiDB Cloud response was interrupted")
        finally:
            await response.aclose()
        try:
            wire: Any = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "TiDB Cloud returned invalid JSON")
        items = wire.get("items") if isinstance(wire, dict) else None
        if (
            not isinstance(items, list)
            or type(wire.get("total")) is not int
            or wire["total"] < 0
            or len(items) > page_size
        ):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "TiDB Cloud returned invalid project page"
            )
        projects: list[ProjectInfo] = []
        for item in items:
            if (
                not isinstance(item, dict)
                or any(
                    not isinstance(item.get(name), str) or not item[name] for name in ("id", "org_id", "name")
                )
                or any(
                    type(item.get(name, 0)) is not int or item.get(name, 0) < 0
                    for name in ("cluster_count", "user_count")
                )
            ):
                return _branch("invalidResponse", FailureKind.PROTOCOL, "TiDB Cloud returned invalid project")
            projects.append(
                ProjectInfo(
                    id=item["id"],
                    org_id=item["org_id"],
                    name=item["name"],
                    cluster_count=item.get("cluster_count", 0),
                    user_count=item.get("user_count", 0),
                    create_timestamp=item.get("create_timestamp", ""),
                    type=item.get("type", ""),
                )
            )
        return QueryAttempt.branch_result(
            "listed",
            ListProjectsOutput(items=tuple(projects), total=wire["total"], page=page, page_size=page_size),
            Receipt(),
        )
