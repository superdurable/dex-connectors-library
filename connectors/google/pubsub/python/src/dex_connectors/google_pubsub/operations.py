# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""PubSub decisions matching the Go-recorded provider scenarios."""

from __future__ import annotations

import json
import re
from typing import Any
from urllib.parse import quote, urlencode

from dex_connectors.sdk import Call, Failure, FailureKind, QueryAttempt, QueryDefinition, Receipt

from ._generated import definitions
from .client import Client, Exchange
from .models import ListTopicsInput, Topic, TopicsPage


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "google-pubsub", "listTopics", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[TopicsPage]:
    return QueryAttempt.branch_result(branch, TopicsPage(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[TopicsPage]:
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


def _read_failure(exchange: Exchange) -> QueryAttempt[TopicsPage] | None:
    if exchange.error == "credential":
        return _branch(
            "defect", FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
        )
    if exchange.error in ("request", "refresh"):
        return _retry(FailureKind.TRANSPORT, "PubSub request failed")
    if exchange.error == "too_large":
        return _branch(
            "invalidResponse",
            FailureKind.RESPONSE_TOO_LARGE,
            "PubSub response exceeded the configured limit",
        )
    if exchange.error:
        return _retry(FailureKind.TRANSPORT, "PubSub response was interrupted")
    if exchange.status == 429:
        return _retry(FailureKind.RATE_LIMIT, "PubSub rate limited the request", exchange.retry_after)
    if exchange.status == 408 or exchange.status >= 500:
        return _retry(FailureKind.AVAILABILITY, "PubSub is temporarily unavailable", exchange.retry_after)
    if exchange.status != 200:
        return _branch(
            "providerRejected", FailureKind.PROVIDER_REJECTION, f"PubSub rejected HTTP {exchange.status}"
        )
    return None


class ListTopicsOperation:
    """Read one bounded topic page in a Google Cloud project."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated topic Query contract."""
        return definitions.LIST_TOPICS_DEFINITION

    async def invoke(self, call: Call, input: ListTopicsInput) -> QueryAttempt[TopicsPage]:
        """Project topic resource names and the next page token."""
        if (
            not _valid_project_id(input.project_id)
            or input.page_size < 0
            or input.page_size > 100
            or not _valid_page_token(input.page_token)
        ):
            return _branch(
                "defect", FailureKind.VALIDATION, "project ID, page size, or page token is invalid"
            )
        page_size = input.page_size or 100
        parameters = {"pageSize": str(page_size)}
        if input.page_token:
            parameters["pageToken"] = input.page_token
        path = (
            "/v1/projects/"
            + quote(input.project_id, safe="")
            + "/topics?"
            + urlencode(sorted(parameters.items()))
        )
        exchange = await self.client.get(call, path)
        failure = _read_failure(exchange)
        if failure is not None:
            return failure
        document = _document(exchange.body)
        if document is None:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "PubSub returned invalid topic JSON")
        entries = document.get("topics")
        if entries is None:
            entries = []
        page_token = document.get("nextPageToken")
        if (
            not isinstance(entries, list)
            or len(entries) > page_size
            or (
                page_token is not None
                and (not isinstance(page_token, str) or not _valid_page_token(page_token))
            )
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "PubSub returned invalid topic JSON")
        topics: list[Topic] = []
        prefix = "projects/" + input.project_id + "/topics/"
        for entry in entries:
            name = entry.get("name") if isinstance(entry, dict) else None
            if (
                not isinstance(name, str)
                or not name.startswith(prefix)
                or not name[len(prefix):]
                or len(name[len(prefix):]) > 255
                or any(character in name[len(prefix):] for character in "/\r\n")
            ):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "PubSub returned invalid topic JSON"
                )
            topics.append(Topic(name))
        return QueryAttempt.branch_result(
            "listed", TopicsPage(tuple(topics), page_token or ""), Receipt()
        )
