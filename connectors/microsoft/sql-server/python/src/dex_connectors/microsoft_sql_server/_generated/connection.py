# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, EXECUTE_DEFINITION, QUERY_DEFINITION
from ..models import ExecuteStatementInput, QueryRowsInput, RowSet, StatementExecution

class Connection:

    __slots__ = ('_query', '_execute', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        query: Query[QueryRowsInput, RowSet],
        execute: Mutation[ExecuteStatementInput, StatementExecution],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or query is None or execute is None:
            raise ValueError("Microsoft SQL Server connection provider and operations are required")
        if query.definition().operation != QUERY_DEFINITION.operation:
            raise ValueError("Microsoft SQL Server connection operation identity differs")
        if execute.definition().operation != EXECUTE_DEFINITION.operation:
            raise ValueError("Microsoft SQL Server connection operation identity differs")
        self.reference = reference
        self._query = query
        self._execute = execute
        self._client = client

    def query(self) -> Query[QueryRowsInput, RowSet]:
        return self._query

    def execute(self) -> Mutation[ExecuteStatementInput, StatementExecution]:
        return self._execute

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "microsoft_sql_server.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Microsoft SQL Server connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.microsoft_sql_server.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Microsoft SQL Server client is required")
    return Connection(reference, client.query(), client.execute(), client)

setattr(new_connection, '__module__', 'dex_connectors.microsoft_sql_server.connection')


def new_connection_with_operations(
    query: Query[QueryRowsInput, RowSet],
    execute: Mutation[ExecuteStatementInput, StatementExecution],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, query, execute)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.microsoft_sql_server.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Microsoft SQL Server stored credential does not match the connector credential fields")
        if set(material) - set(('password',)):
            raise ValueError("Microsoft SQL Server stored credential does not match the connector credential fields")
        password = material.get("password")
        if password is not None and not (isinstance(password, str)):
            raise ValueError("Microsoft SQL Server credential password has an invalid type")
        if not password:
            raise ValueError("Microsoft SQL Server credential password is required")
        return Credentials(SecretString(password))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Microsoft SQL Server connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Microsoft SQL Server project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Microsoft SQL Server project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.microsoft_sql_server.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"host", "port", "database", "user", "encrypt", "connectTimeout", "statementTimeout", "maxRows", "maxResponseBytes"}:
        raise ValueError("Microsoft SQL Server configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "host" in value:
        host = value["host"]
        if not (isinstance(host, str)):
            raise ValueError("Microsoft SQL Server configuration host has an invalid type")
        config_arguments['host'] = host
    else:
        raise ValueError("Microsoft SQL Server configuration host is required")
    if "port" in value:
        port = value["port"]
        if not (type(port) is int):
            raise ValueError("Microsoft SQL Server configuration port has an invalid type")
        config_arguments['port'] = port
    if "database" in value:
        database = value["database"]
        if not (isinstance(database, str)):
            raise ValueError("Microsoft SQL Server configuration database has an invalid type")
        config_arguments['database'] = database
    else:
        raise ValueError("Microsoft SQL Server configuration database is required")
    if "user" in value:
        user = value["user"]
        if not (isinstance(user, str)):
            raise ValueError("Microsoft SQL Server configuration user has an invalid type")
        config_arguments['user'] = user
    else:
        raise ValueError("Microsoft SQL Server configuration user is required")
    if "encrypt" in value:
        encrypt = value["encrypt"]
        if not (isinstance(encrypt, str) and encrypt in ('mandatory', 'strict', 'trust-server-certificate', 'disable')):
            raise ValueError("Microsoft SQL Server configuration encrypt has an invalid type")
        config_arguments['encrypt'] = encrypt
    if "connectTimeout" in value:
        connect_timeout = value["connectTimeout"]
        if not (type(connect_timeout) is int):
            raise ValueError("Microsoft SQL Server configuration connectTimeout has an invalid type")
        config_arguments['connect_timeout_seconds'] = connect_timeout / 1_000_000_000
    if "statementTimeout" in value:
        statement_timeout = value["statementTimeout"]
        if not (type(statement_timeout) is int):
            raise ValueError("Microsoft SQL Server configuration statementTimeout has an invalid type")
        config_arguments['statement_timeout_seconds'] = statement_timeout / 1_000_000_000
    if "maxRows" in value:
        max_rows = value["maxRows"]
        if not (type(max_rows) is int):
            raise ValueError("Microsoft SQL Server configuration maxRows has an invalid type")
        config_arguments['max_rows'] = max_rows
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Microsoft SQL Server configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
