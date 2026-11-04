# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, SAVE_OBJECT_DEFINITION, SEARCH_DEFINITION
from ..models import SaveObjectInput, SavedObject, SearchInput, SearchPage

class Connection:

    __slots__ = ('_save_object', '_search', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        save_object: Mutation[SaveObjectInput, SavedObject],
        search: Query[SearchInput, SearchPage],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or save_object is None or search is None:
            raise ValueError("Algolia connection provider and operations are required")
        if save_object.definition().operation != SAVE_OBJECT_DEFINITION.operation:
            raise ValueError("Algolia connection operation identity differs")
        if search.definition().operation != SEARCH_DEFINITION.operation:
            raise ValueError("Algolia connection operation identity differs")
        self.reference = reference
        self._save_object = save_object
        self._search = search
        self._client = client

    def save_object(self) -> Mutation[SaveObjectInput, SavedObject]:
        return self._save_object

    def search(self) -> Query[SearchInput, SearchPage]:
        return self._search

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "algolia.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Algolia connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.algolia.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Algolia client is required")
    return Connection(reference, client.save_object(), client.search(), client)

setattr(new_connection, '__module__', 'dex_connectors.algolia.connection')


def new_connection_with_operations(
    save_object: Mutation[SaveObjectInput, SavedObject],
    search: Query[SearchInput, SearchPage],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, save_object, search)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.algolia.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        application_id = material.get("application_id")
        if not isinstance(application_id, str) or not application_id:
            raise ValueError("Algolia credential application_id is required")
        api_key = material.get("api_key")
        if not isinstance(api_key, str) or not api_key:
            raise ValueError("Algolia credential api_key is required")
        return Credentials(application_id, SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Algolia connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Algolia project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Algolia project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.algolia.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Algolia configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Algolia configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Algolia configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Algolia configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
