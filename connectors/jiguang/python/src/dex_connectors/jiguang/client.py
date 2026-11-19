# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""JPush application tag Query and Go-compatible outcome classification."""

import base64
import json
from datetime import UTC, datetime

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
    CONNECTOR_ID as CONNECTOR_ID,
)
from ._generated.definitions import (
    DEFECT as DEFECT,
)
from ._generated.definitions import (
    INVALID_RESPONSE as INVALID_RESPONSE,
)
from ._generated.definitions import (
    LIST_APPLICATION_TAGS_DEFINITION as LIST_APPLICATION_TAGS_DEFINITION,
)
from ._generated.definitions import (
    LIST_APPLICATION_TAGS_OPERATION_ID,
)
from ._generated.definitions import (
    LISTED as LISTED,
)
from ._generated.definitions import (
    PROVIDER_REJECTED as PROVIDER_REJECTED,
)
from .models import ApplicationTags, ListApplicationTagsInput


class Client:
    """Bounded HTTP client resolving private JPush credentials for each Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("JPush configuration and credentials are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("JPush timeout and response limit must be positive")
        self.base_url = validate_base_url(config.base_url)
        if httpx.URL(self.base_url).path not in ("", "/"):
            raise ValueError("JPush API URL must be a host root")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)

    def list_application_tags(self) -> "ListApplicationTagsOperation":
        """Bind the application tag Query to this client."""
        return ListApplicationTagsOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport after Worker shutdown."""
        await self.http_client.aclose()


class ListApplicationTagsOperation:
    """Read application tags without exposing device identifiers."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("JPush client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return LIST_APPLICATION_TAGS_DEFINITION

    async def invoke(self, call: Call, input: ListApplicationTagsInput) -> QueryAttempt[ApplicationTags]:
        """Make one provider request and classify its observed outcome."""
        try:
            credentials = await self.client.credentials.resolve(call)
            app_key = credentials.app_key
            secret = credentials.master_secret.reveal()
        except Exception:
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "JPush credentials unavailable")
        if (
            not isinstance(app_key, str)
            or not 0 < len(app_key) <= 128
            or any(not "!" <= character <= "~" or character == ":" for character in app_key)
            or not secret
        ):
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "JPush credentials invalid")
        token = base64.b64encode(f"{app_key}:{secret}".encode()).decode()
        request = self.client.http_client.build_request(
            "GET",
            self.client.base_url + "/v3/tags/",
            headers={"Accept": "application/json", "Authorization": "Basic " + token},
        )
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError):
            return _retry(FailureKind.TRANSPORT, "JPush request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if delay == 0 and response.status_code == 429:
                reset = response.headers.get("X-Rate-Limit-Reset", "")
                if reset.isdecimal() and 0 < int(reset) <= 3600:
                    delay = float(reset)
            if response.status_code == 429:
                return _retry(FailureKind.RATE_LIMIT, "JPush rate limited the request", delay)
            if response.status_code == 408 or response.status_code >= 500:
                return _retry(FailureKind.AVAILABILITY, "JPush is unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE, "JPush tags response too large"
                )
            except ProviderTransportError:
                return _retry(FailureKind.TRANSPORT, "JPush response interrupted")
            if response.status_code != 200:
                return _branch(
                    PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION, "JPush rejected the request"
                )
            try:
                document = json.loads(body.decode("utf-8"), parse_constant=_invalid_constant)
                if not isinstance(document, dict) or "tags" not in document:
                    raise ValueError("missing tags")
                tags = document["tags"]
                if tags is None:
                    tags = []
                if not isinstance(tags, list) or len(tags) > 1000:
                    raise ValueError("invalid tags")
                if any(not isinstance(tag, str) or not tag or len(tag.encode()) > 40 for tag in tags):
                    raise ValueError("invalid tag")
            except (ValueError, TypeError, UnicodeDecodeError):
                return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "JPush returned invalid tags JSON")
            return QueryAttempt.branch_result(LISTED, ApplicationTags(tuple(tags)), Receipt())
        finally:
            await response.aclose()


def _invalid_constant(value: str) -> None:
    raise ValueError(f"invalid JSON constant {value}")


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ApplicationTags]:
    return QueryAttempt.branch_result(
        branch,
        ApplicationTags(),
        Receipt(),
        Failure(kind, CONNECTOR_ID, LIST_APPLICATION_TAGS_OPERATION_ID, message),
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ApplicationTags]:
    return QueryAttempt.retry(
        Failure(kind, CONNECTOR_ID, LIST_APPLICATION_TAGS_OPERATION_ID, message),
        delay,
    )


Config.__doc__ = "JPush endpoint, timeout in seconds, and response bound in bytes."
Credentials.__doc__ = "Application key and private master secret resolved for each Query."
