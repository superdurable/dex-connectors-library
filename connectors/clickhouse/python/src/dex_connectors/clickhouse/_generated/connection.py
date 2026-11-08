# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, LIST_TABLES_DEFINITION
from ..models import ListTablesInput, TablesPage

class Connection:

    __slots__ = ('_list_tables', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_tables: Query[ListTablesInput, TablesPage],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_tables is None:
            raise ValueError("ClickHouse connection provider and operations are required")
        if list_tables.definition().operation != LIST_TABLES_DEFINITION.operation:
            raise ValueError("ClickHouse connection operation identity differs")
        self.reference = reference
        self._list_tables = list_tables
        self._client = client

    def list_tables(self) -> Query[ListTablesInput, TablesPage]:
        return self._list_tables

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "clickhouse.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("ClickHouse connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.clickhouse.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("ClickHouse client is required")
    return Connection(reference, client.list_tables(), client)

setattr(new_connection, '__module__', 'dex_connectors.clickhouse.connection')


def new_connection_with_operations(
    list_tables: Query[ListTablesInput, TablesPage],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_tables)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.clickhouse.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("ClickHouse stored credential does not match the connector credential fields")
        if set(material) - set(('username', 'password')):
            raise ValueError("ClickHouse stored credential does not match the connector credential fields")
        username = material.get("username")
        if username is not None and not (isinstance(username, str)):
            raise ValueError("ClickHouse credential username has an invalid type")
        password = material.get("password")
        if password is not None and not (isinstance(password, str)):
            raise ValueError("ClickHouse credential password has an invalid type")
        if not username:
            raise ValueError("ClickHouse credential username is required")
        if not password:
            raise ValueError("ClickHouse credential password is required")
        return Credentials(username, SecretString(password))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("ClickHouse connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("ClickHouse project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("ClickHouse project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.clickhouse.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("ClickHouse configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("ClickHouse configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    else:
        raise ValueError("ClickHouse configuration baseUrl is required")
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("ClickHouse configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("ClickHouse configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
