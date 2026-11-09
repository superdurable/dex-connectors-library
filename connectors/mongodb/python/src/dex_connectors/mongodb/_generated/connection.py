# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, FIND_DOCUMENTS_DEFINITION
from ..models import DocumentSet, FindDocumentsInput

class Connection:

    __slots__ = ('_find_documents', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        find_documents: Query[FindDocumentsInput, DocumentSet],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or find_documents is None:
            raise ValueError("MongoDB connection provider and operations are required")
        if find_documents.definition().operation != FIND_DOCUMENTS_DEFINITION.operation:
            raise ValueError("MongoDB connection operation identity differs")
        self.reference = reference
        self._find_documents = find_documents
        self._client = client

    def find_documents(self) -> Query[FindDocumentsInput, DocumentSet]:
        return self._find_documents

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "mongodb.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("MongoDB connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.mongodb.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("MongoDB client is required")
    return Connection(reference, client.find_documents(), client)

setattr(new_connection, '__module__', 'dex_connectors.mongodb.connection')


def new_connection_with_operations(
    find_documents: Query[FindDocumentsInput, DocumentSet],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, find_documents)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.mongodb.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("MongoDB stored credential does not match the connector credential fields")
        if set(material) - set(('connection_uri',)):
            raise ValueError("MongoDB stored credential does not match the connector credential fields")
        connection_uri = material.get("connection_uri")
        if connection_uri is not None and not (isinstance(connection_uri, str)):
            raise ValueError("MongoDB credential connection_uri has an invalid type")
        if not connection_uri:
            raise ValueError("MongoDB credential connection_uri is required")
        return Credentials(SecretString(connection_uri))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("MongoDB connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("MongoDB project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("MongoDB project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.mongodb.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"database", "timeout", "maxDocuments", "maxResponseBytes"}:
        raise ValueError("MongoDB configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "database" in value:
        database = value["database"]
        if not (isinstance(database, str)):
            raise ValueError("MongoDB configuration database has an invalid type")
        config_arguments['database'] = database
    else:
        raise ValueError("MongoDB configuration database is required")
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("MongoDB configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxDocuments" in value:
        max_documents = value["maxDocuments"]
        if not (type(max_documents) is int):
            raise ValueError("MongoDB configuration maxDocuments has an invalid type")
        config_arguments['max_documents'] = max_documents
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("MongoDB configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
