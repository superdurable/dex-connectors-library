# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_ROW_DEFINITION, LIST_ROWS_DEFINITION
from ..models import GetRowInput, GetRowOutput, ListRowsInput, ListRowsOutput

class Connection:

    __slots__ = ('_list_rows', '_get_row', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_rows: Query[ListRowsInput, ListRowsOutput],
        get_row: Query[GetRowInput, GetRowOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_rows is None or get_row is None:
            raise ValueError("Coda connection provider and operations are required")
        if list_rows.definition().operation != LIST_ROWS_DEFINITION.operation:
            raise ValueError("Coda connection operation identity differs")
        if get_row.definition().operation != GET_ROW_DEFINITION.operation:
            raise ValueError("Coda connection operation identity differs")
        self.reference = reference
        self._list_rows = list_rows
        self._get_row = get_row
        self._client = client

    def list_rows(self) -> Query[ListRowsInput, ListRowsOutput]:
        return self._list_rows

    def get_row(self) -> Query[GetRowInput, GetRowOutput]:
        return self._get_row

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "coda.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Coda connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.coda.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Coda client is required")
    return Connection(reference, client.list_rows(), client.get_row(), client)

setattr(new_connection, '__module__', 'dex_connectors.coda.connection')


def new_connection_with_operations(
    list_rows: Query[ListRowsInput, ListRowsOutput],
    get_row: Query[GetRowInput, GetRowOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_rows, get_row)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.coda.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        api_token = material.get("api_token")
        if not isinstance(api_token, str) or not api_token:
            raise ValueError("Coda credential api_token is required")
        return Credentials(SecretString(api_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Coda connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Coda project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Coda project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.coda.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Coda configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Coda configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Coda configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Coda configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
