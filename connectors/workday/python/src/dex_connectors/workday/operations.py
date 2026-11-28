# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Workday worker Query decisions matching Go-recorded provider scenarios."""

from __future__ import annotations

import json
import re
from typing import Any, TypeVar
from urllib.parse import quote, urlencode

from dex_connectors.sdk import Call, Failure, FailureKind, QueryAttempt, QueryDefinition, Receipt

from ._generated import definitions
from .client import Client, Exchange
from .models import GetWorkerInput, ListWorkersInput, WorkerPage, WorkerSummary

_WORKER_ID_PATTERN = re.compile(r"[A-Fa-f0-9]{32}\Z")


_Output = TypeVar("_Output")


def _branch(
    operation: str, output: _Output, branch: str, kind: FailureKind, message: str
) -> QueryAttempt[_Output]:
    failure = Failure(kind, "workday", operation, message)
    return QueryAttempt.branch_result(branch, output, Receipt(), failure)


def _document(body: bytes) -> dict[str, Any] | None:
    try:
        value: Any = json.loads(body.decode("utf-8", errors="replace"))
    except (ValueError, UnicodeDecodeError):
        return None
    return value if isinstance(value, dict) else None


def _read_failure(exchange: Exchange, operation: str, output: _Output) -> QueryAttempt[_Output] | None:
    def rejected(branch: str, kind: FailureKind, message: str) -> QueryAttempt[_Output]:
        return _branch(operation, output, branch, kind, message)

    def retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[_Output]:
        return QueryAttempt.retry(Failure(kind, "workday", operation, message), delay)

    if exchange.error == "credential":
        return rejected("defect", FailureKind.AUTHENTICATION, "Workday connection needs authorization")
    if exchange.error in ("request", "refresh"):
        return retry(FailureKind.TRANSPORT, "Workday worker request failed")
    if exchange.status == 429:
        return retry(FailureKind.RATE_LIMIT, "Workday rate limited the request", exchange.retry_after)
    if exchange.status == 408 or exchange.status >= 500:
        return retry(FailureKind.AVAILABILITY, "Workday is temporarily unavailable", exchange.retry_after)
    if exchange.status == 404 and operation == "getWorker":
        return rejected("notFound", FailureKind.NOT_FOUND, "Workday worker was not found")
    if exchange.status != 200:
        return rejected(
            "providerRejected", FailureKind.PROVIDER_REJECTION, f"Workday rejected HTTP {exchange.status}"
        )
    if exchange.error == "too_large":
        return rejected(
            "invalidResponse", FailureKind.RESPONSE_TOO_LARGE, "Workday worker response was too large"
        )
    if exchange.error:
        return retry(FailureKind.TRANSPORT, "Workday worker response was interrupted")
    return None


class GetWorkerOperation:
    """Read one worker by Workday ID."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated worker Query contract."""
        return definitions.GET_WORKER_DEFINITION

    async def invoke(self, call: Call, input: GetWorkerInput) -> QueryAttempt[WorkerSummary]:
        """Project only the stable WID and display name into the Flow."""
        if not _WORKER_ID_PATTERN.fullmatch(input.worker_id):
            return _branch(
                "getWorker", WorkerSummary(), "defect", FailureKind.VALIDATION, "worker WID is invalid"
            )
        exchange = await self.client.get(call, "/workers/" + quote(input.worker_id, safe=""))
        failure = _read_failure(exchange, "getWorker", WorkerSummary())
        if failure is not None:
            return failure
        document = _document(exchange.body)
        if document is None:
            return _branch(
                "getWorker",
                WorkerSummary(),
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Workday returned invalid worker JSON",
            )
        worker_id, descriptor = document.get("id"), document.get("descriptor")
        if worker_id != input.worker_id or not isinstance(descriptor, str) or not descriptor:
            return _branch(
                "getWorker",
                WorkerSummary(),
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Workday returned invalid worker JSON",
            )
        return QueryAttempt.branch_result("found", WorkerSummary(worker_id, descriptor), Receipt())


class ListWorkersOperation:
    """Read one page of workers visible to the tenant connection."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the worker directory Query contract."""
        return definitions.LIST_WORKERS_DEFINITION

    async def invoke(self, call: Call, input: ListWorkersInput) -> QueryAttempt[WorkerPage]:
        """Validate pagination and project each worker to its WID and display name."""
        if (
            type(input.limit) is not int
            or not 0 <= input.limit <= 100
            or type(input.offset) is not int
            or not 0 <= input.offset <= 2**63 - 1
            or not isinstance(input.search, str)
            or (input.search and len(input.search.strip().encode()) < 3)
        ):
            return _branch(
                "listWorkers", WorkerPage(), "defect", FailureKind.VALIDATION, "Invalid worker page options"
            )
        parameters: dict[str, str | int] = {}
        if input.limit:
            parameters["limit"] = input.limit
        if input.offset:
            parameters["offset"] = input.offset
        if input.search:
            parameters["search"] = input.search
        exchange = await self.client.get(
            call, "/workers" + ("?" + urlencode(parameters) if parameters else "")
        )
        failure = _read_failure(exchange, "listWorkers", WorkerPage())
        if failure is not None:
            return failure
        page = _worker_page(exchange.body)
        if page is None:
            return _branch(
                "listWorkers", WorkerPage(), "invalidResponse", FailureKind.PROTOCOL, "Invalid worker page"
            )
        return QueryAttempt.branch_result("found", page, Receipt())


def _worker_page(body: bytes) -> WorkerPage | None:
    document = _document(body)
    if document is None:
        return None
    fields = {name.lower(): value for name, value in document.items()}
    total = fields.get("total")
    total = 0 if total is None else total
    rows = fields.get("data")
    if type(total) is not int or not 0 <= total <= 2**63 - 1 or not isinstance(rows, list):
        return None
    workers = []
    for row in rows:
        if not isinstance(row, dict):
            return None
        fields = {name.lower(): value for name, value in row.items()}
        worker_id, descriptor = fields.get("id"), fields.get("descriptor")
        if not isinstance(worker_id, str) or not _WORKER_ID_PATTERN.fullmatch(worker_id):
            return None
        if not isinstance(descriptor, str) or not descriptor:
            return None
        workers.append(WorkerSummary(worker_id, descriptor))
    return WorkerPage(total, tuple(workers))
