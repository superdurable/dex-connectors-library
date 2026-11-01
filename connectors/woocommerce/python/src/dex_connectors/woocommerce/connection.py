# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""WooCommerce connection and project credential binding."""

from typing import Any

import httpx
from dex_connectors.sdk import Call, ConnectionRef, Query, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from .client import (
    CONNECTOR_ID,
    GET_ORDER_DEFINITION,
    LIST_ORDERS_DEFINITION,
    Client,
    Config,
    Credentials,
)
from .models import GetOrderInput, ListOrdersInput, Order, OrderPage


class Connection:
    """Nonserializable WooCommerce operation bindings for a named project connection."""

    __slots__ = ("_list_orders", "_get_order", "_client", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        list_orders: Query[ListOrdersInput, OrderPage],
        get_order: Query[GetOrderInput, Order],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_orders is None or get_order is None:
            raise ValueError("WooCommerce connection provider and operations are required")
        if (
            list_orders.definition().operation != LIST_ORDERS_DEFINITION.operation
            or get_order.definition().operation != GET_ORDER_DEFINITION.operation
        ):
            raise ValueError("WooCommerce connection operation identity differs")
        self.reference = reference
        self._list_orders = list_orders
        self._get_order = get_order
        self._client = client

    def list_orders(self) -> Query[ListOrdersInput, OrderPage]:
        """Return the order page Query used by an application Step."""
        return self._list_orders

    def get_order(self) -> Query[GetOrderInput, Order]:
        """Return the order lookup Query used by an application Step."""
        return self._get_order

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "woocommerce.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("WooCommerce connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("WooCommerce client is required")
    return Connection(reference, client.list_orders(), client.get_order(), client)


def new_connection_with_operations(
    list_orders: Query[ListOrdersInput, OrderPage],
    get_order: Query[GetOrderInput, Order],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock operations to the same connection identity as the real provider."""
    return Connection(reference, list_orders, get_order)


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        key = material.get("consumer_key")
        secret = material.get("consumer_secret")
        if not isinstance(key, str) or not key or not isinstance(secret, str) or not secret:
            raise ValueError("WooCommerce credentials consumer_key and consumer_secret are required")
        return Credentials(SecretString(key), SecretString(secret))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared ordinary settings and resolve the current consumer key pair per call."""
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


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"storeUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("WooCommerce configuration has unknown fields")
    store_url = value.get("storeUrl", "")
    timeout = value.get("timeout", 0)
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not isinstance(store_url, str):
        raise ValueError("WooCommerce configuration storeUrl must be a string")
    if type(timeout) is not int or type(max_response_bytes) is not int:
        raise ValueError("WooCommerce configuration timeout and maxResponseBytes must be integers")
    return Config(store_url, timeout / 1_000_000_000, max_response_bytes)
