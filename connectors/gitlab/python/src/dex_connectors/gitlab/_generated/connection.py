# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_PROJECT_DEFINITION, LIST_ISSUES_DEFINITION, UPDATE_ISSUE_LABELS_DEFINITION
from ..models import GetProjectInput, Issue, IssuesPage, ListIssuesInput, Project, UpdateIssueLabelsInput

class Connection:

    __slots__ = ('_get_project', '_list_issues', '_update_issue_labels', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_project: Query[GetProjectInput, Project],
        list_issues: Query[ListIssuesInput, IssuesPage],
        update_issue_labels: Mutation[UpdateIssueLabelsInput, Issue],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_project is None or list_issues is None or update_issue_labels is None:
            raise ValueError("GitLab connection provider and operations are required")
        if get_project.definition().operation != GET_PROJECT_DEFINITION.operation:
            raise ValueError("GitLab connection operation identity differs")
        if list_issues.definition().operation != LIST_ISSUES_DEFINITION.operation:
            raise ValueError("GitLab connection operation identity differs")
        if update_issue_labels.definition().operation != UPDATE_ISSUE_LABELS_DEFINITION.operation:
            raise ValueError("GitLab connection operation identity differs")
        self.reference = reference
        self._get_project = get_project
        self._list_issues = list_issues
        self._update_issue_labels = update_issue_labels
        self._client = client

    def get_project(self) -> Query[GetProjectInput, Project]:
        return self._get_project

    def list_issues(self) -> Query[ListIssuesInput, IssuesPage]:
        return self._list_issues

    def update_issue_labels(self) -> Mutation[UpdateIssueLabelsInput, Issue]:
        return self._update_issue_labels

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "gitlab.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("GitLab connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.gitlab.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("GitLab client is required")
    return Connection(reference, client.get_project(), client.list_issues(), client.update_issue_labels(), client)

setattr(new_connection, '__module__', 'dex_connectors.gitlab.connection')


def new_connection_with_operations(
    get_project: Query[GetProjectInput, Project],
    list_issues: Query[ListIssuesInput, IssuesPage],
    update_issue_labels: Mutation[UpdateIssueLabelsInput, Issue],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_project, list_issues, update_issue_labels)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.gitlab.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("GitLab stored credential does not match the connector credential fields")
        if set(material) - set(('access_token',)):
            raise ValueError("GitLab stored credential does not match the connector credential fields")
        access_token = material.get("access_token")
        if access_token is not None and not (isinstance(access_token, str)):
            raise ValueError("GitLab credential access_token has an invalid type")
        if not access_token:
            raise ValueError("GitLab credential access_token is required")
        return Credentials(SecretString(access_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
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

setattr(new_project_connection, '__module__', 'dex_connectors.gitlab.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("GitLab configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("GitLab configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("GitLab configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("GitLab configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
