# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_ORDER_DEFINITION, LIST_ORDERS_DEFINITION
from ..models import GetOrderInput, ListOrdersInput, Order, OrderPage

class Connection:

    __slots__ = ('_list_orders', '_get_order', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_orders: Query[ListOrdersInput, OrderPage],
        get_order: Query[GetOrderInput, Order],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_orders is None or get_order is None:
            raise ValueError("Shopify connection provider and operations are required")
        if list_orders.definition().operation != LIST_ORDERS_DEFINITION.operation:
            raise ValueError("Shopify connection operation identity differs")
        if get_order.definition().operation != GET_ORDER_DEFINITION.operation:
            raise ValueError("Shopify connection operation identity differs")
        self.reference = reference
        self._list_orders = list_orders
        self._get_order = get_order
        self._client = client

    def list_orders(self) -> Query[ListOrdersInput, OrderPage]:
        return self._list_orders

    def get_order(self) -> Query[GetOrderInput, Order]:
        return self._get_order

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "shopify.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Shopify connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.shopify.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Shopify client is required")
    return Connection(reference, client.list_orders(), client.get_order(), client)

setattr(new_connection, '__module__', 'dex_connectors.shopify.connection')


def new_connection_with_operations(
    list_orders: Query[ListOrdersInput, OrderPage],
    get_order: Query[GetOrderInput, Order],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_orders, get_order)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.shopify.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        access_token = material.get("access_token")
        if not isinstance(access_token, str) or not access_token:
            raise ValueError("Shopify credential access_token is required")
        return Credentials(SecretString(access_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Shopify connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Shopify project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Shopify project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.shopify.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"shopSubdomain", "timeout", "maxResponseBytes"}:
        raise ValueError("Shopify configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "shopSubdomain" in value:
        shop_subdomain = value["shopSubdomain"]
        if not (isinstance(shop_subdomain, str)):
            raise ValueError("Shopify configuration shopSubdomain has an invalid type")
        config_arguments['shop_subdomain'] = shop_subdomain
    else:
        raise ValueError("Shopify configuration shopSubdomain is required")
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Shopify configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Shopify configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
