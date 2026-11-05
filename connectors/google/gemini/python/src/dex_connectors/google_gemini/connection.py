# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Gemini connection and project credential binding."""

from typing import Any

import httpx
from dex_connectors.sdk import Call, ConnectionRef, Query, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from .client import CONNECTOR_ID, GENERATE_CONTENT_DEFINITION, PROVIDER, Client, Config, Credentials
from .models import GenerateContentRequest, GenerateContentResponse


class Connection:
    """Nonserializable Gemini operation binding for a named project connection."""

    __slots__ = ("_generate_content", "_client", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        generate_content: Query[GenerateContentRequest, GenerateContentResponse],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != PROVIDER or generate_content is None:
            raise ValueError("Gemini connection provider and operation are required")
        if generate_content.definition().operation != GENERATE_CONTENT_DEFINITION.operation:
            raise ValueError("Gemini connection operation identity differs")
        self.reference = reference
        self._generate_content = generate_content
        self._client = client

    def generate_content(self) -> Query[GenerateContentRequest, GenerateContentResponse]:
        """Return the generateContent Query used by an application Step."""
        return self._generate_content

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "gemini.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("Gemini connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("Gemini client is required")
    return Connection(reference, client.generate_content(), client)


def new_connection_with_operations(
    generate_content: Query[GenerateContentRequest, GenerateContentResponse], reference: ConnectionRef
) -> Connection:
    """Bind a mock operation to the same connection identity as the real provider."""
    return Connection(reference, generate_content)


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        value = material.get("api_key")
        if not isinstance(value, str) or not value:
            raise ValueError("Gemini credential api_key is required")
        return Credentials(SecretString(value))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared ordinary settings and resolve the current API key per call."""
    if project is None:
        raise ValueError("Gemini connection requires loaded project configuration")
    reference = ConnectionRef(PROVIDER, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Gemini project connection is missing or duplicated")
    if matching[0].provider != PROVIDER:
        raise ValueError("Gemini project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"model", "endpoint", "maxResponseBytes"}:
        raise ValueError("Gemini configuration has unknown fields")
    model = value.get("model", "")
    endpoint = value.get("endpoint", "")
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not isinstance(model, str) or not isinstance(endpoint, str):
        raise ValueError("Gemini configuration model and endpoint must be strings")
    if type(max_response_bytes) is not int:
        raise ValueError("Gemini configuration maxResponseBytes must be an integer")
    return Config(model, endpoint, max_response_bytes)
