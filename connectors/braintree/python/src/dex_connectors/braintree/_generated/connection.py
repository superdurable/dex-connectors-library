# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_TRANSACTION_DEFINITION
from ..models import GetTransactionInput, TransactionProfile

class Connection:

    __slots__ = ('_get_transaction', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_transaction: Query[GetTransactionInput, TransactionProfile],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_transaction is None:
            raise ValueError("Braintree connection provider and operations are required")
        if get_transaction.definition().operation != GET_TRANSACTION_DEFINITION.operation:
            raise ValueError("Braintree connection operation identity differs")
        self.reference = reference
        self._get_transaction = get_transaction
        self._client = client

    def get_transaction(self) -> Query[GetTransactionInput, TransactionProfile]:
        return self._get_transaction

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "braintree.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Braintree connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.braintree.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Braintree client is required")
    return Connection(reference, client.get_transaction(), client)

setattr(new_connection, '__module__', 'dex_connectors.braintree.connection')


def new_connection_with_operations(
    get_transaction: Query[GetTransactionInput, TransactionProfile],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_transaction)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.braintree.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Braintree stored credential does not match the connector credential fields")
        if set(material) - set(('publicKey', 'privateKey')):
            raise ValueError("Braintree stored credential does not match the connector credential fields")
        public_key = material.get("publicKey")
        if public_key is not None and not (isinstance(public_key, str)):
            raise ValueError("Braintree credential publicKey has an invalid type")
        private_key = material.get("privateKey")
        if private_key is not None and not (isinstance(private_key, str)):
            raise ValueError("Braintree credential privateKey has an invalid type")
        if not public_key:
            raise ValueError("Braintree credential publicKey is required")
        if not private_key:
            raise ValueError("Braintree credential privateKey is required")
        return Credentials(public_key, SecretString(private_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Braintree connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Braintree project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Braintree project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.braintree.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "apiVersion", "timeout", "maxResponseBytes"}:
        raise ValueError("Braintree configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Braintree configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "apiVersion" in value:
        api_version = value["apiVersion"]
        if not (isinstance(api_version, str)):
            raise ValueError("Braintree configuration apiVersion has an invalid type")
        config_arguments['api_version'] = api_version
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Braintree configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Braintree configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
