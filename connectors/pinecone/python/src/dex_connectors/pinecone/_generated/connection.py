# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, QUERY_VECTORS_DEFINITION, UPSERT_VECTORS_DEFINITION
from ..models import QueryVectorsInput, QueryVectorsOutput, UpsertVectorsInput, UpsertVectorsOutput

class Connection:

    __slots__ = ('_upsert_vectors', '_query_vectors', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        upsert_vectors: Mutation[UpsertVectorsInput, UpsertVectorsOutput],
        query_vectors: Query[QueryVectorsInput, QueryVectorsOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or upsert_vectors is None or query_vectors is None:
            raise ValueError("Pinecone connection provider and operations are required")
        if upsert_vectors.definition().operation != UPSERT_VECTORS_DEFINITION.operation:
            raise ValueError("Pinecone connection operation identity differs")
        if query_vectors.definition().operation != QUERY_VECTORS_DEFINITION.operation:
            raise ValueError("Pinecone connection operation identity differs")
        self.reference = reference
        self._upsert_vectors = upsert_vectors
        self._query_vectors = query_vectors
        self._client = client

    def upsert_vectors(self) -> Mutation[UpsertVectorsInput, UpsertVectorsOutput]:
        return self._upsert_vectors

    def query_vectors(self) -> Query[QueryVectorsInput, QueryVectorsOutput]:
        return self._query_vectors

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "pinecone.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Pinecone connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.pinecone.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Pinecone client is required")
    return Connection(reference, client.upsert_vectors(), client.query_vectors(), client)

setattr(new_connection, '__module__', 'dex_connectors.pinecone.connection')


def new_connection_with_operations(
    upsert_vectors: Mutation[UpsertVectorsInput, UpsertVectorsOutput],
    query_vectors: Query[QueryVectorsInput, QueryVectorsOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, upsert_vectors, query_vectors)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.pinecone.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        api_key = material.get("api_key")
        if not isinstance(api_key, str) or not api_key:
            raise ValueError("Pinecone credential api_key is required")
        return Credentials(SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Pinecone connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Pinecone project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Pinecone project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.pinecone.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Pinecone configuration has unknown fields")
    base_url = value.get("baseUrl", '')
    if not isinstance(base_url, str):
        raise ValueError("Pinecone configuration baseUrl has an invalid type")
    timeout = value.get("timeout", 0)
    if not type(timeout) is int:
        raise ValueError("Pinecone configuration timeout has an invalid type")
    timeout_seconds = timeout / 1_000_000_000
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not type(max_response_bytes) is int:
        raise ValueError("Pinecone configuration maxResponseBytes has an invalid type")
    return Config(base_url=base_url, timeout_seconds=timeout_seconds, max_response_bytes=max_response_bytes)
