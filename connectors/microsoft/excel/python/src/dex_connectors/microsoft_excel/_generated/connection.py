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
from .definitions import CONNECTOR_ID, PROVIDER, APPEND_TABLE_ROWS_DEFINITION, GET_TABLE_ROWS_DEFINITION, GET_VALUES_DEFINITION, UPDATE_VALUES_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import AppendTableRowsInput, AppendTableRowsOutput, GetTableRowsInput, GetTableRowsOutput, GetValuesInput, GetValuesOutput, UpdateValuesInput, UpdateValuesOutput

class Connection:

    __slots__ = ('_get_table_rows', '_get_values', '_update_values', '_append_table_rows', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_table_rows: Query[GetTableRowsInput, GetTableRowsOutput],
        get_values: Query[GetValuesInput, GetValuesOutput],
        update_values: Mutation[UpdateValuesInput, UpdateValuesOutput],
        append_table_rows: Mutation[AppendTableRowsInput, AppendTableRowsOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != PROVIDER or get_table_rows is None or get_values is None or update_values is None or append_table_rows is None:
            raise ValueError("Microsoft Excel connection provider and operations are required")
        if get_table_rows.definition().operation != GET_TABLE_ROWS_DEFINITION.operation:
            raise ValueError("Microsoft Excel connection operation identity differs")
        if get_values.definition().operation != GET_VALUES_DEFINITION.operation:
            raise ValueError("Microsoft Excel connection operation identity differs")
        if update_values.definition().operation != UPDATE_VALUES_DEFINITION.operation:
            raise ValueError("Microsoft Excel connection operation identity differs")
        if append_table_rows.definition().operation != APPEND_TABLE_ROWS_DEFINITION.operation:
            raise ValueError("Microsoft Excel connection operation identity differs")
        self.reference = reference
        self._get_table_rows = get_table_rows
        self._get_values = get_values
        self._update_values = update_values
        self._append_table_rows = append_table_rows
        self._client = client

    def get_table_rows(self) -> Query[GetTableRowsInput, GetTableRowsOutput]:
        return self._get_table_rows

    def get_values(self) -> Query[GetValuesInput, GetValuesOutput]:
        return self._get_values

    def update_values(self) -> Mutation[UpdateValuesInput, UpdateValuesOutput]:
        return self._update_values

    def append_table_rows(self) -> Mutation[AppendTableRowsInput, AppendTableRowsOutput]:
        return self._append_table_rows

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "microsoft_excel.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Microsoft Excel connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.microsoft_excel.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Microsoft Excel client is required")
    return Connection(reference, client.get_table_rows(), client.get_values(), client.update_values(), client.append_table_rows(), client)

setattr(new_connection, '__module__', 'dex_connectors.microsoft_excel.connection')


def new_connection_with_operations(
    get_table_rows: Query[GetTableRowsInput, GetTableRowsOutput],
    get_values: Query[GetValuesInput, GetValuesOutput],
    update_values: Mutation[UpdateValuesInput, UpdateValuesOutput],
    append_table_rows: Mutation[AppendTableRowsInput, AppendTableRowsOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_table_rows, get_values, update_values, append_table_rows)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.microsoft_excel.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("Microsoft Excel stored credential does not match the connector credential fields")
    if set(material) - set(('oauth_client_id', 'oauth_client_secret', 'access_token', 'refresh_token', 'auth_method')):
        raise ValueError("Microsoft Excel stored credential does not match the connector credential fields")
    is_shared = shared
    oauth_client_id = material.get("oauth_client_id")
    if oauth_client_id is not None and not (isinstance(oauth_client_id, str)):
        raise ValueError("Microsoft Excel credential oauth_client_id has an invalid type")
    oauth_client_secret = material.get("oauth_client_secret")
    if oauth_client_secret is not None and not (isinstance(oauth_client_secret, str)):
        raise ValueError("Microsoft Excel credential oauth_client_secret has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("Microsoft Excel credential access_token has an invalid type")
    refresh_token = material.get("refresh_token")
    if refresh_token is not None and not (isinstance(refresh_token, str)):
        raise ValueError("Microsoft Excel credential refresh_token has an invalid type")
    selected_method = material.get('auth_method')
    if selected_method is None:
        selected_method = ''
    if not isinstance(selected_method, str):
        raise ValueError("Microsoft Excel credential auth_method is invalid")
    method = selected_method or "microsoft-oauth"
    values: dict[str, Any] = {'auth_method_id': method}
    if method == "microsoft-oauth":
        if not oauth_client_id:
            raise ValueError("Microsoft Excel credential oauth_client_id is required")
        if oauth_client_id:
            values["oauth_client_id"] = oauth_client_id
        if not oauth_client_secret and not is_shared:
            raise ValueError("Microsoft Excel credential oauth_client_secret is required")
        if oauth_client_secret:
            values["oauth_client_secret"] = SecretString(oauth_client_secret)
        if not access_token:
            raise ValueError("Microsoft Excel credential access_token is required")
        if access_token:
            values["access_token"] = SecretString(access_token)
        if not refresh_token and not is_shared:
            raise ValueError("Microsoft Excel credential refresh_token is required")
        if refresh_token:
            values["refresh_token"] = SecretString(refresh_token)
    else:
        raise ValueError("Microsoft Excel credential auth_method is invalid")
    return Credentials(**values)


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    method = credentials.auth_method_id or "microsoft-oauth"
    material['auth_method'] = method
    if method == "microsoft-oauth":
        if credentials.oauth_client_id:
            material["oauth_client_id"] = credentials.oauth_client_id
        if (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else ''):
            material["oauth_client_secret"] = (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if (credentials.refresh_token.reveal() if credentials.refresh_token is not None else ''):
            material["refresh_token"] = (credentials.refresh_token.reveal() if credentials.refresh_token is not None else '')
    else:
        raise ValueError("Microsoft Excel credential auth_method is invalid")
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials]("Microsoft Excel", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

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
        raise ValueError("Microsoft Excel connection requires loaded project configuration")
    reference = ConnectionRef(PROVIDER, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Microsoft Excel project connection is missing or duplicated")
    if matching[0].provider != PROVIDER:
        raise ValueError("Microsoft Excel project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.microsoft_excel.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"maxResponseBytes", "maxCells"}:
        raise ValueError("Microsoft Excel configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Microsoft Excel configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "maxCells" in value:
        max_cells = value["maxCells"]
        if not (type(max_cells) is int):
            raise ValueError("Microsoft Excel configuration maxCells has an invalid type")
        config_arguments['max_cells'] = max_cells
    return Config(**config_arguments)
