# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated operation bindings for one named Dex connection."""

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_ISSUE_DEFINITION, LIST_ISSUES_DEFINITION, UPDATE_ISSUE_STATUS_DEFINITION
from ..models import GetIssueInput, Issue, IssuePage, ListIssuesInput, UpdateIssueStatusInput

class Connection:
    """Nonserializable Sentry operation binding."""

    __slots__ = ('_list_issues', '_get_issue', '_update_issue_status', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_issues: Query[ListIssuesInput, IssuePage],
        get_issue: Query[GetIssueInput, Issue],
        update_issue_status: Mutation[UpdateIssueStatusInput, Issue],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_issues is None or get_issue is None or update_issue_status is None:
            raise ValueError("Sentry connection provider and operations are required")
        if list_issues.definition().operation != LIST_ISSUES_DEFINITION.operation:
            raise ValueError("Sentry connection operation identity differs")
        if get_issue.definition().operation != GET_ISSUE_DEFINITION.operation:
            raise ValueError("Sentry connection operation identity differs")
        if update_issue_status.definition().operation != UPDATE_ISSUE_STATUS_DEFINITION.operation:
            raise ValueError("Sentry connection operation identity differs")
        self.reference = reference
        self._list_issues = list_issues
        self._get_issue = get_issue
        self._update_issue_status = update_issue_status
        self._client = client

    def list_issues(self) -> Query[ListIssuesInput, IssuePage]:
        """Return the list_issues operation for a Dex Step."""
        return self._list_issues

    def get_issue(self) -> Query[GetIssueInput, Issue]:
        """Return the get_issue operation for a Dex Step."""
        return self._get_issue

    def update_issue_status(self) -> Mutation[UpdateIssueStatusInput, Issue]:
        """Return the update_issue_status operation for a Dex Step."""
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

setattr(Connection, '__module__', 'dex_connectors.sentry.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a provider client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("Sentry client is required")
    return Connection(reference, client.list_issues(), client.get_issue(), client.update_issue_status(), client)

setattr(new_connection, '__module__', 'dex_connectors.sentry.connection')


def new_connection_with_operations(
    list_issues: Query[ListIssuesInput, IssuePage],
    get_issue: Query[GetIssueInput, Issue],
    update_issue_status: Mutation[UpdateIssueStatusInput, Issue],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock operations to the same logical connection identity."""
    return Connection(reference, list_issues, get_issue, update_issue_status)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.sentry.connection')


class _ProjectCredentialSource:
    """Resolve current secret material at each provider attempt."""

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        api_key = material.get("api_key")
        if not isinstance(api_key, str) or not api_key:
            raise ValueError("Sentry credential api_key is required")
        return Credentials(SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared project settings and resolve provider credentials per call."""
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

setattr(new_project_connection, '__module__', 'dex_connectors.sentry.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Sentry configuration has unknown fields")
    base_url = value.get("baseUrl", '')
    if not isinstance(base_url, str):
        raise ValueError("Sentry configuration baseUrl has an invalid type")
    timeout = value.get("timeout", 0)
    if not type(timeout) is int:
        raise ValueError("Sentry configuration timeout has an invalid type")
    timeout_seconds = timeout / 1_000_000_000
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not type(max_response_bytes) is int:
        raise ValueError("Sentry configuration maxResponseBytes has an invalid type")
    return Config(base_url=base_url, timeout_seconds=timeout_seconds, max_response_bytes=max_response_bytes)
