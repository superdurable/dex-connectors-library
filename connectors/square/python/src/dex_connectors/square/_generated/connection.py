# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_PAYMENT_DEFINITION, LIST_PAYMENTS_DEFINITION
from ..models import GetPaymentInput, ListPaymentsInput, Payment, PaymentPage

class Connection:

    __slots__ = ('_list_payments', '_get_payment', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_payments: Query[ListPaymentsInput, PaymentPage],
        get_payment: Query[GetPaymentInput, Payment],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_payments is None or get_payment is None:
            raise ValueError("Square connection provider and operations are required")
        if list_payments.definition().operation != LIST_PAYMENTS_DEFINITION.operation:
            raise ValueError("Square connection operation identity differs")
        if get_payment.definition().operation != GET_PAYMENT_DEFINITION.operation:
            raise ValueError("Square connection operation identity differs")
        self.reference = reference
        self._list_payments = list_payments
        self._get_payment = get_payment
        self._client = client

    def list_payments(self) -> Query[ListPaymentsInput, PaymentPage]:
        return self._list_payments

    def get_payment(self) -> Query[GetPaymentInput, Payment]:
        return self._get_payment

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "square.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Square connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.square.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Square client is required")
    return Connection(reference, client.list_payments(), client.get_payment(), client)

setattr(new_connection, '__module__', 'dex_connectors.square.connection')


def new_connection_with_operations(
    list_payments: Query[ListPaymentsInput, PaymentPage],
    get_payment: Query[GetPaymentInput, Payment],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_payments, get_payment)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.square.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        access_token = material.get("access_token")
        if not isinstance(access_token, str) or not access_token:
            raise ValueError("Square credential access_token is required")
        return Credentials(SecretString(access_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
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

setattr(new_project_connection, '__module__', 'dex_connectors.square.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Square configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Square configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Square configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Square configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
