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
from .definitions import CONNECTOR_ID, PROVIDER, GET_FILE_DEFINITION, READ_FILE_TEXT_DEFINITION, SEARCH_FILES_DEFINITION, UPLOAD_FILE_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import File, GetFileInput, ReadFileTextInput, ReadFileTextOutput, SearchFilesInput, SearchFilesOutput, UploadFileInput, UploadFileOutput

class Connection:

    __slots__ = ('_search_files', '_get_file', '_read_file_text', '_upload_file', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        search_files: Query[SearchFilesInput, SearchFilesOutput],
        get_file: Query[GetFileInput, File],
        read_file_text: Query[ReadFileTextInput, ReadFileTextOutput],
        upload_file: Mutation[UploadFileInput, UploadFileOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != PROVIDER or search_files is None or get_file is None or read_file_text is None or upload_file is None:
            raise ValueError("Google Drive connection provider and operations are required")
        if search_files.definition().operation != SEARCH_FILES_DEFINITION.operation:
            raise ValueError("Google Drive connection operation identity differs")
        if get_file.definition().operation != GET_FILE_DEFINITION.operation:
            raise ValueError("Google Drive connection operation identity differs")
        if read_file_text.definition().operation != READ_FILE_TEXT_DEFINITION.operation:
            raise ValueError("Google Drive connection operation identity differs")
        if upload_file.definition().operation != UPLOAD_FILE_DEFINITION.operation:
            raise ValueError("Google Drive connection operation identity differs")
        self.reference = reference
        self._search_files = search_files
        self._get_file = get_file
        self._read_file_text = read_file_text
        self._upload_file = upload_file
        self._client = client

    def search_files(self) -> Query[SearchFilesInput, SearchFilesOutput]:
        return self._search_files

    def get_file(self) -> Query[GetFileInput, File]:
        return self._get_file

    def read_file_text(self) -> Query[ReadFileTextInput, ReadFileTextOutput]:
        return self._read_file_text

    def upload_file(self) -> Mutation[UploadFileInput, UploadFileOutput]:
        return self._upload_file

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "google_drive.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Google Drive connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.google_drive.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Google Drive client is required")
    return Connection(reference, client.search_files(), client.get_file(), client.read_file_text(), client.upload_file(), client)

setattr(new_connection, '__module__', 'dex_connectors.google_drive.connection')


def new_connection_with_operations(
    search_files: Query[SearchFilesInput, SearchFilesOutput],
    get_file: Query[GetFileInput, File],
    read_file_text: Query[ReadFileTextInput, ReadFileTextOutput],
    upload_file: Mutation[UploadFileInput, UploadFileOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, search_files, get_file, read_file_text, upload_file)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.google_drive.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("Google Drive stored credential does not match the connector credential fields")
    if set(material) - set(('oauth_client_id', 'oauth_client_secret', 'access_token', 'refresh_token', 'service_account_key', 'delegated_user', 'auth_method')):
        raise ValueError("Google Drive stored credential does not match the connector credential fields")
    is_shared = shared
    oauth_client_id = material.get("oauth_client_id")
    if oauth_client_id is not None and not (isinstance(oauth_client_id, str)):
        raise ValueError("Google Drive credential oauth_client_id has an invalid type")
    oauth_client_secret = material.get("oauth_client_secret")
    if oauth_client_secret is not None and not (isinstance(oauth_client_secret, str)):
        raise ValueError("Google Drive credential oauth_client_secret has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("Google Drive credential access_token has an invalid type")
    refresh_token = material.get("refresh_token")
    if refresh_token is not None and not (isinstance(refresh_token, str)):
        raise ValueError("Google Drive credential refresh_token has an invalid type")
    service_account_key = material.get("service_account_key")
    if service_account_key is not None and not (isinstance(service_account_key, str)):
        raise ValueError("Google Drive credential service_account_key has an invalid type")
    delegated_user = material.get("delegated_user")
    if delegated_user is not None and not (isinstance(delegated_user, str)):
        raise ValueError("Google Drive credential delegated_user has an invalid type")
    selected_method = material.get('auth_method')
    if selected_method is None:
        selected_method = ''
    if not isinstance(selected_method, str):
        raise ValueError("Google Drive credential auth_method is invalid")
    method = selected_method or "google-oauth"
    values: dict[str, Any] = {'auth_method_id': method}
    if method == "google-oauth":
        if not oauth_client_id:
            raise ValueError("Google Drive credential oauth_client_id is required")
        if oauth_client_id:
            values["oauth_client_id"] = oauth_client_id
        if not oauth_client_secret and not is_shared:
            raise ValueError("Google Drive credential oauth_client_secret is required")
        if oauth_client_secret:
            values["oauth_client_secret"] = SecretString(oauth_client_secret)
        if not access_token:
            raise ValueError("Google Drive credential access_token is required")
        if access_token:
            values["access_token"] = SecretString(access_token)
        if not refresh_token and not is_shared:
            raise ValueError("Google Drive credential refresh_token is required")
        if refresh_token:
            values["refresh_token"] = SecretString(refresh_token)
    elif method == "workspace-domain-delegation":
        if not service_account_key and not is_shared:
            raise ValueError("Google Drive credential service_account_key is required")
        if service_account_key:
            values["service_account_key"] = SecretString(service_account_key)
        if not delegated_user:
            raise ValueError("Google Drive credential delegated_user is required")
        if delegated_user:
            values["delegated_user"] = delegated_user
        if not access_token and is_shared:
            raise ValueError("Google Drive credential access_token is required")
        values["access_token"] = SecretString(access_token or '')
    else:
        raise ValueError("Google Drive credential auth_method is invalid")
    return Credentials(**values)


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    method = credentials.auth_method_id or "google-oauth"
    material['auth_method'] = method
    if method == "google-oauth":
        if credentials.oauth_client_id:
            material["oauth_client_id"] = credentials.oauth_client_id
        if (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else ''):
            material["oauth_client_secret"] = (credentials.oauth_client_secret.reveal() if credentials.oauth_client_secret is not None else '')
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
        if (credentials.refresh_token.reveal() if credentials.refresh_token is not None else ''):
            material["refresh_token"] = (credentials.refresh_token.reveal() if credentials.refresh_token is not None else '')
    elif method == "workspace-domain-delegation":
        if (credentials.service_account_key.reveal() if credentials.service_account_key is not None else ''):
            material["service_account_key"] = (credentials.service_account_key.reveal() if credentials.service_account_key is not None else '')
        if credentials.delegated_user:
            material["delegated_user"] = credentials.delegated_user
        if (credentials.access_token.reveal() if credentials.access_token is not None else ''):
            material["access_token"] = (credentials.access_token.reveal() if credentials.access_token is not None else '')
    else:
        raise ValueError("Google Drive credential auth_method is invalid")
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials]("Google Drive", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

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
        raise ValueError("Google Drive connection requires loaded project configuration")
    reference = ConnectionRef(PROVIDER, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Google Drive project connection is missing or duplicated")
    if matching[0].provider != PROVIDER:
        raise ValueError("Google Drive project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.google_drive.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"endpoint", "maxResponseBytes", "maxTextBytes", "maxUploadBytes", "searchPageSize"}:
        raise ValueError("Google Drive configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "endpoint" in value:
        endpoint = value["endpoint"]
        if not (isinstance(endpoint, str)):
            raise ValueError("Google Drive configuration endpoint has an invalid type")
        config_arguments['endpoint'] = endpoint
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Google Drive configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "maxTextBytes" in value:
        max_text_bytes = value["maxTextBytes"]
        if not (type(max_text_bytes) is int):
            raise ValueError("Google Drive configuration maxTextBytes has an invalid type")
        config_arguments['max_text_bytes'] = max_text_bytes
    if "maxUploadBytes" in value:
        max_upload_bytes = value["maxUploadBytes"]
        if not (type(max_upload_bytes) is int):
            raise ValueError("Google Drive configuration maxUploadBytes has an invalid type")
        config_arguments['max_upload_bytes'] = max_upload_bytes
    if "searchPageSize" in value:
        search_page_size = value["searchPageSize"]
        if not (type(search_page_size) is int):
            raise ValueError("Google Drive configuration searchPageSize has an invalid type")
        config_arguments['search_page_size'] = search_page_size
    return Config(**config_arguments)
