# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, QUERY_SHOP_DEFINITION
from ..models import QueryShopInput, ShopResult

class Connection:

    __slots__ = ('_query_shop', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        query_shop: Query[QueryShopInput, ShopResult],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or query_shop is None:
            raise ValueError("EasyAPI E-Invoice connection provider and operations are required")
        if query_shop.definition().operation != QUERY_SHOP_DEFINITION.operation:
            raise ValueError("EasyAPI E-Invoice connection operation identity differs")
        self.reference = reference
        self._query_shop = query_shop
        self._client = client

    def query_shop(self) -> Query[QueryShopInput, ShopResult]:
        return self._query_shop

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "easyapi.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("EasyAPI E-Invoice connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.easyapi.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("EasyAPI E-Invoice client is required")
    return Connection(reference, client.query_shop(), client)

setattr(new_connection, '__module__', 'dex_connectors.easyapi.connection')


def new_connection_with_operations(
    query_shop: Query[QueryShopInput, ShopResult],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, query_shop)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.easyapi.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("EasyAPI E-Invoice stored credential does not match the connector credential fields")
        if set(material) - set(('app_key', 'app_secret')):
            raise ValueError("EasyAPI E-Invoice stored credential does not match the connector credential fields")
        app_key = material.get("app_key")
        if app_key is not None and not (isinstance(app_key, str)):
            raise ValueError("EasyAPI E-Invoice credential app_key has an invalid type")
        app_secret = material.get("app_secret")
        if app_secret is not None and not (isinstance(app_secret, str)):
            raise ValueError("EasyAPI E-Invoice credential app_secret has an invalid type")
        if not app_key:
            raise ValueError("EasyAPI E-Invoice credential app_key is required")
        if not app_secret:
            raise ValueError("EasyAPI E-Invoice credential app_secret is required")
        return Credentials(SecretString(app_key), SecretString(app_secret))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("EasyAPI E-Invoice connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("EasyAPI E-Invoice project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("EasyAPI E-Invoice project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.easyapi.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("EasyAPI E-Invoice configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("EasyAPI E-Invoice configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("EasyAPI E-Invoice configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("EasyAPI E-Invoice configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
