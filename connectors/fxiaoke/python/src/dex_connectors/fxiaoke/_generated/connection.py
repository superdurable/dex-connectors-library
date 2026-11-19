# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, LIST_OBJECT_METADATA_DEFINITION
from ..models import ListObjectMetadataInput, ListObjectMetadataOutput

class Connection:

    __slots__ = ('_list_object_metadata', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_object_metadata: Query[ListObjectMetadataInput, ListObjectMetadataOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_object_metadata is None:
            raise ValueError("FXiaoke CRM connection provider and operations are required")
        if list_object_metadata.definition().operation != LIST_OBJECT_METADATA_DEFINITION.operation:
            raise ValueError("FXiaoke CRM connection operation identity differs")
        self.reference = reference
        self._list_object_metadata = list_object_metadata
        self._client = client

    def list_object_metadata(self) -> Query[ListObjectMetadataInput, ListObjectMetadataOutput]:
        return self._list_object_metadata

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "fxiaoke.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("FXiaoke CRM connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.fxiaoke.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("FXiaoke CRM client is required")
    return Connection(reference, client.list_object_metadata(), client)

setattr(new_connection, '__module__', 'dex_connectors.fxiaoke.connection')


def new_connection_with_operations(
    list_object_metadata: Query[ListObjectMetadataInput, ListObjectMetadataOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_object_metadata)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.fxiaoke.connection')


class CredentialMaterialError(ValueError):
    "Stored FXiaoke CRM credential material fails the connector credential checks, so no retry can succeed."


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
            raise CredentialMaterialError("FXiaoke CRM stored credential does not match the connector credential fields")
        if set(material) - set(('app_id', 'app_secret', 'permanent_code')):
            raise CredentialMaterialError("FXiaoke CRM stored credential does not match the connector credential fields")
        app_id = material.get("app_id")
        if app_id is not None and not (isinstance(app_id, str)):
            raise CredentialMaterialError("FXiaoke CRM credential app_id has an invalid type")
        app_secret = material.get("app_secret")
        if app_secret is not None and not (isinstance(app_secret, str)):
            raise CredentialMaterialError("FXiaoke CRM credential app_secret has an invalid type")
        permanent_code = material.get("permanent_code")
        if permanent_code is not None and not (isinstance(permanent_code, str)):
            raise CredentialMaterialError("FXiaoke CRM credential permanent_code has an invalid type")
        if not app_id:
            raise CredentialMaterialError("FXiaoke CRM credential app_id is required")
        if not app_secret:
            raise CredentialMaterialError("FXiaoke CRM credential app_secret is required")
        if not permanent_code:
            raise CredentialMaterialError("FXiaoke CRM credential permanent_code is required")
        return Credentials(app_id, SecretString(app_secret), SecretString(permanent_code))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("FXiaoke CRM connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("FXiaoke CRM project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("FXiaoke CRM project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.fxiaoke.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"cloudHost", "employeeId", "timeout", "maxResponseBytes"}:
        raise ValueError("FXiaoke CRM configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "cloudHost" in value:
        cloud_host = value["cloudHost"]
        if not (isinstance(cloud_host, str)):
            raise ValueError("FXiaoke CRM configuration cloudHost has an invalid type")
        config_arguments['cloud_host'] = cloud_host
    if "employeeId" in value:
        employee_id = value["employeeId"]
        if not (isinstance(employee_id, str)):
            raise ValueError("FXiaoke CRM configuration employeeId has an invalid type")
        config_arguments['employee_id'] = employee_id
    else:
        raise ValueError("FXiaoke CRM configuration employeeId is required")
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("FXiaoke CRM configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("FXiaoke CRM configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
