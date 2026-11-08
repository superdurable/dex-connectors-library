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
from .definitions import CONNECTOR_ID, DOWNLOAD_FILE_DEFINITION, UPLOAD_FILE_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import DownloadFileInput, DownloadedFile, UploadFileInput, UploadedFile

class Connection:

    __slots__ = ('_download_file', '_upload_file', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        download_file: Query[DownloadFileInput, DownloadedFile],
        upload_file: Mutation[UploadFileInput, UploadedFile],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or download_file is None or upload_file is None:
            raise ValueError("Box connection provider and operations are required")
        if download_file.definition().operation != DOWNLOAD_FILE_DEFINITION.operation:
            raise ValueError("Box connection operation identity differs")
        if upload_file.definition().operation != UPLOAD_FILE_DEFINITION.operation:
            raise ValueError("Box connection operation identity differs")
        self.reference = reference
        self._download_file = download_file
        self._upload_file = upload_file
        self._client = client

    def download_file(self) -> Query[DownloadFileInput, DownloadedFile]:
        return self._download_file

    def upload_file(self) -> Mutation[UploadFileInput, UploadedFile]:
        return self._upload_file

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "box.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Box connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.box.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Box client is required")
    return Connection(reference, client.download_file(), client.upload_file(), client)

setattr(new_connection, '__module__', 'dex_connectors.box.connection')


def new_connection_with_operations(
    download_file: Query[DownloadFileInput, DownloadedFile],
    upload_file: Mutation[UploadFileInput, UploadedFile],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, download_file, upload_file)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.box.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("Box stored credential does not match the connector credential fields")
    if set(material) - set(('oauth_client_id', 'oauth_client_secret', 'access_token', 'refresh_token')):
        raise ValueError("Box stored credential does not match the connector credential fields")
    is_shared = shared
    oauth_client_id = material.get("oauth_client_id")
    if oauth_client_id is not None and not (isinstance(oauth_client_id, str)):
        raise ValueError("Box credential oauth_client_id has an invalid type")
    oauth_client_secret = material.get("oauth_client_secret")
    if oauth_client_secret is not None and not (isinstance(oauth_client_secret, str)):
        raise ValueError("Box credential oauth_client_secret has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("Box credential access_token has an invalid type")
    refresh_token = material.get("refresh_token")
    if refresh_token is not None and not (isinstance(refresh_token, str)):
        raise ValueError("Box credential refresh_token has an invalid type")
    if not oauth_client_id:
        raise ValueError("Box credential oauth_client_id is required")
    if not oauth_client_secret and not is_shared:
        raise ValueError("Box credential oauth_client_secret is required")
    if not access_token:
        raise ValueError("Box credential access_token is required")
    if not refresh_token and not is_shared:
        raise ValueError("Box credential refresh_token is required")
    return Credentials(oauth_client_id, SecretString(oauth_client_secret or ''), SecretString(access_token), SecretString(refresh_token or ''))


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    if credentials.oauth_client_id:
        material["oauth_client_id"] = credentials.oauth_client_id
    if credentials.oauth_client_secret.reveal():
        material["oauth_client_secret"] = credentials.oauth_client_secret.reveal()
    if credentials.access_token.reveal():
        material["access_token"] = credentials.access_token.reveal()
    if credentials.refresh_token.reveal():
        material["refresh_token"] = credentials.refresh_token.reveal()
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials]("Box", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

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
        raise ValueError("Box connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Box project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Box project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.box.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes", "maxDownloadBytes", "uploadBaseUrl", "maxUploadBytes"}:
        raise ValueError("Box configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Box configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Box configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Box configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "maxDownloadBytes" in value:
        max_download_bytes = value["maxDownloadBytes"]
        if not (type(max_download_bytes) is int):
            raise ValueError("Box configuration maxDownloadBytes has an invalid type")
        config_arguments['max_download_bytes'] = max_download_bytes
    if "uploadBaseUrl" in value:
        upload_base_url = value["uploadBaseUrl"]
        if not (isinstance(upload_base_url, str)):
            raise ValueError("Box configuration uploadBaseUrl has an invalid type")
        config_arguments['upload_base_url'] = upload_base_url
    if "maxUploadBytes" in value:
        max_upload_bytes = value["maxUploadBytes"]
        if not (type(max_upload_bytes) is int):
            raise ValueError("Box configuration maxUploadBytes has an invalid type")
        config_arguments['max_upload_bytes'] = max_upload_bytes
    return Config(**config_arguments)
