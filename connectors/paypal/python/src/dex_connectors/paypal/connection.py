# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""PayPal connection and project credential binding."""

from typing import Any

import httpx
from dex_connectors.sdk import Call, ConnectionRef, Query, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from .client import (
    CONNECTOR_ID,
    GET_ORDER_DEFINITION,
    LIST_TRANSACTIONS_DEFINITION,
    Client,
    Config,
    Credentials,
)
from .models import GetOrderInput, ListTransactionsInput, Order, TransactionPage


class Connection:
    """Nonserializable PayPal operation bindings for a named project connection."""

    __slots__ = ("_list_transactions", "_get_order", "_client", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        list_transactions: Query[ListTransactionsInput, TransactionPage],
        get_order: Query[GetOrderInput, Order],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_transactions is None or get_order is None:
            raise ValueError("PayPal connection provider and operations are required")
        if (
            list_transactions.definition().operation != LIST_TRANSACTIONS_DEFINITION.operation
            or get_order.definition().operation != GET_ORDER_DEFINITION.operation
        ):
            raise ValueError("PayPal connection operation identity differs")
        self.reference = reference
        self._list_transactions = list_transactions
        self._get_order = get_order
        self._client = client

    def list_transactions(self) -> Query[ListTransactionsInput, TransactionPage]:
        """Return the transaction page Query used by an application Step."""
        return self._list_transactions

    def get_order(self) -> Query[GetOrderInput, Order]:
        """Return the order lookup Query used by an application Step."""
        return self._get_order

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "paypal.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("PayPal connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("PayPal client is required")
    return Connection(reference, client.list_transactions(), client.get_order(), client)


def new_connection_with_operations(
    list_transactions: Query[ListTransactionsInput, TransactionPage],
    get_order: Query[GetOrderInput, Order],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock operations to the same connection identity as the real provider."""
    return Connection(reference, list_transactions, get_order)


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        client_id = material.get("client_id")
        client_secret = material.get("client_secret")
        if (
            not isinstance(client_id, str)
            or not client_id
            or not isinstance(client_secret, str)
            or not client_secret
        ):
            raise ValueError("PayPal credentials client_id and client_secret are required")
        return Credentials(client_id, SecretString(client_secret))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared ordinary settings and resolve the current client credentials per call."""
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


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("PayPal configuration has unknown fields")
    base_url = value.get("baseUrl", "")
    timeout = value.get("timeout", 0)
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not isinstance(base_url, str):
        raise ValueError("PayPal configuration baseUrl must be a string")
    if type(timeout) is not int or type(max_response_bytes) is not int:
        raise ValueError("PayPal configuration timeout and maxResponseBytes must be integers")
    return Config(base_url, timeout / 1_000_000_000, max_response_bytes)
