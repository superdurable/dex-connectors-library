# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""OceanBase Cloud Digest-authenticated instance Query."""

import hashlib
import json
import re
import secrets
from collections.abc import Callable
from datetime import UTC, datetime
from typing import Any
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
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_INSTANCES_DEFINITION
from .models import InstanceInfo, ListInstancesInput, ListInstancesOutput


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListInstancesOutput]:
    return QueryAttempt.branch_result(
        branch, ListInstancesOutput(), Receipt(), Failure(kind, "oceanbase", "listInstances", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListInstancesOutput]:
    return QueryAttempt.retry(Failure(kind, "oceanbase", "listInstances", message), delay)


def _valid_secret(value: str) -> bool:
    return bool(value) and len(value) <= 512 and all(0x20 < ord(character) < 0x7F for character in value)


def _request_id(value: bytes) -> str:
    octets = bytearray(value)
    octets[6] = (octets[6] & 0x0F) | 0x40
    octets[8] = (octets[8] & 0x3F) | 0x80
    encoded = octets.hex()
    return f"{encoded[:8]}-{encoded[8:12]}-{encoded[12:16]}-{encoded[16:20]}-{encoded[20:]}"


def _digest_challenge(header: str) -> dict[str, str] | None:
    if len(header) > 8192 or not header.lower().startswith("digest "):
        return None
    values: dict[str, str] = {}
    content = header[7:]
    pattern = re.compile(r'\s*([A-Za-z][A-Za-z0-9_-]*)\s*=\s*(?:"((?:\\.|[^"\\])*)"|([^,\s]+))\s*(?:,|$)')
    position = 0
    while position < len(content):
        match = pattern.match(content, position)
        if match is None:
            return None
        name = match.group(1).lower()
        if name in values:
            return None
        value = match.group(2) if match.group(2) is not None else match.group(3)
        if value is None:
            return None
        value = re.sub(r"\\(.)", r"\1", value)
        if not value or len(value) > 2048 or any(ord(char) < 32 or ord(char) == 127 for char in value):
            return None
        values[name] = value
        position = match.end()
    algorithm = values.get("algorithm", "MD5")
    if not values.get("realm") or not values.get("nonce") or values.get("stale", "").lower() == "true":
        return None
    if algorithm.upper() not in {"MD5", "MD5-SESS", "SHA-256", "SHA-256-SESS"}:
        return None
    if "auth" not in [item.strip() for item in values.get("qop", "").split(",")]:
        return None
    values["algorithm"] = algorithm
    return values


def _digest_authorization(
    challenge: dict[str, str], username: str, password: str, uri: str, cnonce: str
) -> str:
    algorithm = challenge["algorithm"]
    hash_name = "sha256" if algorithm.upper().startswith("SHA-256") else "md5"

    def digest(value: str) -> str:
        return hashlib.new(hash_name, value.encode()).hexdigest()

    ha1 = digest(username + ":" + challenge["realm"] + ":" + password)
    if algorithm.upper().endswith("-SESS"):
        ha1 = digest(ha1 + ":" + challenge["nonce"] + ":" + cnonce)
    ha2 = digest("GET:" + uri)
    response = digest(ha1 + ":" + challenge["nonce"] + ":00000001:" + cnonce + ":auth:" + ha2)

    def quote(value: str) -> str:
        return '"' + value.replace("\\", "\\\\").replace('"', '\\"') + '"'

    result = (
        "Digest username="
        + quote(username)
        + ", realm="
        + quote(challenge["realm"])
        + ", nonce="
        + quote(challenge["nonce"])
        + ", uri="
        + quote(uri)
        + ", response="
        + quote(response)
        + ", algorithm="
        + algorithm
        + ", qop=auth, nc=00000001, cnonce="
        + quote(cnonce)
    )
    if challenge.get("opaque"):
        result += ", opaque=" + quote(challenge["opaque"])
    return result


class Client:
    """Resolve credentials and complete the OceanBase Digest challenge."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        clock: Callable[[], datetime] | None = None,
        random_bytes: Callable[[], bytes] | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.site not in {"domestic", "international"}
            or config.timeout_seconds <= 0
            or not 0 < config.max_response_bytes <= 16 << 20
        ):
            raise ValueError("OceanBase configuration and credentials are invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.base_url = (
            "https://api-cloud.oceanbase.com"
            if config.site == "international"
            else "https://api-cloud-cn.oceanbase.com"
        )
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.clock = clock or (lambda: datetime.now(UTC))
        self.random_bytes = random_bytes or (lambda: secrets.token_bytes(16))

    def list_instances(self) -> "ListInstancesOperation":
        """Bind the instance-list Query."""
        return ListInstancesOperation(self)

    async def aclose(self) -> None:
        """Close the transport if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class ListInstancesOperation:
    """Read one page of project instances."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("OceanBase client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable generated Query definition."""
        return LIST_INSTANCES_DEFINITION

    async def invoke(self, call: Call, input: ListInstancesInput) -> QueryAttempt[ListInstancesOutput]:
        """Complete a Digest challenge and validate the instance page."""
        if not isinstance(input, ListInstancesInput):
            return _branch("defect", FailureKind.VALIDATION, "page input is invalid")
        page_number = input.page_number or 1
        page_size = input.page_size or 10
        if page_number < 1 or not 1 <= page_size <= 100:
            return _branch("defect", FailureKind.VALIDATION, "pageNumber or pageSize is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            access_key = credentials.access_key_id.reveal()
            secret = credentials.access_key_secret.reveal()
            project_id = credentials.project_id.reveal()
            if (
                not _valid_secret(access_key)
                or not _valid_secret(secret)
                or not re.fullmatch(r"[A-Za-z0-9_-]{1,128}", project_id)
            ):
                raise ValueError("invalid credential")
        except (ValueError, LookupError, OSError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        try:
            random_value = self.client.random_bytes()
            if len(random_value) != 16:
                raise ValueError("invalid random bytes")
            request_id = _request_id(random_value)
        except (OSError, ValueError):
            return _branch("defect", FailureKind.AVAILABILITY, "request ID could not be generated")
        query = urlencode(
            sorted(
                {"requestId": request_id, "pageNumber": str(page_number), "pageSize": str(page_size)}.items()
            )
        )
        uri = "/api/v2/instances?" + query
        url = self.client.base_url + uri

        async def send(authorization: str = "") -> httpx.Response:
            headers = {"Accept": "application/json", "X-Ob-Project-Id": project_id}
            if authorization:
                headers["Authorization"] = authorization
            request = self.client.http_client.build_request("GET", url, headers=headers)
            response, _ = await send_traced(self.client.http_client, request)
            return response

        try:
            response = await send()
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "OceanBase Cloud request failed")
        if response.status_code == 401:
            challenge = _digest_challenge(response.headers.get("WWW-Authenticate", ""))
            await response.aclose()
            if challenge is None:
                return _branch(
                    "providerRejected",
                    FailureKind.AUTHENTICATION,
                    "OceanBase Cloud returned an unsupported Digest challenge",
                )
            try:
                cnonce = self.client.random_bytes().hex()
                if len(cnonce) != 32:
                    raise ValueError("invalid cnonce")
                authorization = _digest_authorization(challenge, access_key, secret, uri, cnonce)
            except (OSError, ValueError):
                return _branch(
                    "defect", FailureKind.AVAILABILITY, "Digest authorization could not be constructed"
                )
            try:
                response = await send(authorization)
            except (ProviderTransportError, httpx.HTTPError, ValueError):
                return _retry(FailureKind.AVAILABILITY, "OceanBase Cloud request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), self.client.clock())
            if response.status_code == 429:
                return _retry(FailureKind.RATE_LIMIT, "OceanBase Cloud rate limited the request", delay)
            if response.status_code == 408 or response.status_code >= 500:
                return _retry(FailureKind.AVAILABILITY, "OceanBase Cloud is temporarily unavailable", delay)
            if response.status_code != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if response.status_code == 401
                    else FailureKind.AUTHORIZATION
                    if response.status_code == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch(
                    "providerRejected",
                    kind,
                    f"OceanBase Cloud rejected the request with HTTP {response.status_code}",
                )
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "OceanBase Cloud response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "OceanBase Cloud response was interrupted")
            try:
                data: Any = json.loads(body)
            except (UnicodeDecodeError, ValueError):
                return _branch(
                    "invalidResponse",
                    FailureKind.PROTOCOL,
                    "OceanBase Cloud returned invalid instance response",
                )
            if not isinstance(data, dict) or type(data.get("success")) is not bool:
                return _branch(
                    "invalidResponse",
                    FailureKind.PROTOCOL,
                    "OceanBase Cloud returned invalid instance response",
                )
            if not data["success"]:
                return _branch(
                    "providerRejected",
                    FailureKind.PROVIDER_REJECTION,
                    "OceanBase Cloud rejected the instance query",
                )
            page = data.get("data")
            items = page.get("dataList") if isinstance(page, dict) else None
            total = data.get("totalCount")
            if not isinstance(items, list) or type(total) is not int or total < 0 or len(items) > page_size:
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "OceanBase Cloud returned invalid instance data"
                )
            output_items: list[InstanceInfo] = []
            for item in items:
                if not isinstance(item, dict):
                    return _branch(
                        "invalidResponse",
                        FailureKind.PROTOCOL,
                        "OceanBase Cloud returned invalid instance data",
                    )
                fields = [item.get(key) for key in ("instanceId", "instanceName", "state", "region")]
                limits = (256, 256, 64, 128)
                if any(
                    not isinstance(field, str)
                    or not field
                    or len(field.encode()) > limit
                    or any(secret_value in field for secret_value in (access_key, secret, project_id))
                    for field, limit in zip(fields, limits, strict=True)
                ):
                    return _branch(
                        "invalidResponse",
                        FailureKind.PROTOCOL,
                        "OceanBase Cloud returned invalid instance data",
                    )
                output_items.append(
                    InstanceInfo(item["instanceId"], item["instanceName"], item["state"], item["region"])
                )
            return QueryAttempt.branch_result(
                "listed", ListInstancesOutput(tuple(output_items), total, page_number, page_size), Receipt()
            )
        finally:
            await response.aclose()
