# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, LIST_PROJECTS_DEFINITION, LIST_TIME_ENTRIES_DEFINITION
from ..models import ListProjectsInput, ListTimeEntriesInput, ProjectPage, TimeEntryPage

class Connection:

    __slots__ = ('_list_projects', '_list_time_entries', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_projects: Query[ListProjectsInput, ProjectPage],
        list_time_entries: Query[ListTimeEntriesInput, TimeEntryPage],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_projects is None or list_time_entries is None:
            raise ValueError("Harvest connection provider and operations are required")
        if list_projects.definition().operation != LIST_PROJECTS_DEFINITION.operation:
            raise ValueError("Harvest connection operation identity differs")
        if list_time_entries.definition().operation != LIST_TIME_ENTRIES_DEFINITION.operation:
            raise ValueError("Harvest connection operation identity differs")
        self.reference = reference
        self._list_projects = list_projects
        self._list_time_entries = list_time_entries
        self._client = client

    def list_projects(self) -> Query[ListProjectsInput, ProjectPage]:
        return self._list_projects

    def list_time_entries(self) -> Query[ListTimeEntriesInput, TimeEntryPage]:
        return self._list_time_entries

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "harvest.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Harvest connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.harvest.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Harvest client is required")
    return Connection(reference, client.list_projects(), client.list_time_entries(), client)

setattr(new_connection, '__module__', 'dex_connectors.harvest.connection')


def new_connection_with_operations(
    list_projects: Query[ListProjectsInput, ProjectPage],
    list_time_entries: Query[ListTimeEntriesInput, TimeEntryPage],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_projects, list_time_entries)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.harvest.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Harvest stored credential does not match the connector credential fields")
        if set(material) - set(('api_key',)):
            raise ValueError("Harvest stored credential does not match the connector credential fields")
        api_key = material.get("api_key")
        if api_key is not None and not (isinstance(api_key, str)):
            raise ValueError("Harvest credential api_key has an invalid type")
        if not api_key:
            raise ValueError("Harvest credential api_key is required")
        return Credentials(SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Harvest connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Harvest project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Harvest project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.harvest.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes", "accountId"}:
        raise ValueError("Harvest configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Harvest configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Harvest configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Harvest configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "accountId" in value:
        account_id = value["accountId"]
        if not (isinstance(account_id, str)):
            raise ValueError("Harvest configuration accountId has an invalid type")
        config_arguments['account_id'] = account_id
    else:
        raise ValueError("Harvest configuration accountId is required")
    return Config(**config_arguments)
