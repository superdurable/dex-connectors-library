# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Personio v2 person lookup with Go-compatible outcomes."""

from __future__ import annotations

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any, TypeVar
from urllib.parse import parse_qs, unquote, urlencode, urlsplit

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
)
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
from ._generated.definitions import GET_PERSON_DEFINITION, LIST_PERSONS_DEFINITION
from .models import GetPersonInput, ListPersonsInput, PersonPage, PersonProfile


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One provider response or sanitized request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


_PersonOutput = TypeVar("_PersonOutput", PersonProfile, PersonPage)


def _exchange_failure(
    exchange: _Exchange, operation: str, empty: _PersonOutput
) -> QueryAttempt[_PersonOutput] | None:
    def terminal(branch: str, kind: FailureKind, message: str) -> QueryAttempt[_PersonOutput]:
        return QueryAttempt.branch_result(
            branch, empty, Receipt(), Failure(kind, "personio", operation, message)
        )

    def retry(kind: FailureKind, message: str) -> QueryAttempt[_PersonOutput]:
        return QueryAttempt.retry(Failure(kind, "personio", operation, message), exchange.retry_after)

    if exchange.error == "credential":
        return terminal("defect", FailureKind.AUTHENTICATION, "Personio connection needs authorization")
    if exchange.error == "request":
        return retry(FailureKind.TRANSPORT, "Personio request failed")
    if exchange.status == 429:
        return retry(FailureKind.RATE_LIMIT, "Personio rate limited the request")
    if exchange.status == 408 or exchange.status >= 500:
        return retry(FailureKind.AVAILABILITY, "Personio is temporarily unavailable")
    if exchange.status == 404 and operation == "getPerson":
        return terminal("notFound", FailureKind.NOT_FOUND, "Personio person was not found")
    if exchange.status != 200:
        return terminal(
            "providerRejected", FailureKind.PROVIDER_REJECTION, f"Personio rejected HTTP {exchange.status}"
        )
    if exchange.error == "too_large":
        return terminal(
            "invalidResponse",
            FailureKind.RESPONSE_TOO_LARGE,
            "Personio response exceeded the configured limit",
        )
    if exchange.error == "interrupted":
        return QueryAttempt.retry(
            Failure(FailureKind.TRANSPORT, "personio", operation, "Personio response was interrupted")
        )
    return None


def _invalid(
    branch: str, kind: FailureKind, message: str, operation: str, empty: _PersonOutput
) -> QueryAttempt[_PersonOutput]:
    return QueryAttempt.branch_result(branch, empty, Receipt(), Failure(kind, "personio", operation, message))


def _read_json(body: bytes) -> Any:
    return json.loads(
        body,
        object_pairs_hook=lambda pairs: {key.lower(): value for key, value in pairs},
        parse_constant=lambda _: (_ for _ in ()).throw(ValueError("invalid JSON number")),
    )


def _decode_person(body: bytes, requested_id: str) -> PersonProfile:
    raw: Any = _read_json(body)
    if not isinstance(raw, dict) or raw.get("id") != requested_id:
        raise ValueError("person ID mismatch")
    first_name = raw.get("first_name")
    last_name = raw.get("last_name")
    if first_name is not None and not isinstance(first_name, str):
        raise ValueError("invalid first name")
    if last_name is not None and not isinstance(last_name, str):
        raise ValueError("invalid last name")
    return PersonProfile(requested_id, first_name or "", last_name or "")


class Client:
    """Resolve access tokens per call and read selected Personio person fields."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Personio configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Personio request limits must be positive")
        base_url = validate_base_url(config.base_url).rstrip("/")
        if urlsplit(base_url).path != "/v2":
            raise ValueError("Personio API URL must end in /v2")
        self.base_url = base_url
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_person(self) -> GetPersonOperation:
        """Bind the getPerson Query to this client."""
        return GetPersonOperation(self)

    def list_persons(self) -> ListPersonsOperation:
        """Bind the filtered, cursor-paginated persons Query."""
        return ListPersonsOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client only when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _request_persons(self, call: Call, suffix: str) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            token = credential.access_token.reveal()
        except ReauthorizationRequiredError:
            return _Exchange(error="credential")
        except Exception:
            return _Exchange(error="request")
        if not is_header_safe_credential(token):
            return _Exchange(error="credential")
        return await self._send(token, suffix)

    async def _send(self, token: str, suffix: str) -> _Exchange:
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + "/persons" + suffix,
                headers={"Accept": "application/json", "Authorization": "Bearer " + token},
            )
            response, _ = await send_traced(self.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _Exchange(error="request")
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="too_large")
            except ProviderTransportError:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="interrupted")
            return _Exchange(status=response.status_code, body=body, retry_after=retry_after)
        finally:
            await response.aclose()


class GetPersonOperation:
    """Read one person's ID and names from Personio."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Personio client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return GET_PERSON_DEFINITION

    async def invoke(self, call: Call, input: GetPersonInput) -> QueryAttempt[PersonProfile]:
        """Classify person lookup outcomes consistently with Go."""
        if (
            not isinstance(input, GetPersonInput)
            or not isinstance(input.person_id, str)
            or re.fullmatch(r"[A-Za-z0-9_-]{1,128}", input.person_id) is None
        ):
            return _invalid(
                "defect",
                FailureKind.VALIDATION,
                "Personio person ID must be an API ID",
                "getPerson",
                PersonProfile(),
            )
        exchange = await self.client._request_persons(call, "/" + input.person_id)
        failure = _exchange_failure(exchange, "getPerson", PersonProfile())
        if failure is not None:
            return failure
        try:
            profile = _decode_person(exchange.body, input.person_id)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _invalid(
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Personio returned invalid person JSON",
                "getPerson",
                PersonProfile(),
            )
        return QueryAttempt.branch_result("found", profile, Receipt())


class ListPersonsOperation:
    """Read one Personio page, projecting IDs and names and returning the next cursor."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Personio client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated listPersons definition."""
        return LIST_PERSONS_DEFINITION

    async def invoke(self, call: Call, input: ListPersonsInput) -> QueryAttempt[PersonPage]:
        """Apply optional filters and read one bounded page with the current credentials."""
        if (
            not isinstance(input, ListPersonsInput)
            or type(input.limit) is not int
            or not 0 <= input.limit <= 50
            or not all(
                isinstance(v, str) for v in (input.cursor, input.first_name, input.last_name, input.status)
            )
            or input.status not in ("", "ACTIVE", "INACTIVE")
            or len(input.cursor.encode()) > 1024
        ):
            return _invalid(
                "defect",
                FailureKind.VALIDATION,
                "Personio list filters are invalid",
                "listPersons",
                PersonPage(),
            )
        filters = {
            "cursor": input.cursor,
            "first_name": input.first_name,
            "last_name": input.last_name,
            "limit": str(input.limit) if input.limit else "",
            "status": input.status,
        }
        query = urlencode({key: value for key, value in filters.items() if value})
        exchange = await self.client._request_persons(call, "?" + query if query else "")
        failure = _exchange_failure(exchange, "listPersons", PersonPage())
        if failure is not None:
            return failure
        try:
            page = _read_page(exchange.body, self.client.base_url)
        except (ValueError, TypeError, UnicodeError):
            return _invalid(
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Personio returned invalid persons JSON",
                "listPersons",
                PersonPage(),
            )
        return QueryAttempt.branch_result("listed", page, Receipt())


def _object(value: object) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError("expected Personio object")
    return value


def _string(value: object) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("expected Personio string")
    return value


def _read_page(body: bytes, base_url: str) -> PersonPage:
    record = _object(_read_json(body))
    rows = record.get("_data")
    if not isinstance(rows, list):
        raise ValueError("missing persons")
    persons = []
    for row in rows:
        item = _object(row)
        identifier = _string(item.get("id"))
        if re.fullmatch(r"[A-Za-z0-9_-]{1,128}", identifier) is None:
            raise ValueError("invalid person ID")
        persons.append(
            PersonProfile(identifier, _string(item.get("first_name")), _string(item.get("last_name")))
        )
    links = record
    for key in ("_meta", "links", "next"):
        nested = links.get(key)
        links = {} if nested is None else _object(nested)
    href = _string(links.get("href"))
    cursor = ""
    if href:
        if any(ord(char) < 32 or ord(char) == 127 for char in href):
            raise ValueError("invalid Personio link")
        next_page = urlsplit(href)
        base = urlsplit(base_url)
        if unquote(next_page.path) != "/v2/persons" or (
            next_page.scheme
            and (next_page.scheme, next_page.netloc.rsplit("@", 1)[-1]) != (base.scheme, base.netloc)
        ):
            raise ValueError("invalid Personio next-page link")
        valid_query = "&".join(
            pair
            for pair in next_page.query.split("&")
            if ";" not in pair and re.search(r"%(?![0-9a-fA-F]{2})", pair) is None
        )
        cursor = parse_qs(valid_query, keep_blank_values=True).get("cursor", [""])[0]
        if not cursor:
            raise ValueError("missing Personio next cursor")
    return PersonPage(tuple(persons), cursor)
