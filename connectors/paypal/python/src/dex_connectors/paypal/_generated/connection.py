# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_ORDER_DEFINITION, LIST_TRANSACTIONS_DEFINITION
from ..models import GetOrderInput, ListTransactionsInput, Order, TransactionPage

class Connection:

    __slots__ = ('_list_transactions', '_get_order', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_transactions: Query[ListTransactionsInput, TransactionPage],
        get_order: Query[GetOrderInput, Order],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_transactions is None or get_order is None:
            raise ValueError("PayPal connection provider and operations are required")
        if list_transactions.definition().operation != LIST_TRANSACTIONS_DEFINITION.operation:
            raise ValueError("PayPal connection operation identity differs")
        if get_order.definition().operation != GET_ORDER_DEFINITION.operation:
            raise ValueError("PayPal connection operation identity differs")
        self.reference = reference
        self._list_transactions = list_transactions
        self._get_order = get_order
        self._client = client

    def list_transactions(self) -> Query[ListTransactionsInput, TransactionPage]:
        return self._list_transactions

    def get_order(self) -> Query[GetOrderInput, Order]:
        return self._get_order

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "paypal.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("PayPal connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.paypal.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("PayPal client is required")
    return Connection(reference, client.list_transactions(), client.get_order(), client)

setattr(new_connection, '__module__', 'dex_connectors.paypal.connection')


def new_connection_with_operations(
    list_transactions: Query[ListTransactionsInput, TransactionPage],
    get_order: Query[GetOrderInput, Order],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_transactions, get_order)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.paypal.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        client_id = material.get("client_id")
        if not isinstance(client_id, str) or not client_id:
            raise ValueError("PayPal credential client_id is required")
        client_secret = material.get("client_secret")
        if not isinstance(client_secret, str) or not client_secret:
            raise ValueError("PayPal credential client_secret is required")
        return Credentials(client_id, SecretString(client_secret))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("PayPal connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("PayPal project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("PayPal project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.paypal.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("PayPal configuration has unknown fields")
    base_url = value.get("baseUrl", '')
    if not isinstance(base_url, str):
        raise ValueError("PayPal configuration baseUrl has an invalid type")
    timeout = value.get("timeout", 0)
    if not type(timeout) is int:
        raise ValueError("PayPal configuration timeout has an invalid type")
    timeout_seconds = timeout / 1_000_000_000
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not type(max_response_bytes) is int:
        raise ValueError("PayPal configuration maxResponseBytes has an invalid type")
    return Config(base_url=base_url, timeout_seconds=timeout_seconds, max_response_bytes=max_response_bytes)
