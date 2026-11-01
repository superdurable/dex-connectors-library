# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""OpenAI connection and project credential binding."""

from typing import Any

import httpx
from dex_connectors.sdk import Call, ConnectionRef, Mutation, Query, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from .client import (
    CONNECTOR_ID,
    CREATE_RESPONSE_DEFINITION,
    RETRIEVE_RESPONSE_DEFINITION,
    Client,
    Config,
    Credentials,
)
from .models import CreateRequest, Response, RetrieveRequest


class Connection:
    """Nonserializable OpenAI operation bindings for a named project connection."""

    __slots__ = ("_create_response", "_retrieve_response", "_client", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        create_response: Mutation[CreateRequest, Response],
        retrieve_response: Query[RetrieveRequest, Response],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or create_response is None or retrieve_response is None:
            raise ValueError("OpenAI connection provider and operations are required")
        if (
            create_response.definition().operation != CREATE_RESPONSE_DEFINITION.operation
            or retrieve_response.definition().operation != RETRIEVE_RESPONSE_DEFINITION.operation
        ):
            raise ValueError("OpenAI connection operation identity differs")
        self.reference = reference
        self._create_response = create_response
        self._retrieve_response = retrieve_response
        self._client = client

    def create_response(self) -> Mutation[CreateRequest, Response]:
        """Return the fenced create Mutation used by an application Step."""
        return self._create_response

    def retrieve_response(self) -> Query[RetrieveRequest, Response]:
        """Return the stored response Query used by an application Step."""
        return self._retrieve_response

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "openai.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("OpenAI connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("OpenAI client is required")
    return Connection(reference, client.create_response(), client.retrieve_response(), client)


def new_connection_with_operations(
    create_response: Mutation[CreateRequest, Response],
    retrieve_response: Query[RetrieveRequest, Response],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock operations to the same connection identity as the real provider."""
    return Connection(reference, create_response, retrieve_response)


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        value = material.get("api_key")
        if not isinstance(value, str) or not value:
            raise ValueError("OpenAI credential api_key is required")
        return Credentials(SecretString(value))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared ordinary settings and resolve the current private key per call."""
    if project is None:
        raise ValueError("OpenAI connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("OpenAI project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("OpenAI project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"model", "endpoint", "maxResponseBytes", "maxSseEventBytes"}:
        raise ValueError("OpenAI configuration has unknown fields")
    model = value.get("model", "")
    endpoint = value.get("endpoint", "")
    max_response_bytes = value.get("maxResponseBytes", 0)
    max_sse_event_bytes = value.get("maxSseEventBytes", 0)
    if not isinstance(model, str) or not isinstance(endpoint, str):
        raise ValueError("OpenAI configuration model and endpoint must be strings")
    if type(max_response_bytes) is not int or type(max_sse_event_bytes) is not int:
        raise ValueError("OpenAI configuration byte limits must be integers")
    return Config(model, endpoint, max_response_bytes, max_sse_event_bytes)
