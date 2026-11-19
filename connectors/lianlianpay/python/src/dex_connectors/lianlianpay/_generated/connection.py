# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, QUERY_PAYMENT_STATUS_DEFINITION
from ..models import PaymentStatusResult, QueryPaymentStatusInput

class Connection:

    __slots__ = ('_query_payment_status', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        query_payment_status: Query[QueryPaymentStatusInput, PaymentStatusResult],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or query_payment_status is None:
            raise ValueError("LianLian Pay connection provider and operations are required")
        if query_payment_status.definition().operation != QUERY_PAYMENT_STATUS_DEFINITION.operation:
            raise ValueError("LianLian Pay connection operation identity differs")
        self.reference = reference
        self._query_payment_status = query_payment_status
        self._client = client

    def query_payment_status(self) -> Query[QueryPaymentStatusInput, PaymentStatusResult]:
        return self._query_payment_status

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "lianlianpay.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("LianLian Pay connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.lianlianpay.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("LianLian Pay client is required")
    return Connection(reference, client.query_payment_status(), client)

setattr(new_connection, '__module__', 'dex_connectors.lianlianpay.connection')


def new_connection_with_operations(
    query_payment_status: Query[QueryPaymentStatusInput, PaymentStatusResult],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, query_payment_status)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.lianlianpay.connection')


class CredentialMaterialError(ValueError):
    "Stored LianLian Pay credential material fails the connector credential checks, so no retry can succeed."


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
            raise CredentialMaterialError("LianLian Pay stored credential does not match the connector credential fields")
        if set(material) - set(('merchant_id', 'private_key')):
            raise CredentialMaterialError("LianLian Pay stored credential does not match the connector credential fields")
        merchant_id = material.get("merchant_id")
        if merchant_id is not None and not (isinstance(merchant_id, str)):
            raise CredentialMaterialError("LianLian Pay credential merchant_id has an invalid type")
        private_key = material.get("private_key")
        if private_key is not None and not (isinstance(private_key, str)):
            raise CredentialMaterialError("LianLian Pay credential private_key has an invalid type")
        if not merchant_id:
            raise CredentialMaterialError("LianLian Pay credential merchant_id is required")
        if not private_key:
            raise CredentialMaterialError("LianLian Pay credential private_key is required")
        return Credentials(merchant_id, SecretString(private_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("LianLian Pay connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("LianLian Pay project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("LianLian Pay project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.lianlianpay.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("LianLian Pay configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("LianLian Pay configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("LianLian Pay configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("LianLian Pay configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
