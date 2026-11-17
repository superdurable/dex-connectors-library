# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# connectorctl: handwritten facade
"""Named MySQL connections with call-time private credentials."""

from typing import Any

import httpx
from dex_connectors.sdk import Call, ConnectionRef, Mutation, Query, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ._generated.definitions import (
    CONNECTOR_ID,
    EXECUTE_DEFINITION,
    EXECUTE_NON_IDEMPOTENT_DEFINITION,
    QUERY_DEFINITION,
)
from .client import Client, Config, Credentials
from .models import ExecuteStatementInput, QueryRowsInput, RowSet, StatementExecution

__all__ = ["Connection", "new_connection", "new_connection_with_operations", "new_project_connection"]


class Connection:
    """Bind the three operations to one static Dex connection name."""

    __slots__ = ("_query", "_execute", "_execute_non_idempotent", "_client", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        query: Query[QueryRowsInput, RowSet],
        execute: Mutation[ExecuteStatementInput, StatementExecution],
        execute_non_idempotent: Mutation[ExecuteStatementInput, StatementExecution] | None = None,
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or query is None or execute is None:
            raise ValueError("MySQL connection provider and operations are required")
        if query.definition().operation != QUERY_DEFINITION.operation:
            raise ValueError("MySQL connection operation identity differs")
        if execute.definition().operation != EXECUTE_DEFINITION.operation:
            raise ValueError("MySQL connection operation identity differs")
        if (
            execute_non_idempotent is not None
            and execute_non_idempotent.definition().operation != EXECUTE_NON_IDEMPOTENT_DEFINITION.operation
        ):
            raise ValueError("MySQL connection operation identity differs")
        self.reference = reference
        self._query = query
        self._execute = execute
        self._execute_non_idempotent = execute_non_idempotent
        self._client = client

    def query(self) -> Query[QueryRowsInput, RowSet]:
        """Return the read-only query operation."""
        return self._query

    def execute(self) -> Mutation[ExecuteStatementInput, StatementExecution]:
        """Return the repeat-safe transactional write operation."""
        return self._execute

    def execute_non_idempotent(self) -> Mutation[ExecuteStatementInput, StatementExecution]:
        """Return the fenced write operation that never runs its statement twice.

        Raises ValueError for a connection built by `new_connection_with_operations` without it.
        """
        if self._execute_non_idempotent is None:
            raise ValueError("MySQL connection was built without the executeNonIdempotent operation")
        return self._execute_non_idempotent

    async def aclose(self) -> None:
        """Close the provider client when this connection owns one."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep credentials and provider state out of logs."""
        return "mysql.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Reject serialization into durable Flow state."""
        raise TypeError("MySQL connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a provider client to a logical connection reference."""
    if client is None:
        raise ValueError("MySQL client is required")
    return Connection(reference, client.query(), client.execute(), client.execute_non_idempotent(), client)


def new_connection_with_operations(
    query: Query[QueryRowsInput, RowSet],
    execute: Mutation[ExecuteStatementInput, StatementExecution],
    reference: ConnectionRef,
    *,
    execute_non_idempotent: Mutation[ExecuteStatementInput, StatementExecution] | None = None,
) -> Connection:
    """Bind test or mock operations to a MySQL connection name.

    `execute_non_idempotent` is optional so earlier callers keep working; without it, a
    `new_execute_non_idempotent_step` built on this connection raises ValueError.
    """
    return Connection(reference, query, execute, execute_non_idempotent)


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict) or set(material) - {"password"}:
            raise ValueError("MySQL stored credential does not match the connector credential fields")
        password = material.get("password")
        if password is not None and not isinstance(password, str):
            raise ValueError("MySQL credential password has an invalid type")
        if not password:
            raise ValueError("MySQL credential password is required")
        return Credentials(SecretString(password))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open a declared project connection with credentials resolved per call."""
    if project is None:
        raise ValueError("MySQL connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("MySQL project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("MySQL project connection provider differs")
    config = _decode_config(matching[0].configuration)
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)


_REQUIRED_STRING_FIELDS = ("host", "database", "user")
_INTEGER_FIELDS = (("port", "port"), ("maxRows", "max_rows"), ("maxResponseBytes", "max_response_bytes"))
_DURATION_FIELDS = (
    ("connectTimeout", "connect_timeout_seconds"),
    ("statementTimeout", "statement_timeout_seconds"),
)
_SSL_MODES = ("required", "verify-ca", "verify-identity", "disabled")


def _decode_config(value: dict[str, Any]) -> Config:
    known = set(_REQUIRED_STRING_FIELDS) | {name for name, _ in _INTEGER_FIELDS}
    known |= {name for name, _ in _DURATION_FIELDS} | {"sslMode"}
    if set(value) - known:
        raise ValueError("MySQL configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    for name in _REQUIRED_STRING_FIELDS:
        if name not in value:
            raise ValueError(f"MySQL configuration {name} is required")
        if not isinstance(value[name], str):
            raise ValueError(f"MySQL configuration {name} has an invalid type")
        config_arguments[name] = value[name]
    for name, argument in _INTEGER_FIELDS:
        if name in value:
            if type(value[name]) is not int:
                raise ValueError(f"MySQL configuration {name} has an invalid type")
            config_arguments[argument] = value[name]
    for name, argument in _DURATION_FIELDS:
        if name in value:
            if type(value[name]) is not int:
                raise ValueError(f"MySQL configuration {name} has an invalid type")
            config_arguments[argument] = value[name] / 1_000_000_000
    if "sslMode" in value:
        if not isinstance(value["sslMode"], str) or value["sslMode"] not in _SSL_MODES:
            raise ValueError("MySQL configuration sslMode has an invalid type")
        config_arguments["ssl_mode"] = value["sslMode"]
    return Config(**config_arguments)
