# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Square connection and project credential binding."""

from typing import Any

import httpx
from dex_connectors.sdk import Call, ConnectionRef, Query, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from .client import (
    CONNECTOR_ID,
    GET_PAYMENT_DEFINITION,
    LIST_PAYMENTS_DEFINITION,
    Client,
    Config,
    Credentials,
)
from .models import GetPaymentInput, ListPaymentsInput, Payment, PaymentPage


class Connection:
    """Nonserializable Square operation bindings for a named project connection."""

    __slots__ = ("_list_payments", "_get_payment", "_client", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        list_payments: Query[ListPaymentsInput, PaymentPage],
        get_payment: Query[GetPaymentInput, Payment],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_payments is None or get_payment is None:
            raise ValueError("Square connection provider and operations are required")
        if (
            list_payments.definition().operation != LIST_PAYMENTS_DEFINITION.operation
            or get_payment.definition().operation != GET_PAYMENT_DEFINITION.operation
        ):
            raise ValueError("Square connection operation identity differs")
        self.reference = reference
        self._list_payments = list_payments
        self._get_payment = get_payment
        self._client = client

    def list_payments(self) -> Query[ListPaymentsInput, PaymentPage]:
        """Return the payment page Query used by an application Step."""
        return self._list_payments

    def get_payment(self) -> Query[GetPaymentInput, Payment]:
        """Return the payment lookup Query used by an application Step."""
        return self._get_payment

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "square.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("Square connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("Square client is required")
    return Connection(reference, client.list_payments(), client.get_payment(), client)


def new_connection_with_operations(
    list_payments: Query[ListPaymentsInput, PaymentPage],
    get_payment: Query[GetPaymentInput, Payment],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock operations to the same connection identity as the real provider."""
    return Connection(reference, list_payments, get_payment)


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        value = material.get("access_token")
        if not isinstance(value, str) or not value:
            raise ValueError("Square credential access_token is required")
        return Credentials(SecretString(value))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared ordinary settings and resolve the current access token per call."""
    if project is None:
        raise ValueError("Square connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Square project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Square project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Square configuration has unknown fields")
    base_url = value.get("baseUrl", "")
    timeout = value.get("timeout", 0)
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not isinstance(base_url, str):
        raise ValueError("Square configuration baseUrl must be a string")
    if type(timeout) is not int or type(max_response_bytes) is not int:
        raise ValueError("Square configuration timeout and maxResponseBytes must be integers")
    return Config(base_url, timeout / 1_000_000_000, max_response_bytes)
