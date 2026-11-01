# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Sentry connection and project credential binding."""

from typing import Any

import httpx
from dex_connectors.sdk import Call, ConnectionRef, Mutation, Query, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from .client import (
    CONNECTOR_ID,
    GET_ISSUE_DEFINITION,
    LIST_ISSUES_DEFINITION,
    UPDATE_ISSUE_STATUS_DEFINITION,
    Client,
    Config,
    Credentials,
)
from .models import GetIssueInput, Issue, IssuePage, ListIssuesInput, UpdateIssueStatusInput


class Connection:
    """Nonserializable Sentry operation bindings for a named project connection."""

    __slots__ = ("_list_issues", "_get_issue", "_update_issue_status", "_client", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        list_issues: Query[ListIssuesInput, IssuePage],
        get_issue: Query[GetIssueInput, Issue],
        update_issue_status: Mutation[UpdateIssueStatusInput, Issue],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or None in (list_issues, get_issue, update_issue_status):
            raise ValueError("Sentry connection provider and operations are required")
        if (
            list_issues.definition().operation != LIST_ISSUES_DEFINITION.operation
            or get_issue.definition().operation != GET_ISSUE_DEFINITION.operation
            or update_issue_status.definition().operation != UPDATE_ISSUE_STATUS_DEFINITION.operation
        ):
            raise ValueError("Sentry connection operation identity differs")
        self.reference = reference
        self._list_issues = list_issues
        self._get_issue = get_issue
        self._update_issue_status = update_issue_status
        self._client = client

    def list_issues(self) -> Query[ListIssuesInput, IssuePage]:
        """Return the issue search Query used by an application Step."""
        return self._list_issues

    def get_issue(self) -> Query[GetIssueInput, Issue]:
        """Return the issue lookup Query used by an application Step."""
        return self._get_issue

    def update_issue_status(self) -> Mutation[UpdateIssueStatusInput, Issue]:
        """Return the status update Mutation used by an application Step."""
        return self._update_issue_status

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "sentry.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("Sentry connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("Sentry client is required")
    return Connection(
        reference, client.list_issues(), client.get_issue(), client.update_issue_status(), client
    )


def new_connection_with_operations(
    list_issues: Query[ListIssuesInput, IssuePage],
    get_issue: Query[GetIssueInput, Issue],
    update_issue_status: Mutation[UpdateIssueStatusInput, Issue],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock operations to the same connection identity as the real provider."""
    return Connection(reference, list_issues, get_issue, update_issue_status)


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        value = material.get("api_key")
        if not isinstance(value, str) or not value:
            raise ValueError("Sentry credential api_key is required")
        return Credentials(SecretString(value))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared ordinary settings and resolve the current token per call."""
    if project is None:
        raise ValueError("Sentry connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Sentry project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Sentry project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Sentry configuration has unknown fields")
    base_url = value.get("baseUrl", "")
    timeout = value.get("timeout", 0)
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not isinstance(base_url, str):
        raise ValueError("Sentry configuration baseUrl must be a string")
    if type(timeout) is not int or type(max_response_bytes) is not int:
        raise ValueError("Sentry configuration timeout and maxResponseBytes must be integers")
    return Config(base_url, timeout / 1_000_000_000, max_response_bytes)
