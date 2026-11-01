# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Coda connection and project credential binding."""

from typing import Any

import httpx
from dex_connectors.sdk import Call, ConnectionRef, Query, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from .client import CONNECTOR_ID, GET_ROW_DEFINITION, LIST_ROWS_DEFINITION, Client, Config, Credentials
from .models import GetRowInput, GetRowOutput, ListRowsInput, ListRowsOutput


class Connection:
    """Nonserializable Coda operation bindings for a named project connection."""

    __slots__ = ("_list_rows", "_get_row", "_client", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        list_rows: Query[ListRowsInput, ListRowsOutput],
        get_row: Query[GetRowInput, GetRowOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_rows is None or get_row is None:
            raise ValueError("Coda connection provider and operations are required")
        if (
            list_rows.definition().operation != LIST_ROWS_DEFINITION.operation
            or get_row.definition().operation != GET_ROW_DEFINITION.operation
        ):
            raise ValueError("Coda connection operation identity differs")
        self.reference = reference
        self._list_rows = list_rows
        self._get_row = get_row
        self._client = client

    def list_rows(self) -> Query[ListRowsInput, ListRowsOutput]:
        """Return the listRows Query used by an application Step."""
        return self._list_rows

    def get_row(self) -> Query[GetRowInput, GetRowOutput]:
        """Return the getRow Query used by an application Step."""
        return self._get_row

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "coda.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("Coda connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("Coda client is required")
    return Connection(reference, client.list_rows(), client.get_row(), client)


def new_connection_with_operations(
    list_rows: Query[ListRowsInput, ListRowsOutput],
    get_row: Query[GetRowInput, GetRowOutput],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock operations to the same connection identity as the real provider."""
    return Connection(reference, list_rows, get_row)


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        value = material.get("api_token")
        if not isinstance(value, str) or not value:
            raise ValueError("Coda credential api_token is required")
        return Credentials(SecretString(value))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared ordinary settings and resolve the current token per call."""
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
    client = Client(
        _decode_config(matching[0].configuration), _ProjectCredentialSource(project, key), http_client
    )
    return new_connection(client, reference)


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Coda configuration has unknown fields")
    base_url = value.get("baseUrl", "")
    timeout = value.get("timeout", 0)
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not isinstance(base_url, str):
        raise ValueError("Coda configuration baseUrl must be a string")
    if type(timeout) is not int or type(max_response_bytes) is not int:
        raise ValueError("Coda configuration timeout and maxResponseBytes must be integers")
    return Config(base_url, timeout / 1_000_000_000, max_response_bytes)
