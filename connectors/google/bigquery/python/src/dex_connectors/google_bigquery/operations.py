# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""BigQuery decisions matching the Go-recorded provider scenarios."""

from __future__ import annotations

import json
import re
from typing import Any
from urllib.parse import quote, urlencode

from dex_connectors.sdk import Call, Failure, FailureKind, QueryAttempt, QueryDefinition, Receipt

from ._generated import definitions
from .client import Client, Exchange
from .models import Dataset, DatasetsPage, ListDatasetsInput


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "google-bigquery", "listDatasets", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[DatasetsPage]:
    return QueryAttempt.branch_result(branch, DatasetsPage(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[DatasetsPage]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _document(body: bytes) -> dict[str, Any] | None:
    try:
        value: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return None
    return value if isinstance(value, dict) else None


def _valid_project_id(project_id: str) -> bool:
    return re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9:._~-]{0,255}", project_id) is not None


def _valid_page_token(page_token: str) -> bool:
    return len(page_token.encode()) <= 4096 and "\r" not in page_token and "\n" not in page_token


def _read_failure(exchange: Exchange) -> QueryAttempt[DatasetsPage] | None:
    if exchange.error == "credential":
        return _branch(
            "defect", FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
        )
    if exchange.error in ("request", "refresh"):
        return _retry(FailureKind.TRANSPORT, "BigQuery request failed")
    if exchange.error == "too_large":
        return _branch(
            "invalidResponse",
            FailureKind.RESPONSE_TOO_LARGE,
            "BigQuery response exceeded the configured limit",
        )
    if exchange.error:
        return _retry(FailureKind.TRANSPORT, "BigQuery response was interrupted")
    if exchange.status == 429:
        return _retry(FailureKind.RATE_LIMIT, "BigQuery rate limited the request", exchange.retry_after)
    if exchange.status == 408 or exchange.status >= 500:
        return _retry(FailureKind.AVAILABILITY, "BigQuery is temporarily unavailable", exchange.retry_after)
    if exchange.status != 200:
        return _branch(
            "providerRejected", FailureKind.PROVIDER_REJECTION, f"BigQuery rejected HTTP {exchange.status}"
        )
    return None


class ListDatasetsOperation:
    """Read one bounded dataset page in a Google Cloud project."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated dataset Query contract."""
        return definitions.LIST_DATASETS_DEFINITION

    async def invoke(self, call: Call, input: ListDatasetsInput) -> QueryAttempt[DatasetsPage]:
        """Project dataset IDs, project IDs, display names, and the next page token."""
        if (
            not _valid_project_id(input.project_id)
            or input.max_results < 0
            or input.max_results > 100
            or not _valid_page_token(input.page_token)
        ):
            return _branch(
                "defect", FailureKind.VALIDATION, "project ID, page size, or page token is invalid"
            )
        max_results = input.max_results or 100
        parameters = {"maxResults": str(max_results)}
        if input.page_token:
            parameters["pageToken"] = input.page_token
        path = (
            "/bigquery/v2/projects/"
            + quote(input.project_id, safe="")
            + "/datasets?"
            + urlencode(sorted(parameters.items()))
        )
        exchange = await self.client.get(call, path)
        failure = _read_failure(exchange)
        if failure is not None:
            return failure
        document = _document(exchange.body)
        if document is None:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "BigQuery returned invalid dataset JSON")
        entries = document.get("datasets")
        if entries is None:
            entries = []
        page_token = document.get("nextPageToken")
        if (
            not isinstance(entries, list)
            or len(entries) > max_results
            or (
                page_token is not None
                and (not isinstance(page_token, str) or not _valid_page_token(page_token))
            )
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "BigQuery returned invalid dataset JSON")
        datasets: list[Dataset] = []
        for entry in entries:
            reference = entry.get("datasetReference") if isinstance(entry, dict) else None
            dataset_id = reference.get("datasetId") if isinstance(reference, dict) else None
            project_id = reference.get("projectId") if isinstance(reference, dict) else None
            friendly_name = entry.get("friendlyName") if isinstance(entry, dict) else None
            if (
                not isinstance(dataset_id, str)
                or len(dataset_id) > 1024
                or re.fullmatch(r"[A-Za-z0-9_]+", dataset_id) is None
                or project_id != input.project_id
                or (friendly_name is not None and not isinstance(friendly_name, str))
            ):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "BigQuery returned invalid dataset JSON"
                )
            datasets.append(Dataset(dataset_id, project_id, friendly_name or ""))
        return QueryAttempt.branch_result(
            "listed", DatasetsPage(tuple(datasets), page_token or ""), Receipt()
        )
