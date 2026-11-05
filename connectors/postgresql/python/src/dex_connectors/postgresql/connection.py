# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# connectorctl: handwritten facade
"""Named PostgreSQL connections with call-time private credentials."""

from typing import Any

from dex_connectors.sdk import Call, ConnectionRef, Mutation, Query, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ._generated.definitions import CONNECTOR_ID, EXECUTE_DEFINITION, QUERY_DEFINITION
from .client import Client, Config, Credentials
from .models import ExecuteStatementInput, QueryRowsInput, RowSet, StatementExecution


class Connection:
    """Bind the two operations to one static Dex connection name."""

    __slots__ = ("_query", "_execute", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        query: Query[QueryRowsInput, RowSet],
        execute: Mutation[ExecuteStatementInput, StatementExecution],
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or query is None or execute is None:
            raise ValueError("PostgreSQL connection provider and operations are required")
        if query.definition().operation != QUERY_DEFINITION.operation:
            raise ValueError("PostgreSQL query identity differs")
        if execute.definition().operation != EXECUTE_DEFINITION.operation:
            raise ValueError("PostgreSQL execute identity differs")
        self.reference = reference
        self._query = query
        self._execute = execute

    def query(self) -> Query[QueryRowsInput, RowSet]:
        """Return the read-only query operation."""
        return self._query

    def execute(self) -> Mutation[ExecuteStatementInput, StatementExecution]:
        """Return the transactional write operation."""
        return self._execute

    async def aclose(self) -> None:
        """Close connection resources; each call already owns its own session."""

    def __repr__(self) -> str:
        """Keep credentials and provider state out of logs."""
        return "postgresql.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Reject serialization into durable Flow state."""
        raise TypeError("PostgreSQL connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a provider client to a logical connection reference."""
    if client is None:
        raise ValueError("PostgreSQL client is required")
    return Connection(reference, client.query(), client.execute())


def new_connection_with_operations(
    query: Query[QueryRowsInput, RowSet],
    execute: Mutation[ExecuteStatementInput, StatementExecution],
    reference: ConnectionRef,
) -> Connection:
    """Bind test or mock operations to a PostgreSQL connection name."""
    return Connection(reference, query, execute)


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        password = material.get("password")
        if not isinstance(password, str) or not password:
            raise ValueError("PostgreSQL credential password is required")
        return Credentials(SecretString(password))


def new_project_connection(project: LoadedProject, connection_name: str) -> Connection:
    """Open a declared project connection with credentials resolved per call."""
    if project is None:
        raise ValueError("PostgreSQL project configuration is required")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1 or matching[0].provider != CONNECTOR_ID:
        raise ValueError("PostgreSQL project connection is missing, duplicated, or mismatched")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, ConnectionKey(CONNECTOR_ID, connection_name)))
    return new_connection(client, reference)


def _decode_config(value: dict[str, Any]) -> Config:
    names = {
        "host",
        "port",
        "database",
        "user",
        "sslMode",
        "connectTimeout",
        "statementTimeout",
        "maxRows",
        "maxResponseBytes",
    }
    if set(value) - names:
        raise ValueError("PostgreSQL configuration has unknown fields")
    for name in ("host", "database", "user"):
        if not isinstance(value.get(name), str):
            raise ValueError(f"PostgreSQL configuration {name} is required")
    for name in ("port", "maxRows", "maxResponseBytes"):
        if name in value and type(value[name]) is not int:
            raise ValueError(f"PostgreSQL configuration {name} must be an integer")
    if "sslMode" in value and not isinstance(value["sslMode"], str):
        raise ValueError("PostgreSQL configuration sslMode must be a string")
    for name in ("connectTimeout", "statementTimeout"):
        if name in value and type(value[name]) not in (int, float):
            raise ValueError(f"PostgreSQL configuration {name} must be a duration")
    return Config(
        host=value["host"],
        database=value["database"],
        user=value["user"],
        port=value.get("port", 5432),
        ssl_mode=value.get("sslMode", "require"),
        connect_timeout_seconds=value.get("connectTimeout", 5_000_000_000) / 1_000_000_000,
        statement_timeout_seconds=value.get("statementTimeout", 5_000_000_000) / 1_000_000_000,
        max_rows=value.get("maxRows", 1000),
        max_response_bytes=value.get("maxResponseBytes", 1048576),
    )
