# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, LIST_ROOT_GOODS_CATEGORIES_DEFINITION
from ..models import ListRootGoodsCategoriesInput, ListRootGoodsCategoriesOutput

class Connection:

    __slots__ = ('_list_root_goods_categories', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_root_goods_categories: Query[ListRootGoodsCategoriesInput, ListRootGoodsCategoriesOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_root_goods_categories is None:
            raise ValueError("XiaoeTech (小鹅通) connection provider and operations are required")
        if list_root_goods_categories.definition().operation != LIST_ROOT_GOODS_CATEGORIES_DEFINITION.operation:
            raise ValueError("XiaoeTech (小鹅通) connection operation identity differs")
        self.reference = reference
        self._list_root_goods_categories = list_root_goods_categories
        self._client = client

    def list_root_goods_categories(self) -> Query[ListRootGoodsCategoriesInput, ListRootGoodsCategoriesOutput]:
        return self._list_root_goods_categories

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "xiaoetech.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("XiaoeTech (小鹅通) connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.xiaoetech.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("XiaoeTech (小鹅通) client is required")
    return Connection(reference, client.list_root_goods_categories(), client)

setattr(new_connection, '__module__', 'dex_connectors.xiaoetech.connection')


def new_connection_with_operations(
    list_root_goods_categories: Query[ListRootGoodsCategoriesInput, ListRootGoodsCategoriesOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_root_goods_categories)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.xiaoetech.connection')


class CredentialMaterialError(ValueError):
    "Stored XiaoeTech (小鹅通) credential material fails the connector credential checks, so no retry can succeed."


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
            raise CredentialMaterialError("XiaoeTech (小鹅通) stored credential does not match the connector credential fields")
        if set(material) - set(('app_id', 'client_id', 'secret_key')):
            raise CredentialMaterialError("XiaoeTech (小鹅通) stored credential does not match the connector credential fields")
        app_id = material.get("app_id")
        if app_id is not None and not (isinstance(app_id, str)):
            raise CredentialMaterialError("XiaoeTech (小鹅通) credential app_id has an invalid type")
        client_id = material.get("client_id")
        if client_id is not None and not (isinstance(client_id, str)):
            raise CredentialMaterialError("XiaoeTech (小鹅通) credential client_id has an invalid type")
        secret_key = material.get("secret_key")
        if secret_key is not None and not (isinstance(secret_key, str)):
            raise CredentialMaterialError("XiaoeTech (小鹅通) credential secret_key has an invalid type")
        if not app_id:
            raise CredentialMaterialError("XiaoeTech (小鹅通) credential app_id is required")
        if not client_id:
            raise CredentialMaterialError("XiaoeTech (小鹅通) credential client_id is required")
        if not secret_key:
            raise CredentialMaterialError("XiaoeTech (小鹅通) credential secret_key is required")
        return Credentials(app_id, client_id, SecretString(secret_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("XiaoeTech (小鹅通) connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("XiaoeTech (小鹅通) project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("XiaoeTech (小鹅通) project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.xiaoetech.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("XiaoeTech (小鹅通) configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("XiaoeTech (小鹅通) configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("XiaoeTech (小鹅通) configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("XiaoeTech (小鹅通) configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
