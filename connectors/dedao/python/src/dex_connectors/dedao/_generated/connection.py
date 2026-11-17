# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_NOTE_TASK_STATUS_DEFINITION
from ..models import GetNoteTaskStatusInput, GetNoteTaskStatusOutput

class Connection:

    __slots__ = ('_get_note_task_status', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_note_task_status: Query[GetNoteTaskStatusInput, GetNoteTaskStatusOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_note_task_status is None:
            raise ValueError("Dedao Brain connection provider and operations are required")
        if get_note_task_status.definition().operation != GET_NOTE_TASK_STATUS_DEFINITION.operation:
            raise ValueError("Dedao Brain connection operation identity differs")
        self.reference = reference
        self._get_note_task_status = get_note_task_status
        self._client = client

    def get_note_task_status(self) -> Query[GetNoteTaskStatusInput, GetNoteTaskStatusOutput]:
        return self._get_note_task_status

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "dedao.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Dedao Brain connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.dedao.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Dedao Brain client is required")
    return Connection(reference, client.get_note_task_status(), client)

setattr(new_connection, '__module__', 'dex_connectors.dedao.connection')


def new_connection_with_operations(
    get_note_task_status: Query[GetNoteTaskStatusInput, GetNoteTaskStatusOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_note_task_status)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.dedao.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Dedao Brain stored credential does not match the connector credential fields")
        if set(material) - set(('apiKey', 'clientID')):
            raise ValueError("Dedao Brain stored credential does not match the connector credential fields")
        api_key = material.get("apiKey")
        if api_key is not None and not (isinstance(api_key, str)):
            raise ValueError("Dedao Brain credential apiKey has an invalid type")
        client_id = material.get("clientID")
        if client_id is not None and not (isinstance(client_id, str)):
            raise ValueError("Dedao Brain credential clientID has an invalid type")
        if not api_key:
            raise ValueError("Dedao Brain credential apiKey is required")
        if not client_id:
            raise ValueError("Dedao Brain credential clientID is required")
        return Credentials(SecretString(api_key), SecretString(client_id))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Dedao Brain connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Dedao Brain project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Dedao Brain project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.dedao.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"timeout", "maxResponseBytes"}:
        raise ValueError("Dedao Brain configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Dedao Brain configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Dedao Brain configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
