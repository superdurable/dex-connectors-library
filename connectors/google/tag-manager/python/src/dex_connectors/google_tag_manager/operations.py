# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Tag Manager operation decisions matching Go provider recordings."""

from __future__ import annotations

import json
import re
from typing import TYPE_CHECKING, Any, TypeVar
from urllib.parse import urlencode

from dex_connectors.sdk import Call, Failure, FailureKind, QueryAttempt, QueryDefinition, Receipt

from ._generated import definitions
from .client import Exchange
from .models import (
    AccountsPage,
    ContainersPage,
    ListAccountsInput,
    ListContainersInput,
    ListTagsInput,
    TagManagerAccount,
    TagManagerContainer,
    TagManagerTag,
    TagsPage,
)

if TYPE_CHECKING:
    from .client import Client

Output = TypeVar("Output")
_NUMERIC_ID = re.compile(r"^[0-9]{1,30}$")


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "google-tag-manager", operation, message)


def _branch(
    operation: str, branch: str, kind: FailureKind, message: str, empty: Output
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(branch, empty, Receipt(), _failure(operation, kind, message))


def _retry(operation: str, kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[Any]:
    return QueryAttempt.retry(_failure(operation, kind, message), delay)


def _read_failure(operation: str, exchange: Exchange, empty: Output) -> QueryAttempt[Output] | None:
    if exchange.error == "credential":
        return _branch(
            operation,
            "defect",
            FailureKind.AUTHENTICATION,
            "connection credentials are unavailable or invalid",
            empty,
        )
    if exchange.error in ("request", "refresh"):
        return _retry(operation, FailureKind.TRANSPORT, "Google Tag Manager request failed")
    if exchange.status == 429:
        return _retry(
            operation, FailureKind.RATE_LIMIT, "Google rate limited the request", exchange.retry_after
        )
    if exchange.status == 408 or exchange.status >= 500:
        return _retry(
            operation,
            FailureKind.AVAILABILITY,
            "Google Tag Manager is temporarily unavailable",
            exchange.retry_after,
        )
    if exchange.status != 200:
        return _branch(
            operation,
            "providerRejected",
            FailureKind.PROVIDER_REJECTION,
            f"Google rejected HTTP {exchange.status}",
            empty,
        )
    if exchange.error == "too_large":
        return _branch(
            operation,
            "invalidResponse",
            FailureKind.RESPONSE_TOO_LARGE,
            "Google response exceeded the configured limit",
            empty,
        )
    if exchange.error:
        return _retry(operation, FailureKind.TRANSPORT, "Google response was interrupted")
    return None


def _document(body: bytes) -> dict[str, Any] | None:
    try:
        value: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return None
    return value if isinstance(value, dict) else None


def _valid_id(value: str) -> bool:
    return isinstance(value, str) and bool(_NUMERIC_ID.fullmatch(value))


def _valid_token(value: str) -> bool:
    return isinstance(value, str) and len(value.encode()) <= 4096 and "\r" not in value and "\n" not in value


def _valid_resource(path: object, prefix: str) -> bool:
    return isinstance(path, str) and path.startswith(prefix + "/") and _valid_id(path[len(prefix) + 1 :])


def _page_path(path: str, token: str) -> str:
    return path + ("?" + urlencode({"pageToken": token}) if token else "")


def _page_fields(document: dict[str, Any] | None, field: str) -> tuple[list[Any], str] | None:
    if document is None:
        return None
    entries = document.get(field, [])
    next_token = document.get("nextPageToken", "")
    if not isinstance(entries, list) or len(entries) > 1000 or not _valid_token(next_token):
        return None
    return entries, next_token


class ListAccountsOperation:
    """Read one page of account identities."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated account Query contract."""
        return definitions.LIST_ACCOUNTS_DEFINITION

    async def invoke(self, call: Call, input: ListAccountsInput) -> QueryAttempt[AccountsPage]:
        """List accessible accounts without retaining private API fields."""
        empty = AccountsPage()
        if not _valid_token(input.page_token):
            return _branch(
                "listAccounts", "defect", FailureKind.VALIDATION, "account page token is invalid", empty
            )
        exchange = await self.client.request(call, _page_path("/tagmanager/v2/accounts", input.page_token))
        failure = _read_failure("listAccounts", exchange, empty)
        if failure is not None:
            return failure
        fields = _page_fields(_document(exchange.body), "account")
        if fields is None:
            return _branch(
                "listAccounts",
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Google returned invalid account JSON",
                empty,
            )
        entries, next_token = fields
        accounts: list[TagManagerAccount] = []
        for entry in entries:
            if (
                not isinstance(entry, dict)
                or not _valid_resource(entry.get("path"), "accounts")
                or not isinstance(entry.get("name"), str)
                or not entry["name"]
            ):
                return _branch(
                    "listAccounts",
                    "invalidResponse",
                    FailureKind.PROTOCOL,
                    "Google returned incomplete account metadata",
                    empty,
                )
            accounts.append(TagManagerAccount(entry["path"], entry["name"]))
        return QueryAttempt.branch_result("listed", AccountsPage(tuple(accounts), next_token), Receipt())


class ListContainersOperation:
    """Read one page of container identities in an account."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated container Query contract."""
        return definitions.LIST_CONTAINERS_DEFINITION

    async def invoke(self, call: Call, input: ListContainersInput) -> QueryAttempt[ContainersPage]:
        """List containers for a numeric account ID."""
        empty = ContainersPage()
        if not _valid_id(input.account_id) or not _valid_token(input.page_token):
            return _branch(
                "listContainers", "defect", FailureKind.VALIDATION, "container page input is invalid", empty
            )
        prefix = f"accounts/{input.account_id}/containers"
        exchange = await self.client.request(call, _page_path(f"/tagmanager/v2/{prefix}", input.page_token))
        failure = _read_failure("listContainers", exchange, empty)
        if failure is not None:
            return failure
        fields = _page_fields(_document(exchange.body), "container")
        if fields is None:
            return _branch(
                "listContainers",
                "invalidResponse",
                FailureKind.PROTOCOL,
                "Google returned invalid container JSON",
                empty,
            )
        entries, next_token = fields
        containers: list[TagManagerContainer] = []
        for entry in entries:
            if (
                not isinstance(entry, dict)
                or not _valid_resource(entry.get("path"), prefix)
                or not isinstance(entry.get("name"), str)
                or not entry["name"]
                or not isinstance(entry.get("publicId"), str)
                or not entry["publicId"]
            ):
                return _branch(
                    "listContainers",
                    "invalidResponse",
                    FailureKind.PROTOCOL,
                    "Google returned incomplete container metadata",
                    empty,
                )
            containers.append(TagManagerContainer(entry["path"], entry["name"], entry["publicId"]))
        return QueryAttempt.branch_result("listed", ContainersPage(tuple(containers), next_token), Receipt())


class ListTagsOperation:
    """Read one page of tag identities in a workspace."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated tag Query contract."""
        return definitions.LIST_TAGS_DEFINITION

    async def invoke(self, call: Call, input: ListTagsInput) -> QueryAttempt[TagsPage]:
        """List tag names and types without retaining configuration parameters."""
        empty = TagsPage()
        if not all(
            (
                _valid_id(input.account_id),
                _valid_id(input.container_id),
                _valid_id(input.workspace_id),
                _valid_token(input.page_token),
            )
        ):
            return _branch("listTags", "defect", FailureKind.VALIDATION, "tag page input is invalid", empty)
        prefix = (
            f"accounts/{input.account_id}/containers/{input.container_id}"
            f"/workspaces/{input.workspace_id}/tags"
        )
        exchange = await self.client.request(call, _page_path(f"/tagmanager/v2/{prefix}", input.page_token))
        failure = _read_failure("listTags", exchange, empty)
        if failure is not None:
            return failure
        fields = _page_fields(_document(exchange.body), "tag")
        if fields is None:
            return _branch(
                "listTags", "invalidResponse", FailureKind.PROTOCOL, "Google returned invalid tag JSON", empty
            )
        entries, next_token = fields
        tags: list[TagManagerTag] = []
        for entry in entries:
            if (
                not isinstance(entry, dict)
                or not _valid_resource(entry.get("path"), prefix)
                or not isinstance(entry.get("name"), str)
                or not entry["name"]
                or not isinstance(entry.get("type"), str)
                or not entry["type"]
            ):
                return _branch(
                    "listTags",
                    "invalidResponse",
                    FailureKind.PROTOCOL,
                    "Google returned incomplete tag metadata",
                    empty,
                )
            tags.append(TagManagerTag(entry["path"], entry["name"], entry["type"]))
        return QueryAttempt.branch_result("listed", TagsPage(tuple(tags), next_token), Receipt())
