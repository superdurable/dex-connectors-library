# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""GitLab connection and project credential binding."""

from typing import Any

import httpx
from dex_connectors.sdk import Call, ConnectionRef, Mutation, Query, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from .client import (
    CONNECTOR_ID,
    GET_PROJECT_DEFINITION,
    LIST_ISSUES_DEFINITION,
    UPDATE_ISSUE_LABELS_DEFINITION,
    Client,
    Config,
    Credentials,
)
from .models import GetProjectInput, Issue, IssuesPage, ListIssuesInput, Project, UpdateIssueLabelsInput


class Connection:
    """Nonserializable GitLab operation bindings for a named project connection."""

    __slots__ = ("_get_project", "_list_issues", "_update_issue_labels", "_client", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        get_project: Query[GetProjectInput, Project],
        list_issues: Query[ListIssuesInput, IssuesPage],
        update_issue_labels: Mutation[UpdateIssueLabelsInput, Issue],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if (
            reference.provider != CONNECTOR_ID
            or get_project is None
            or list_issues is None
            or update_issue_labels is None
        ):
            raise ValueError("GitLab connection provider and operations are required")
        if (
            get_project.definition().operation != GET_PROJECT_DEFINITION.operation
            or list_issues.definition().operation != LIST_ISSUES_DEFINITION.operation
            or update_issue_labels.definition().operation != UPDATE_ISSUE_LABELS_DEFINITION.operation
        ):
            raise ValueError("GitLab connection operation identity differs")
        self.reference = reference
        self._get_project = get_project
        self._list_issues = list_issues
        self._update_issue_labels = update_issue_labels
        self._client = client

    def get_project(self) -> Query[GetProjectInput, Project]:
        """Return the project lookup Query used by an application Step."""
        return self._get_project

    def list_issues(self) -> Query[ListIssuesInput, IssuesPage]:
        """Return the issue page Query used by an application Step."""
        return self._list_issues

    def update_issue_labels(self) -> Mutation[UpdateIssueLabelsInput, Issue]:
        """Return the issue label replacement Mutation used by an application Step."""
        return self._update_issue_labels

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "gitlab.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("GitLab connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("GitLab client is required")
    return Connection(
        reference, client.get_project(), client.list_issues(), client.update_issue_labels(), client
    )


def new_connection_with_operations(
    get_project: Query[GetProjectInput, Project],
    list_issues: Query[ListIssuesInput, IssuesPage],
    update_issue_labels: Mutation[UpdateIssueLabelsInput, Issue],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock operations to the same connection identity as the real provider."""
    return Connection(reference, get_project, list_issues, update_issue_labels)


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        value = material.get("access_token")
        if not isinstance(value, str) or not value:
            raise ValueError("GitLab credential access_token is required")
        return Credentials(SecretString(value))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared ordinary settings and resolve the current access token per call."""
    if project is None:
        raise ValueError("GitLab connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("GitLab project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("GitLab project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("GitLab configuration has unknown fields")
    base_url = value.get("baseUrl", "")
    timeout = value.get("timeout", 0)
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not isinstance(base_url, str):
        raise ValueError("GitLab configuration baseUrl must be a string")
    if any(type(number) is not int for number in (timeout, max_response_bytes)):
        raise ValueError("GitLab configuration timeout and maxResponseBytes must be integers")
    return Config(base_url, timeout / 1_000_000_000, max_response_bytes)
