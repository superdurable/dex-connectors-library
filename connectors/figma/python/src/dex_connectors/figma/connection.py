# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Figma connection and project credential binding."""

from typing import Any

import httpx
from dex_connectors.sdk import Call, ConnectionRef, Query, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from .client import (
    CONNECTOR_ID,
    GET_FILE_COMMENTS_DEFINITION,
    GET_FILE_DEFINITION,
    Client,
    Config,
    Credentials,
)
from .models import File, FileComments, GetFileCommentsInput, GetFileInput


class Connection:
    """Nonserializable Figma operation bindings for a named project connection."""

    __slots__ = ("_get_file", "_get_file_comments", "_client", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        get_file: Query[GetFileInput, File],
        get_file_comments: Query[GetFileCommentsInput, FileComments],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_file is None or get_file_comments is None:
            raise ValueError("Figma connection provider and operations are required")
        if (
            get_file.definition().operation != GET_FILE_DEFINITION.operation
            or get_file_comments.definition().operation != GET_FILE_COMMENTS_DEFINITION.operation
        ):
            raise ValueError("Figma connection operation identity differs")
        self.reference = reference
        self._get_file = get_file
        self._get_file_comments = get_file_comments
        self._client = client

    def get_file(self) -> Query[GetFileInput, File]:
        """Return the file Query used by an application Step."""
        return self._get_file

    def get_file_comments(self) -> Query[GetFileCommentsInput, FileComments]:
        """Return the file comments Query used by an application Step."""
        return self._get_file_comments

    async def aclose(self) -> None:
        """Figma the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "figma.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("Figma connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("Figma client is required")
    return Connection(reference, client.get_file(), client.get_file_comments(), client)


def new_connection_with_operations(
    get_file: Query[GetFileInput, File],
    get_file_comments: Query[GetFileCommentsInput, FileComments],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock operations to the same connection identity as the real provider."""
    return Connection(reference, get_file, get_file_comments)


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        value = material.get("api_key")
        if not isinstance(value, str) or not value:
            raise ValueError("Figma credential api_key is required")
        return Credentials(SecretString(value))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared ordinary settings and resolve the current private key per call."""
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


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Figma configuration has unknown fields")
    base_url = value.get("baseUrl", "")
    timeout = value.get("timeout", 0)
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not isinstance(base_url, str):
        raise ValueError("Figma configuration baseUrl must be a string")
    if type(timeout) is not int or type(max_response_bytes) is not int:
        raise ValueError("Figma configuration timeout and maxResponseBytes must be integers")
    return Config(base_url, timeout / 1_000_000_000, max_response_bytes)
