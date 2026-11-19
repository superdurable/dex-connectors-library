# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, LIST_FOLDER_FILES_DEFINITION
from ..models import ListFolderFilesInput, ListFolderFilesOutput

class Connection:

    __slots__ = ('_list_folder_files', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_folder_files: Query[ListFolderFilesInput, ListFolderFilesOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_folder_files is None:
            raise ValueError("WPS 365 connection provider and operations are required")
        if list_folder_files.definition().operation != LIST_FOLDER_FILES_DEFINITION.operation:
            raise ValueError("WPS 365 connection operation identity differs")
        self.reference = reference
        self._list_folder_files = list_folder_files
        self._client = client

    def list_folder_files(self) -> Query[ListFolderFilesInput, ListFolderFilesOutput]:
        return self._list_folder_files

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "kingsoftoffice.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("WPS 365 connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.kingsoftoffice.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("WPS 365 client is required")
    return Connection(reference, client.list_folder_files(), client)

setattr(new_connection, '__module__', 'dex_connectors.kingsoftoffice.connection')


def new_connection_with_operations(
    list_folder_files: Query[ListFolderFilesInput, ListFolderFilesOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_folder_files)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.kingsoftoffice.connection')


class CredentialMaterialError(ValueError):
    "Stored WPS 365 credential material fails the connector credential checks, so no retry can succeed."


def is_credential_material_error(error: BaseException) -> bool:
    """Report malformed stored credential material, also behind the SDK provider's decoding ValueError."""
    if isinstance(error, CredentialMaterialError):
        return True
    # Only the SDK decoding wrapper qualifies; other errors may be transient.
    return (
        type(error) is ValueError
        and str(error) in {'project credential decoding failed', 'shared project credential decoding failed'}
        and isinstance(error.__context__, CredentialMaterialError)
    )


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise CredentialMaterialError("WPS 365 stored credential does not match the connector credential fields")
        if set(material) - set(('app_id', 'app_key')):
            raise CredentialMaterialError("WPS 365 stored credential does not match the connector credential fields")
        app_id = material.get("app_id")
        if app_id is not None and not (isinstance(app_id, str)):
            raise CredentialMaterialError("WPS 365 credential app_id has an invalid type")
        app_key = material.get("app_key")
        if app_key is not None and not (isinstance(app_key, str)):
            raise CredentialMaterialError("WPS 365 credential app_key has an invalid type")
        if not app_id:
            raise CredentialMaterialError("WPS 365 credential app_id is required")
        if not app_key:
            raise CredentialMaterialError("WPS 365 credential app_key is required")
        return Credentials(app_id, SecretString(app_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("WPS 365 connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("WPS 365 project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("WPS 365 project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.kingsoftoffice.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes", "pageSize", "requestSigningEnabled"}:
        raise ValueError("WPS 365 configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("WPS 365 configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("WPS 365 configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("WPS 365 configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "pageSize" in value:
        page_size = value["pageSize"]
        if not (type(page_size) is int):
            raise ValueError("WPS 365 configuration pageSize has an invalid type")
        config_arguments['page_size'] = page_size
    if "requestSigningEnabled" in value:
        request_signing_enabled = value["requestSigningEnabled"]
        if not (type(request_signing_enabled) is bool):
            raise ValueError("WPS 365 configuration requestSigningEnabled has an invalid type")
        config_arguments['request_signing_enabled'] = request_signing_enabled
    return Config(**config_arguments)
