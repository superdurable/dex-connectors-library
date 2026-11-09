# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Workday worker Query decisions matching Go-recorded provider scenarios."""

from __future__ import annotations

import json
import re
from typing import Any
from urllib.parse import quote

from dex_connectors.sdk import Call, Failure, FailureKind, QueryAttempt, QueryDefinition, Receipt

from ._generated import definitions
from .client import Client, Exchange
from .models import GetWorkerInput, WorkerSummary

_WORKER_ID_PATTERN = re.compile(r"[A-Fa-f0-9]{32}\Z")


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "workday", "getWorker", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[WorkerSummary]:
    return QueryAttempt.branch_result(branch, WorkerSummary(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[WorkerSummary]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _document(body: bytes) -> dict[str, Any] | None:
    try:
        value: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return None
    return value if isinstance(value, dict) else None


def _read_failure(exchange: Exchange) -> QueryAttempt[WorkerSummary] | None:
    if exchange.error == "credential":
        return _branch("defect", FailureKind.AUTHENTICATION, "Workday connection needs authorization")
    if exchange.error in ("request", "refresh"):
        return _retry(FailureKind.TRANSPORT, "Workday worker request failed")
    if exchange.status == 429:
        return _retry(FailureKind.RATE_LIMIT, "Workday rate limited the request", exchange.retry_after)
    if exchange.status == 408 or exchange.status >= 500:
        return _retry(
            FailureKind.AVAILABILITY, "Workday is temporarily unavailable", exchange.retry_after
        )
    if exchange.status == 404:
        return _branch("notFound", FailureKind.NOT_FOUND, "Workday worker was not found")
    if exchange.error == "too_large":
        return _branch(
            "invalidResponse", FailureKind.RESPONSE_TOO_LARGE,
            "Workday worker response exceeded the configured limit",
        )
    if exchange.status != 200:
        return _branch(
            "providerRejected", FailureKind.PROVIDER_REJECTION,
            f"Workday rejected HTTP {exchange.status}",
        )
    if exchange.error:
        return _retry(FailureKind.TRANSPORT, "Workday worker response was interrupted")
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
            return _branch("defect", FailureKind.VALIDATION, "worker WID is invalid")
        exchange = await self.client.get(call, "/workers/" + quote(input.worker_id, safe=""))
        failure = _read_failure(exchange)
        if failure is not None:
            return failure
        document = _document(exchange.body)
        if document is None:
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Workday returned invalid worker JSON"
            )
        worker_id, descriptor = document.get("id"), document.get("descriptor")
        if worker_id != input.worker_id or not isinstance(descriptor, str) or not descriptor:
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Workday returned invalid worker JSON"
            )
        return QueryAttempt.branch_result("found", WorkerSummary(worker_id, descriptor), Receipt())
