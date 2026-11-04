# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, SELECT_ROWS_DEFINITION, UPSERT_ROWS_DEFINITION
from ..models import SelectRowsInput, SelectRowsOutput, UpsertRowsInput, UpsertRowsOutput

class Connection:

    __slots__ = ('_upsert_rows', '_select_rows', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        upsert_rows: Mutation[UpsertRowsInput, UpsertRowsOutput],
        select_rows: Query[SelectRowsInput, SelectRowsOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or upsert_rows is None or select_rows is None:
            raise ValueError("Supabase connection provider and operations are required")
        if upsert_rows.definition().operation != UPSERT_ROWS_DEFINITION.operation:
            raise ValueError("Supabase connection operation identity differs")
        if select_rows.definition().operation != SELECT_ROWS_DEFINITION.operation:
            raise ValueError("Supabase connection operation identity differs")
        self.reference = reference
        self._upsert_rows = upsert_rows
        self._select_rows = select_rows
        self._client = client

    def upsert_rows(self) -> Mutation[UpsertRowsInput, UpsertRowsOutput]:
        return self._upsert_rows

    def select_rows(self) -> Query[SelectRowsInput, SelectRowsOutput]:
        return self._select_rows

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "supabase.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Supabase connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.supabase.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Supabase client is required")
    return Connection(reference, client.upsert_rows(), client.select_rows(), client)

setattr(new_connection, '__module__', 'dex_connectors.supabase.connection')


def new_connection_with_operations(
    upsert_rows: Mutation[UpsertRowsInput, UpsertRowsOutput],
    select_rows: Query[SelectRowsInput, SelectRowsOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, upsert_rows, select_rows)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.supabase.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        api_key = material.get("api_key")
        if not isinstance(api_key, str) or not api_key:
            raise ValueError("Supabase credential api_key is required")
        return Credentials(SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
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

setattr(new_project_connection, '__module__', 'dex_connectors.supabase.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Supabase configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Supabase configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    else:
        raise ValueError("Supabase configuration baseUrl is required")
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Supabase configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Supabase configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
