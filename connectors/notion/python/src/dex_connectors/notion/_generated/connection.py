# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, CREATE_PAGE_DEFINITION, GET_PAGE_DEFINITION, QUERY_DATABASE_DEFINITION, SEARCH_DEFINITION, UPDATE_PAGE_PROPERTIES_DEFINITION
from ..models import CreatePageInput, CreatePageOutput, GetPageInput, GetPageOutput, QueryDatabaseInput, QueryDatabaseOutput, SearchInput, SearchOutput, UpdatePagePropertiesInput, UpdatePagePropertiesOutput

class Connection:

    __slots__ = ('_search', '_query_database', '_get_page', '_create_page', '_update_page_properties', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        search: Query[SearchInput, SearchOutput],
        query_database: Query[QueryDatabaseInput, QueryDatabaseOutput],
        get_page: Query[GetPageInput, GetPageOutput],
        create_page: Mutation[CreatePageInput, CreatePageOutput],
        update_page_properties: Mutation[UpdatePagePropertiesInput, UpdatePagePropertiesOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or search is None or query_database is None or get_page is None or create_page is None or update_page_properties is None:
            raise ValueError("Notion connection provider and operations are required")
        if search.definition().operation != SEARCH_DEFINITION.operation:
            raise ValueError("Notion connection operation identity differs")
        if query_database.definition().operation != QUERY_DATABASE_DEFINITION.operation:
            raise ValueError("Notion connection operation identity differs")
        if get_page.definition().operation != GET_PAGE_DEFINITION.operation:
            raise ValueError("Notion connection operation identity differs")
        if create_page.definition().operation != CREATE_PAGE_DEFINITION.operation:
            raise ValueError("Notion connection operation identity differs")
        if update_page_properties.definition().operation != UPDATE_PAGE_PROPERTIES_DEFINITION.operation:
            raise ValueError("Notion connection operation identity differs")
        self.reference = reference
        self._search = search
        self._query_database = query_database
        self._get_page = get_page
        self._create_page = create_page
        self._update_page_properties = update_page_properties
        self._client = client

    def search(self) -> Query[SearchInput, SearchOutput]:
        return self._search

    def query_database(self) -> Query[QueryDatabaseInput, QueryDatabaseOutput]:
        return self._query_database

    def get_page(self) -> Query[GetPageInput, GetPageOutput]:
        return self._get_page

    def create_page(self) -> Mutation[CreatePageInput, CreatePageOutput]:
        return self._create_page

    def update_page_properties(self) -> Mutation[UpdatePagePropertiesInput, UpdatePagePropertiesOutput]:
        return self._update_page_properties

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "notion.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Notion connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.notion.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Notion client is required")
    return Connection(reference, client.search(), client.query_database(), client.get_page(), client.create_page(), client.update_page_properties(), client)

setattr(new_connection, '__module__', 'dex_connectors.notion.connection')


def new_connection_with_operations(
    search: Query[SearchInput, SearchOutput],
    query_database: Query[QueryDatabaseInput, QueryDatabaseOutput],
    get_page: Query[GetPageInput, GetPageOutput],
    create_page: Mutation[CreatePageInput, CreatePageOutput],
    update_page_properties: Mutation[UpdatePagePropertiesInput, UpdatePagePropertiesOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, search, query_database, get_page, create_page, update_page_properties)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.notion.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Notion stored credential does not match the connector credential fields")
        if set(material) - set(('api_token',)):
            raise ValueError("Notion stored credential does not match the connector credential fields")
        api_token = material.get("api_token")
        if api_token is not None and not (isinstance(api_token, str)):
            raise ValueError("Notion credential api_token has an invalid type")
        if not api_token:
            raise ValueError("Notion credential api_token is required")
        return Credentials(SecretString(api_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Notion connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Notion project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Notion project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.notion.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"endpoint", "maxResponseBytes"}:
        raise ValueError("Notion configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "endpoint" in value:
        endpoint = value["endpoint"]
        if not (isinstance(endpoint, str)):
            raise ValueError("Notion configuration endpoint has an invalid type")
        config_arguments['endpoint'] = endpoint
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Notion configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
