# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_FILE_COMMENTS_DEFINITION, GET_FILE_DEFINITION
from ..models import File, FileComments, GetFileCommentsInput, GetFileInput

class Connection:

    __slots__ = ('_get_file', '_get_file_comments', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_file: Query[GetFileInput, File],
        get_file_comments: Query[GetFileCommentsInput, FileComments],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_file is None or get_file_comments is None:
            raise ValueError("Figma connection provider and operations are required")
        if get_file.definition().operation != GET_FILE_DEFINITION.operation:
            raise ValueError("Figma connection operation identity differs")
        if get_file_comments.definition().operation != GET_FILE_COMMENTS_DEFINITION.operation:
            raise ValueError("Figma connection operation identity differs")
        self.reference = reference
        self._get_file = get_file
        self._get_file_comments = get_file_comments
        self._client = client

    def get_file(self) -> Query[GetFileInput, File]:
        return self._get_file

    def get_file_comments(self) -> Query[GetFileCommentsInput, FileComments]:
        return self._get_file_comments

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "figma.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Figma connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.figma.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Figma client is required")
    return Connection(reference, client.get_file(), client.get_file_comments(), client)

setattr(new_connection, '__module__', 'dex_connectors.figma.connection')


def new_connection_with_operations(
    get_file: Query[GetFileInput, File],
    get_file_comments: Query[GetFileCommentsInput, FileComments],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_file, get_file_comments)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.figma.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        api_key = material.get("api_key")
        if not isinstance(api_key, str) or not api_key:
            raise ValueError("Figma credential api_key is required")
        return Credentials(SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Figma connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Figma project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Figma project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.figma.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Figma configuration has unknown fields")
    base_url = value.get("baseUrl", '')
    if not isinstance(base_url, str):
        raise ValueError("Figma configuration baseUrl has an invalid type")
    timeout = value.get("timeout", 0)
    if not type(timeout) is int:
        raise ValueError("Figma configuration timeout has an invalid type")
    timeout_seconds = timeout / 1_000_000_000
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not type(max_response_bytes) is int:
        raise ValueError("Figma configuration maxResponseBytes has an invalid type")
    return Config(base_url=base_url, timeout_seconds=timeout_seconds, max_response_bytes=max_response_bytes)
