# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString, CredentialRefreshDriver
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject
from dex_connectors.sdk.projectconfig.provider import RefreshingCredentialProvider
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, ADD_UPDATE_DEFINITION, CREATE_ITEM_DEFINITION, GET_ITEM_DEFINITION, LIST_ITEMS_DEFINITION, UPDATE_ITEM_COLUMN_VALUES_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import AddUpdateInput, AddUpdateOutput, CreateItemInput, CreateItemOutput, GetItemInput, Item, ListItemsInput, ListItemsOutput, UpdateItemColumnValuesInput, UpdateItemColumnValuesOutput

class Connection:

    __slots__ = ('_list_items', '_get_item', '_create_item', '_update_item_column_values', '_add_update', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_items: Query[ListItemsInput, ListItemsOutput],
        get_item: Query[GetItemInput, Item],
        create_item: Mutation[CreateItemInput, CreateItemOutput],
        update_item_column_values: Mutation[UpdateItemColumnValuesInput, UpdateItemColumnValuesOutput],
        add_update: Mutation[AddUpdateInput, AddUpdateOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_items is None or get_item is None or create_item is None or update_item_column_values is None or add_update is None:
            raise ValueError("monday.com connection provider and operations are required")
        if list_items.definition().operation != LIST_ITEMS_DEFINITION.operation:
            raise ValueError("monday.com connection operation identity differs")
        if get_item.definition().operation != GET_ITEM_DEFINITION.operation:
            raise ValueError("monday.com connection operation identity differs")
        if create_item.definition().operation != CREATE_ITEM_DEFINITION.operation:
            raise ValueError("monday.com connection operation identity differs")
        if update_item_column_values.definition().operation != UPDATE_ITEM_COLUMN_VALUES_DEFINITION.operation:
            raise ValueError("monday.com connection operation identity differs")
        if add_update.definition().operation != ADD_UPDATE_DEFINITION.operation:
            raise ValueError("monday.com connection operation identity differs")
        self.reference = reference
        self._list_items = list_items
        self._get_item = get_item
        self._create_item = create_item
        self._update_item_column_values = update_item_column_values
        self._add_update = add_update
        self._client = client

    def list_items(self) -> Query[ListItemsInput, ListItemsOutput]:
        return self._list_items

    def get_item(self) -> Query[GetItemInput, Item]:
        return self._get_item

    def create_item(self) -> Mutation[CreateItemInput, CreateItemOutput]:
        return self._create_item

    def update_item_column_values(self) -> Mutation[UpdateItemColumnValuesInput, UpdateItemColumnValuesOutput]:
        return self._update_item_column_values

    def add_update(self) -> Mutation[AddUpdateInput, AddUpdateOutput]:
        return self._add_update

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "monday.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("monday.com connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.monday.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("monday.com client is required")
    return Connection(reference, client.list_items(), client.get_item(), client.create_item(), client.update_item_column_values(), client.add_update(), client)

setattr(new_connection, '__module__', 'dex_connectors.monday.connection')


def new_connection_with_operations(
    list_items: Query[ListItemsInput, ListItemsOutput],
    get_item: Query[GetItemInput, Item],
    create_item: Mutation[CreateItemInput, CreateItemOutput],
    update_item_column_values: Mutation[UpdateItemColumnValuesInput, UpdateItemColumnValuesOutput],
    add_update: Mutation[AddUpdateInput, AddUpdateOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_items, get_item, create_item, update_item_column_values, add_update)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.monday.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("monday.com stored credential does not match the connector credential fields")
    if set(material) - set(('api_token', 'oauth_client_id', 'oauth_client_secret', 'access_token', 'refresh_token', 'auth_method')):
        raise ValueError("monday.com stored credential does not match the connector credential fields")
    is_shared = shared
    api_token = material.get("api_token")
    if api_token is not None and not (isinstance(api_token, str)):
        raise ValueError("monday.com credential api_token has an invalid type")
    oauth_client_id = material.get("oauth_client_id")
    if oauth_client_id is not None and not (isinstance(oauth_client_id, str)):
        raise ValueError("monday.com credential oauth_client_id has an invalid type")
    oauth_client_secret = material.get("oauth_client_secret")
    if oauth_client_secret is not None and not (isinstance(oauth_client_secret, str)):
        raise ValueError("monday.com credential oauth_client_secret has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("monday.com credential access_token has an invalid type")
    refresh_token = material.get("refresh_token")
    if refresh_token is not None and not (isinstance(refresh_token, str)):
        raise ValueError("monday.com credential refresh_token has an invalid type")
    selected_method = material.get('auth_method')
    if selected_method is None:
        selected_method = ''
    if not isinstance(selected_method, str):
        raise ValueError("monday.com credential auth_method is invalid")
    method = selected_method or "personal-api-token"
    values: dict[str, Any] = {'auth_method_id': method}
    if method == "personal-api-token":
        if not api_token:
            raise ValueError("monday.com credential api_token is required")
        if api_token:
            values["api_token"] = SecretString(api_token)
    elif method == "monday-oauth":
        if not oauth_client_id:
            raise ValueError("monday.com credential oauth_client_id is required")
        if oauth_client_id:
            values["oauth_client_id"] = oauth_client_id
        if not oauth_client_secret and not is_shared:
            raise ValueError("monday.com credential oauth_client_secret is required")
        if oauth_client_secret:
            values["oauth_client_secret"] = SecretString(oauth_client_secret)
        if not access_token:
            raise ValueError("monday.com credential access_token is required")
        if access_token:
            values["access_token"] = SecretString(access_token)
        if not refresh_token and not is_shared:
            raise ValueError("monday.com credential refresh_token is required")
        if refresh_token:
            values["refresh_token"] = SecretString(refresh_token)
    else:
        raise ValueError("monday.com credential auth_method is invalid")
    return Credentials(**values)


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    method = credentials.auth_method_id or "personal-api-token"
    material['auth_method'] = method
    if method == "personal-api-token":
        if (credentials.api_token.reveal() if credentials.api_token is not None else ''):
            material["api_token"] = (credentials.api_token.reveal() if credentials.api_token is not None else '')
    elif method == "monday-oauth":
        if credentials.oauth_client_id:
            material["oauth_client_id"] = credentials.oauth_client_id
        if (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else ''):
            material["oauth_client_secret"] = (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if (credentials.refresh_token.reveal() if credentials.refresh_token is not None else ''):
            material["refresh_token"] = (credentials.refresh_token.reveal() if credentials.refresh_token is not None else '')
    else:
        raise ValueError("monday.com credential auth_method is invalid")
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials]("monday.com", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

    async def resolve(self, call: Call) -> Credentials:
        return await self.provider.resolve_with_refresh(call, self.driver)

    async def resolve_with_refresh(self, call: Call, driver: CredentialRefreshDriver[Credentials] | None = None) -> Credentials:
        return await self.provider.resolve_with_refresh(call, driver or self.driver)

    async def resolve_after_rejection(self, call: Call, driver: CredentialRefreshDriver[Credentials] | None = None) -> Credentials:
        return await self.provider.resolve_after_rejection(call, driver or self.driver)


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("monday.com connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("monday.com project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("monday.com project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.monday.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"maxResponseBytes"}:
        raise ValueError("monday.com configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("monday.com configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
