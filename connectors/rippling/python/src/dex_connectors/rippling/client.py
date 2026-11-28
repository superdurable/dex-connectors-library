# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Rippling worker reads with Go-compatible outcomes."""

from __future__ import annotations

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import TypeVar
from urllib.parse import parse_qs, quote, urlencode, urlsplit

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
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import GET_WORKER_DEFINITION, LIST_WORKERS_DEFINITION
from .models import GetWorkerInput, ListWorkersInput, WorkerPage, WorkerSummary


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


_Output = TypeVar("_Output")


def _branch(
    operation: str, empty: _Output, branch: str, kind: FailureKind, message: str
) -> QueryAttempt[_Output]:
    return QueryAttempt.branch_result(branch, empty, Receipt(), Failure(kind, "rippling", operation, message))


def _valid_worker_id(worker_id: object) -> bool:
    return (
        isinstance(worker_id, str)
        and 1 <= len(worker_id) <= 64
        and worker_id.isascii()
        and all(character.isalnum() or character in "-_" for character in worker_id)
    )


def _decode_worker(raw: object) -> WorkerSummary:
    if not isinstance(raw, dict) or not isinstance(raw.get("id"), str) or not _valid_worker_id(raw["id"]):
        raise ValueError("worker identity is invalid")
    status = raw.get("status")
    title = raw.get("title")
    if status is not None and not isinstance(status, str):
        raise ValueError("worker status is invalid")
    if title is not None and not isinstance(title, str):
        raise ValueError("worker title is invalid")
    return WorkerSummary(raw["id"], title or "", status or "")


class Client:
    """Resolve an API token per call and read selected worker metadata."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Rippling configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Rippling request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_worker(self) -> GetWorkerOperation:
        """Bind the getWorker Query to this client."""
        return GetWorkerOperation(self)

    def list_workers(self) -> ListWorkersOperation:
        """Bind the paginated worker Query to this connection."""
        return ListWorkersOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, path: str) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            api_token = credential.api_token.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not is_header_safe_credential(api_token):
            return _Exchange(error="credential")
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + path,
                headers={"Accept": "application/json", "Authorization": "Bearer " + api_token},
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


class GetWorkerOperation:
    """Read one worker's ID, status, and title."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query worker."""
        return GET_WORKER_DEFINITION

    async def invoke(self, call: Call, input: GetWorkerInput) -> QueryAttempt[WorkerSummary]:
        """Classify worker lookup outcomes consistently with Go."""
        if not isinstance(input, GetWorkerInput) or not _valid_worker_id(input.worker_id):
            return _branch(
                "getWorker",
                WorkerSummary(),
                "defect",
                FailureKind.VALIDATION,
                "worker ID must contain 1–64 URL-safe characters",
            )
        exchange = await self.client._get(call, "/workers/" + quote(input.worker_id, safe="") + "/")
        failed = _read_failure(exchange, "getWorker", WorkerSummary())
        if failed is not None:
            return failed
        try:
            worker = _decode_worker(json.loads(exchange.body))
            if worker.id != input.worker_id:
                raise ValueError("worker identity does not match request")
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch(
                "getWorker",
                WorkerSummary(),
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Rippling returned invalid worker JSON",
            )
        return QueryAttempt.branch_result("found", worker, Receipt())


def _read_failure(exchange: _Exchange, operation: str, empty: _Output) -> QueryAttempt[_Output] | None:
    def retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[_Output]:
        return QueryAttempt.retry(Failure(kind, "rippling", operation, message), delay)

    if exchange.error == "credential":
        return _branch(
            operation,
            empty,
            "defect",
            FailureKind.AUTHENTICATION,
            "Rippling credentials are unavailable or invalid",
        )
    if exchange.error == "request":
        return retry(FailureKind.TRANSPORT, "Rippling request failed")
    if exchange.status == 429:
        return retry(FailureKind.RATE_LIMIT, "Rippling rate limited the request", exchange.retry_after)
    if exchange.status == 408 or exchange.status >= 500:
        return retry(FailureKind.AVAILABILITY, "Rippling is temporarily unavailable", exchange.retry_after)
    if exchange.status == 404 and operation == "getWorker":
        return _branch(
            operation, empty, "notFound", FailureKind.NOT_FOUND, "Rippling did not expose the worker"
        )
    if exchange.status != 200:
        return _branch(
            operation,
            empty,
            "providerRejected",
            FailureKind.PROVIDER_REJECTION,
            f"Rippling rejected HTTP {exchange.status}",
        )
    if exchange.error == "too_large":
        return _branch(
            operation,
            empty,
            "invalidResponse",
            FailureKind.RESPONSE_TOO_LARGE,
            "Rippling response exceeded the configured limit",
        )
    if exchange.error:
        return retry(FailureKind.TRANSPORT, "Rippling response was interrupted")
    return None


class ListWorkersOperation:
    """Read one page of workers visible to the Rippling connection."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated worker page contract."""
        return LIST_WORKERS_DEFINITION

    async def invoke(self, call: Call, input: ListWorkersInput) -> QueryAttempt[WorkerPage]:
        """Read a bounded response and project worker IDs, statuses, titles and the next cursor."""
        if (
            not isinstance(input, ListWorkersInput)
            or type(input.limit) is not int
            or not 0 <= input.limit <= 100
            or not isinstance(input.cursor, str)
            or len(input.cursor.encode()) > 2048
        ):
            return _branch(
                "listWorkers",
                WorkerPage(),
                "defect",
                FailureKind.VALIDATION,
                "invalid worker page limit or cursor",
            )
        params = {}
        if input.cursor:
            params["cursor"] = input.cursor
        if input.limit:
            params["limit"] = str(input.limit)
        path = "/workers/" + ("?" + urlencode(params) if params else "")
        exchange = await self.client._get(call, path)
        failed = _read_failure(exchange, "listWorkers", WorkerPage())
        if failed is not None:
            return failed
        try:
            document = json.loads(exchange.body)
            if not isinstance(document, dict) or not isinstance(document.get("results"), list):
                raise ValueError("worker page is invalid")
            workers = tuple(_decode_worker(record) for record in document["results"])
            page = WorkerPage(workers=workers, next_cursor=_next_cursor(document.get("next_link")))
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch(
                "listWorkers",
                WorkerPage(),
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Rippling returned invalid worker page JSON",
            )
        return QueryAttempt.branch_result("found", page, Receipt())


def _next_cursor(link: object) -> str:
    if link is None or link == "":
        return ""
    if not isinstance(link, str) or re.search(r"[\x00-\x1f\x7f]", link):
        raise ValueError("invalid worker page link")
    parsed = urlsplit(link)
    if re.search(r"%(?![0-9a-fA-F]{2})", parsed.path):
        raise ValueError("invalid worker page path")
    for pair in parsed.query.split("&"):
        if ";" in pair or re.search(r"%(?![0-9a-fA-F]{2})", pair):
            continue
        values = parse_qs(pair, keep_blank_values=True).get("cursor")
        if values:
            if values[0]:
                return values[0]
            break
    raise ValueError("worker page cursor missing")
