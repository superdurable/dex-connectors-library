# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_PAYMENT_DEFINITION
from ..models import GetPaymentInput, PaymentStatus

class Connection:

    __slots__ = ('_get_payment', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_payment: Query[GetPaymentInput, PaymentStatus],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_payment is None:
            raise ValueError("Razorpay connection provider and operations are required")
        if get_payment.definition().operation != GET_PAYMENT_DEFINITION.operation:
            raise ValueError("Razorpay connection operation identity differs")
        self.reference = reference
        self._get_payment = get_payment
        self._client = client

    def get_payment(self) -> Query[GetPaymentInput, PaymentStatus]:
        return self._get_payment

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "razorpay.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Razorpay connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.razorpay.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Razorpay client is required")
    return Connection(reference, client.get_payment(), client)

setattr(new_connection, '__module__', 'dex_connectors.razorpay.connection')


def new_connection_with_operations(
    get_payment: Query[GetPaymentInput, PaymentStatus],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_payment)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.razorpay.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Razorpay stored credential does not match the connector credential fields")
        if set(material) - set(('key_id', 'key_secret')):
            raise ValueError("Razorpay stored credential does not match the connector credential fields")
        key_id = material.get("key_id")
        if key_id is not None and not (isinstance(key_id, str)):
            raise ValueError("Razorpay credential key_id has an invalid type")
        key_secret = material.get("key_secret")
        if key_secret is not None and not (isinstance(key_secret, str)):
            raise ValueError("Razorpay credential key_secret has an invalid type")
        if not key_id:
            raise ValueError("Razorpay credential key_id is required")
        if not key_secret:
            raise ValueError("Razorpay credential key_secret is required")
        return Credentials(SecretString(key_id), SecretString(key_secret))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Razorpay connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Razorpay project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Razorpay project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.razorpay.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Razorpay configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Razorpay configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Razorpay configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Razorpay configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
