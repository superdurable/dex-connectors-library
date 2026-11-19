# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_STORE_BASIC_DEFINITION
from ..models import GetStoreBasicInput, StoreBasic

class Connection:

    __slots__ = ('_get_store_basic', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_store_basic: Query[GetStoreBasicInput, StoreBasic],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_store_basic is None:
            raise ValueError("Weimob connection provider and operations are required")
        if get_store_basic.definition().operation != GET_STORE_BASIC_DEFINITION.operation:
            raise ValueError("Weimob connection operation identity differs")
        self.reference = reference
        self._get_store_basic = get_store_basic
        self._client = client

    def get_store_basic(self) -> Query[GetStoreBasicInput, StoreBasic]:
        return self._get_store_basic

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "weimob.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Weimob connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.weimob.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Weimob client is required")
    return Connection(reference, client.get_store_basic(), client)

setattr(new_connection, '__module__', 'dex_connectors.weimob.connection')


def new_connection_with_operations(
    get_store_basic: Query[GetStoreBasicInput, StoreBasic],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_store_basic)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.weimob.connection')


class CredentialMaterialError(ValueError):
    "Stored Weimob credential material fails the connector credential checks, so no retry can succeed."


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
            raise CredentialMaterialError("Weimob stored credential does not match the connector credential fields")
        if set(material) - set(('clientID', 'clientSecret', 'shopID')):
            raise CredentialMaterialError("Weimob stored credential does not match the connector credential fields")
        client_id = material.get("clientID")
        if client_id is not None and not (isinstance(client_id, str)):
            raise CredentialMaterialError("Weimob credential clientID has an invalid type")
        client_secret = material.get("clientSecret")
        if client_secret is not None and not (isinstance(client_secret, str)):
            raise CredentialMaterialError("Weimob credential clientSecret has an invalid type")
        shop_id = material.get("shopID")
        if shop_id is not None and not (isinstance(shop_id, str)):
            raise CredentialMaterialError("Weimob credential shopID has an invalid type")
        if not client_id:
            raise CredentialMaterialError("Weimob credential clientID is required")
        if not client_secret:
            raise CredentialMaterialError("Weimob credential clientSecret is required")
        return Credentials(client_id, SecretString(client_secret), (shop_id or ''))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Weimob connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Weimob project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Weimob project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.weimob.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Weimob configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value and value["baseUrl"] != '':
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Weimob configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value and value["timeout"] != '':
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Weimob configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value and value["maxResponseBytes"] != '':
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Weimob configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
