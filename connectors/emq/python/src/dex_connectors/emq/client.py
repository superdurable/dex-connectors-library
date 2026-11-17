# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""EMQX Cloud deployment listing with Go-compatible provider classification."""

import base64
import json
from datetime import UTC, datetime
from typing import Any
from urllib.parse import urlsplit

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
from ._generated.definitions import (
    CONNECTOR_ID,
    DEFECT,
    INVALID_RESPONSE,
    LIST_DEPLOYMENTS_DEFINITION,
    LIST_DEPLOYMENTS_OPERATION_ID,
    LISTED,
    PROVIDER_REJECTED,
)
from .models import Deployment, ListDeploymentsInput, ListDeploymentsOutput

_MAX_INT64 = (1 << 63) - 1


class Client:
    """Resolve Platform credentials for each deployment list Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        """Validate the Platform endpoint and own transport unless injected."""
        if config is None or credentials is None:
            raise ValueError("EMQX Cloud configuration and credential source are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("EMQX Cloud timeout or response limit is invalid")
        if not isinstance(config.platform_endpoint, str) or config.platform_endpoint.endswith("/"):
            raise ValueError("EMQX Cloud Platform endpoint must end in /public_api/v1")
        endpoint = validate_base_url(config.platform_endpoint)
        if urlsplit(endpoint).path != "/public_api/v1":
            raise ValueError("EMQX Cloud Platform endpoint must end in /public_api/v1")
        self.endpoint = endpoint
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self.owns_http_client = http_client is None

    def list_deployments(self) -> "ListDeploymentsOperation":
        """Bind the scoped deployment listing Query."""
        return ListDeploymentsOperation(self)

    async def aclose(self) -> None:
        """Close the connector-owned transport after the Worker stops."""
        if self.owns_http_client:
            await self.http_client.aclose()


class ListDeploymentsOperation:
    """Read the deployments visible to one Platform API key."""

    def __init__(self, client: Client) -> None:
        """Bind a validated EMQX Cloud client."""
        if client is None:
            raise ValueError("EMQX Cloud client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest operation identity and branches."""
        return LIST_DEPLOYMENTS_DEFINITION

    async def invoke(self, call: Call, input: ListDeploymentsInput) -> QueryAttempt[ListDeploymentsOutput]:
        """Send one GET and classify provider, protocol, and transport outcomes."""
        if not isinstance(input, ListDeploymentsInput):
            return _branch(DEFECT, FailureKind.VALIDATION, "deployment-list input is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            platform_key = credentials.platform_api_key.reveal()
            platform_secret = credentials.platform_api_secret.reveal()
        except Exception:
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Platform API credentials are unavailable or invalid"
            )
        if (
            not _valid_basic_credential(platform_key)
            or not _valid_basic_credential(platform_secret)
            or ":" in platform_key
        ):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Platform API credentials are unavailable or invalid"
            )
        basic_value = base64.b64encode(f"{platform_key}:{platform_secret}".encode("ascii")).decode("ascii")
        try:
            request = self.client.http_client.build_request(
                "GET",
                self.client.endpoint + "/deployments",
                headers={"Authorization": "Basic " + basic_value, "Accept": "application/json"},
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError):
            return _retry(FailureKind.AVAILABILITY, "EMQX Cloud request failed")
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "EMQX Cloud rate limited the request", retry_after)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "EMQX Cloud is temporarily unavailable", retry_after)
            if status != 200:
                kind = FailureKind.PROVIDER_REJECTION
                if status == 401:
                    kind = FailureKind.AUTHENTICATION
                elif status == 403:
                    kind = FailureKind.AUTHORIZATION
                return _branch(PROVIDER_REJECTED, kind, f"EMQX Cloud rejected the request with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "EMQX Cloud response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "EMQX Cloud response was interrupted")
            output = _decode_deployments(body)
            if output is None:
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "EMQX Cloud returned an invalid deployment list"
                )
            return QueryAttempt.branch_result(LISTED, output, Receipt())
        finally:
            await response.aclose()


def _valid_basic_credential(value: object) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 512 and all(0x21 <= ord(c) <= 0x7E for c in value)


def _decode_deployments(body: bytes) -> ListDeploymentsOutput | None:
    try:
        wire: Any = json.loads(body.decode("utf-8"))
    except (UnicodeDecodeError, ValueError):
        return None
    if not isinstance(wire, list):
        return None
    deployments: list[Deployment] = []
    for item in wire:
        if not isinstance(item, dict):
            return None
        try:
            deployment_id = _json_string(item.get("deploymentID"))
            deployment_name = _json_string(item.get("deploymentName"))
            deployment_type = _json_string(item.get("deploymentType"))
            project_name = _json_string(item.get("projectName"))
            region = _json_string(item.get("region"))
            status = _json_string(item.get("status"))
            version = _json_string(item.get("version"))
            platform = _json_string(item.get("platform"))
            created_at = _json_string(item.get("createAt"))
        except TypeError:
            return None
        connections = item.get("connections")
        tps = item.get("tps")
        if (
            not deployment_id
            or not deployment_name
            or not deployment_type
            or not status
            or type(connections) is not int
            or not 0 <= connections <= _MAX_INT64
            or type(tps) is not int
            or not 0 <= tps <= _MAX_INT64
        ):
            return None
        deployments.append(
            Deployment(
                deployment_id,
                deployment_name,
                deployment_type,
                project_name,
                region,
                status,
                version,
                platform,
                connections,
                tps,
                created_at,
            )
        )
    return ListDeploymentsOutput(tuple(deployments))


def _json_string(value: object) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise TypeError("EMQX Cloud deployment field must be a string")
    return value


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListDeploymentsOutput]:
    return QueryAttempt.branch_result(
        branch,
        ListDeploymentsOutput(),
        Receipt(),
        Failure(kind, CONNECTOR_ID, LIST_DEPLOYMENTS_OPERATION_ID, message),
    )


def _retry(kind: FailureKind, message: str, after: float = 0) -> QueryAttempt[ListDeploymentsOutput]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, LIST_DEPLOYMENTS_OPERATION_ID, message), after)
