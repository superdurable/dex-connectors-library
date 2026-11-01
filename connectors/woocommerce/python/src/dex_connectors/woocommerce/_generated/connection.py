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
            raise ValueError("WooCommerce connection provider and operations are required")
        if list_orders.definition().operation != LIST_ORDERS_DEFINITION.operation:
            raise ValueError("WooCommerce connection operation identity differs")
        if get_order.definition().operation != GET_ORDER_DEFINITION.operation:
            raise ValueError("WooCommerce connection operation identity differs")
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
        return "woocommerce.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("WooCommerce connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.woocommerce.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("WooCommerce client is required")
    return Connection(reference, client.list_orders(), client.get_order(), client)

setattr(new_connection, '__module__', 'dex_connectors.woocommerce.connection')


def new_connection_with_operations(
    list_orders: Query[ListOrdersInput, OrderPage],
    get_order: Query[GetOrderInput, Order],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_orders, get_order)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.woocommerce.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        consumer_key = material.get("consumer_key")
        if not isinstance(consumer_key, str) or not consumer_key:
            raise ValueError("WooCommerce credential consumer_key is required")
        consumer_secret = material.get("consumer_secret")
        if not isinstance(consumer_secret, str) or not consumer_secret:
            raise ValueError("WooCommerce credential consumer_secret is required")
        return Credentials(SecretString(consumer_key), SecretString(consumer_secret))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("WooCommerce connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("WooCommerce project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("WooCommerce project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.woocommerce.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"storeUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("WooCommerce configuration has unknown fields")
    store_url = value.get("storeUrl", '')
    if not isinstance(store_url, str):
        raise ValueError("WooCommerce configuration storeUrl has an invalid type")
    timeout = value.get("timeout", 0)
    if not type(timeout) is int:
        raise ValueError("WooCommerce configuration timeout has an invalid type")
    timeout_seconds = timeout / 1_000_000_000
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not type(max_response_bytes) is int:
        raise ValueError("WooCommerce configuration maxResponseBytes has an invalid type")
    return Config(store_url=store_url, timeout_seconds=timeout_seconds, max_response_bytes=max_response_bytes)
