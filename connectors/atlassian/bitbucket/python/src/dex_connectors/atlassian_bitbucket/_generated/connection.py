# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, LIST_PULL_REQUESTS_DEFINITION, LIST_REPOSITORIES_DEFINITION
from ..models import ListPullRequestsInput, ListRepositoriesInput, PullRequestPage, RepositoryPage

class Connection:

    __slots__ = ('_list_repositories', '_list_pull_requests', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_repositories: Query[ListRepositoriesInput, RepositoryPage],
        list_pull_requests: Query[ListPullRequestsInput, PullRequestPage],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_repositories is None or list_pull_requests is None:
            raise ValueError("Bitbucket connection provider and operations are required")
        if list_repositories.definition().operation != LIST_REPOSITORIES_DEFINITION.operation:
            raise ValueError("Bitbucket connection operation identity differs")
        if list_pull_requests.definition().operation != LIST_PULL_REQUESTS_DEFINITION.operation:
            raise ValueError("Bitbucket connection operation identity differs")
        self.reference = reference
        self._list_repositories = list_repositories
        self._list_pull_requests = list_pull_requests
        self._client = client

    def list_repositories(self) -> Query[ListRepositoriesInput, RepositoryPage]:
        return self._list_repositories

    def list_pull_requests(self) -> Query[ListPullRequestsInput, PullRequestPage]:
        return self._list_pull_requests

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "atlassian_bitbucket.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Bitbucket connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.atlassian_bitbucket.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Bitbucket client is required")
    return Connection(reference, client.list_repositories(), client.list_pull_requests(), client)

setattr(new_connection, '__module__', 'dex_connectors.atlassian_bitbucket.connection')


def new_connection_with_operations(
    list_repositories: Query[ListRepositoriesInput, RepositoryPage],
    list_pull_requests: Query[ListPullRequestsInput, PullRequestPage],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_repositories, list_pull_requests)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.atlassian_bitbucket.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        access_token = material.get("access_token")
        if not isinstance(access_token, str) or not access_token:
            raise ValueError("Bitbucket credential access_token is required")
        return Credentials(SecretString(access_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Bitbucket connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Bitbucket project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Bitbucket project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.atlassian_bitbucket.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Bitbucket configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Bitbucket configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Bitbucket configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Bitbucket configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
