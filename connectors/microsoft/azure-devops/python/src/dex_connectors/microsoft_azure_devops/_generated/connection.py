# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_WORK_ITEM_DEFINITION, LIST_PROJECTS_DEFINITION
from ..models import GetWorkItemInput, ListProjectsInput, Projects, WorkItemMetadata

class Connection:

    __slots__ = ('_list_projects', '_get_work_item', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_projects: Query[ListProjectsInput, Projects],
        get_work_item: Query[GetWorkItemInput, WorkItemMetadata],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_projects is None or get_work_item is None:
            raise ValueError("Azure DevOps connection provider and operations are required")
        if list_projects.definition().operation != LIST_PROJECTS_DEFINITION.operation:
            raise ValueError("Azure DevOps connection operation identity differs")
        if get_work_item.definition().operation != GET_WORK_ITEM_DEFINITION.operation:
            raise ValueError("Azure DevOps connection operation identity differs")
        self.reference = reference
        self._list_projects = list_projects
        self._get_work_item = get_work_item
        self._client = client

    def list_projects(self) -> Query[ListProjectsInput, Projects]:
        return self._list_projects

    def get_work_item(self) -> Query[GetWorkItemInput, WorkItemMetadata]:
        return self._get_work_item

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "microsoft_azure_devops.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Azure DevOps connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.microsoft_azure_devops.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Azure DevOps client is required")
    return Connection(reference, client.list_projects(), client.get_work_item(), client)

setattr(new_connection, '__module__', 'dex_connectors.microsoft_azure_devops.connection')


def new_connection_with_operations(
    list_projects: Query[ListProjectsInput, Projects],
    get_work_item: Query[GetWorkItemInput, WorkItemMetadata],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_projects, get_work_item)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.microsoft_azure_devops.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Azure DevOps stored credential does not match the connector credential fields")
        if set(material) - set(('personal_access_token',)):
            raise ValueError("Azure DevOps stored credential does not match the connector credential fields")
        personal_access_token = material.get("personal_access_token")
        if personal_access_token is not None and not (isinstance(personal_access_token, str)):
            raise ValueError("Azure DevOps credential personal_access_token has an invalid type")
        if not personal_access_token:
            raise ValueError("Azure DevOps credential personal_access_token is required")
        return Credentials(SecretString(personal_access_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Azure DevOps connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Azure DevOps project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Azure DevOps project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.microsoft_azure_devops.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Azure DevOps configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Azure DevOps configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    else:
        raise ValueError("Azure DevOps configuration baseUrl is required")
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Azure DevOps configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Azure DevOps configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
