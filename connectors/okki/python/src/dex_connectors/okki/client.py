# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Okki provider transport and Go-compatible Query decisions."""

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
    ReauthorizationRequiredError,
    Receipt,
    RejectedCredentialRefreshingProvider,
    resolve_credential,
    resolve_credential_after_rejection,
)
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver
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
from ._generated.definitions import LIST_CUSTOMER_FIELDS_DEFINITION
from ._generated.refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from .models import CustomerField, CustomerFields, ListCustomerFieldsInput


def _opaque(value: str, limit: int) -> bool:
    return (
        isinstance(value, str) and 0 < len(value) <= limit and all(0x20 < ord(char) < 0x7F for char in value)
    )


class Client:
    """Resolve credentials for each listCustomerFields request."""

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
            or not 0 < config.max_response_bytes <= 16 << 20
        ):
            raise ValueError("Okki configuration and credentials are required")
        validate_base_url(config.base_url)
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None
        self.refresh_driver = ManifestCredentialRefreshDriver[Credentials](
            "OKKI", AUTHORIZATION, CREDENTIAL_FIELDS, self.http_client
        )

    def list_customer_fields(self) -> "ListCustomerFieldsOperation":
        """Bind the listCustomerFields Query."""
        return ListCustomerFieldsOperation(self)

    async def aclose(self) -> None:
        """Close the owned HTTP transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class ListCustomerFieldsOperation:
    """Query Okki and classify the provider outcome."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return LIST_CUSTOMER_FIELDS_DEFINITION

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[CustomerFields]:
        return QueryAttempt.branch_result(
            branch,
            CustomerFields(),
            Receipt(),
            Failure(kind, "okki", "listCustomerFields", "Okki request failed"),
        )

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[CustomerFields]:
        return QueryAttempt.retry(Failure(kind, "okki", "listCustomerFields", "Okki request failed"), delay)

    def _request(
        self, input: ListCustomerFieldsInput, credentials: Credentials
    ) -> httpx.Request | QueryAttempt[CustomerFields]:
        if not credentials.client_id or not _opaque(credentials.client_secret.reveal(), 1024):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        token = credentials.access_token.reveal()
        if not _opaque(token, 2048):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        return self.client.http_client.build_request(
            "GET",
            self.client.config.base_url.rstrip("/") + "/v1/company/fields",
            params={"type": "company"},
            headers={"Accept": "application/json", "Authorization": token},
        )

    def _decode(
        self, wire: object, input: ListCustomerFieldsInput, credentials: Credentials
    ) -> CustomerFields | QueryAttempt[CustomerFields] | None:
        if not isinstance(wire, dict):
            return None
        code = wire.get("code")
        if type(code) is not int:
            return None
        if code >= 500:
            return self._retry(FailureKind.AVAILABILITY)
        if code != 200:
            return self._branch("providerRejected", FailureKind.PROVIDER_REJECTION)
        fields = wire.get("data")
        if not isinstance(fields, list) or len(fields) > 500:
            return None
        values = []
        for item in fields:
            if not isinstance(item, dict):
                return None
            field_id, name, field_type = item.get("id"), item.get("name"), item.get("field_type")
            if type(field_id) is int:
                field_id = str(field_id)
            if field_id == "":
                continue
            if (
                not isinstance(field_id, str)
                or not isinstance(name, str)
                or not name
                or type(field_type) is not int
                or len(field_id) > 128
                or len(name) > 512
                or field_type <= 0
            ):
                return None
            values.append(CustomerField(id=field_id, name=name, field_type=field_type))
        return CustomerFields(fields=tuple(values))

    async def invoke(self, call: Call, input: ListCustomerFieldsInput) -> QueryAttempt[CustomerFields]:
        """Refresh credentials when supported, then read bounded field metadata."""
        if not isinstance(input, ListCustomerFieldsInput):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            credentials = await resolve_credential(self.client.credentials, call, self.client.refresh_driver)
        except ReauthorizationRequiredError:
            return self._branch("defect", FailureKind.AUTHENTICATION)
        except (ValueError, LookupError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        except OSError:
            return self._retry(FailureKind.TRANSPORT)

        for attempt in range(2):
            request = self._request(input, credentials)
            if isinstance(request, QueryAttempt):
                return request
            try:
                response, _ = await send_traced(self.client.http_client, request)
            except (ProviderTransportError, httpx.HTTPError, ValueError):
                return self._retry(FailureKind.TRANSPORT)
            try:
                status = response.status_code
                delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
                if (
                    status == 401
                    and attempt == 0
                    and isinstance(self.client.credentials, RejectedCredentialRefreshingProvider)
                ):
                    try:
                        credentials = await resolve_credential_after_rejection(
                            self.client.credentials, call, self.client.refresh_driver
                        )
                    except ReauthorizationRequiredError:
                        return self._branch("providerRejected", FailureKind.PROVIDER_REJECTION)
                    except OSError:
                        return self._retry(FailureKind.TRANSPORT)
                    continue
                if status == 429:
                    return self._retry(FailureKind.RATE_LIMIT, delay)
                if status == 408 or status >= 500:
                    return self._retry(FailureKind.AVAILABILITY, delay)
                if status != 200:
                    return self._branch("providerRejected", FailureKind.PROVIDER_REJECTION)
                try:
                    body = await read_bounded_body(response, self.client.config.max_response_bytes)
                except BodyTooLargeError:
                    return self._branch("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
                except ProviderTransportError:
                    return self._retry(FailureKind.TRANSPORT)
            finally:
                await response.aclose()
            try:
                wire = json.loads(body)
            except (ValueError, UnicodeDecodeError):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            result = self._decode(wire, input, credentials)
            if isinstance(result, QueryAttempt):
                return result
            if result is None:
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            return QueryAttempt.branch_result("listed", result, Receipt())
        return self._retry(FailureKind.TRANSPORT)
