# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, DELETE_DOCUMENT_DEFINITION, INDEX_DOCUMENT_DEFINITION, SEARCH_DOCUMENTS_DEFINITION
from ..models import DeleteDocumentInput, DeletedDocument, IndexDocumentInput, IndexedDocument, SearchDocumentsInput, SearchPage

class Connection:

    __slots__ = ('_index_document', '_delete_document', '_search_documents', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        index_document: Mutation[IndexDocumentInput, IndexedDocument],
        delete_document: Mutation[DeleteDocumentInput, DeletedDocument],
        search_documents: Query[SearchDocumentsInput, SearchPage],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or index_document is None or delete_document is None or search_documents is None:
            raise ValueError("OpenSearch connection provider and operations are required")
        if index_document.definition().operation != INDEX_DOCUMENT_DEFINITION.operation:
            raise ValueError("OpenSearch connection operation identity differs")
        if delete_document.definition().operation != DELETE_DOCUMENT_DEFINITION.operation:
            raise ValueError("OpenSearch connection operation identity differs")
        if search_documents.definition().operation != SEARCH_DOCUMENTS_DEFINITION.operation:
            raise ValueError("OpenSearch connection operation identity differs")
        self.reference = reference
        self._index_document = index_document
        self._delete_document = delete_document
        self._search_documents = search_documents
        self._client = client

    def index_document(self) -> Mutation[IndexDocumentInput, IndexedDocument]:
        return self._index_document

    def delete_document(self) -> Mutation[DeleteDocumentInput, DeletedDocument]:
        return self._delete_document

    def search_documents(self) -> Query[SearchDocumentsInput, SearchPage]:
        return self._search_documents

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "opensearch.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("OpenSearch connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.opensearch.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("OpenSearch client is required")
    return Connection(reference, client.index_document(), client.delete_document(), client.search_documents(), client)

setattr(new_connection, '__module__', 'dex_connectors.opensearch.connection')


def new_connection_with_operations(
    index_document: Mutation[IndexDocumentInput, IndexedDocument],
    delete_document: Mutation[DeleteDocumentInput, DeletedDocument],
    search_documents: Query[SearchDocumentsInput, SearchPage],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, index_document, delete_document, search_documents)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.opensearch.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("OpenSearch stored credential does not match the connector credential fields")
        if set(material) - set(('username', 'password')):
            raise ValueError("OpenSearch stored credential does not match the connector credential fields")
        username = material.get("username")
        if username is not None and not (isinstance(username, str)):
            raise ValueError("OpenSearch credential username has an invalid type")
        password = material.get("password")
        if password is not None and not (isinstance(password, str)):
            raise ValueError("OpenSearch credential password has an invalid type")
        if not username:
            raise ValueError("OpenSearch credential username is required")
        if not password:
            raise ValueError("OpenSearch credential password is required")
        return Credentials(username, SecretString(password))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("OpenSearch connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("OpenSearch project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("OpenSearch project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.opensearch.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"clusterUrl", "timeout", "maxResponseBytes", "maxDocumentBytes"}:
        raise ValueError("OpenSearch configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "clusterUrl" in value:
        cluster_url = value["clusterUrl"]
        if not (isinstance(cluster_url, str)):
            raise ValueError("OpenSearch configuration clusterUrl has an invalid type")
        config_arguments['cluster_url'] = cluster_url
    else:
        raise ValueError("OpenSearch configuration clusterUrl is required")
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("OpenSearch configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("OpenSearch configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "maxDocumentBytes" in value:
        max_document_bytes = value["maxDocumentBytes"]
        if not (type(max_document_bytes) is int):
            raise ValueError("OpenSearch configuration maxDocumentBytes has an invalid type")
        config_arguments['max_document_bytes'] = max_document_bytes
    return Config(**config_arguments)
