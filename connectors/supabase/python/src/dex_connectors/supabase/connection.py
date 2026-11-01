# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Supabase connection and project credential binding."""

from typing import Any

import httpx
from dex_connectors.sdk import Call, ConnectionRef, Mutation, Query, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from .client import (
    CONNECTOR_ID,
    SELECT_ROWS_DEFINITION,
    UPSERT_ROWS_DEFINITION,
    Client,
    Config,
    Credentials,
)
from .models import SelectRowsInput, SelectRowsOutput, UpsertRowsInput, UpsertRowsOutput


class Connection:
    """Nonserializable Supabase operation bindings for a named project connection."""

    __slots__ = ("_upsert_rows", "_select_rows", "_client", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        upsert_rows: Mutation[UpsertRowsInput, UpsertRowsOutput],
        select_rows: Query[SelectRowsInput, SelectRowsOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or upsert_rows is None or select_rows is None:
            raise ValueError("Supabase connection provider and operations are required")
        if (
            upsert_rows.definition().operation != UPSERT_ROWS_DEFINITION.operation
            or select_rows.definition().operation != SELECT_ROWS_DEFINITION.operation
        ):
            raise ValueError("Supabase connection operation identity differs")
        self.reference = reference
        self._upsert_rows = upsert_rows
        self._select_rows = select_rows
        self._client = client

    def upsert_rows(self) -> Mutation[UpsertRowsInput, UpsertRowsOutput]:
        """Return the row upsert Mutation used by an application Step."""
        return self._upsert_rows

    def select_rows(self) -> Query[SelectRowsInput, SelectRowsOutput]:
        """Return the row selection Query used by an application Step."""
        return self._select_rows

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "supabase.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("Supabase connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("Supabase client is required")
    return Connection(reference, client.upsert_rows(), client.select_rows(), client)


def new_connection_with_operations(
    upsert_rows: Mutation[UpsertRowsInput, UpsertRowsOutput],
    select_rows: Query[SelectRowsInput, SelectRowsOutput],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock operations to the same connection identity as the real provider."""
    return Connection(reference, upsert_rows, select_rows)


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        value = material.get("api_key")
        if not isinstance(value, str) or not value:
            raise ValueError("Supabase credential api_key is required")
        return Credentials(SecretString(value))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared ordinary settings and resolve the current API key per call."""
    if project is None:
        raise ValueError("Supabase connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Supabase project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Supabase project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Supabase configuration has unknown fields")
    base_url = value.get("baseUrl", "")
    timeout = value.get("timeout", 0)
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not isinstance(base_url, str):
        raise ValueError("Supabase configuration baseUrl must be a string")
    if any(type(number) is not int for number in (timeout, max_response_bytes)):
        raise ValueError("Supabase configuration timeout and maxResponseBytes must be integers")
    return Config(base_url, timeout / 1_000_000_000, max_response_bytes)
