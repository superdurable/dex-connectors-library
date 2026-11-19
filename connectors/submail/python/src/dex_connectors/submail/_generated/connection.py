# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_SMS_BALANCE_DEFINITION
from ..models import GetSMSBalanceInput, SMSBalance

class Connection:

    __slots__ = ('_get_sms_balance', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_sms_balance: Query[GetSMSBalanceInput, SMSBalance],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_sms_balance is None:
            raise ValueError("SUBMAIL SMS connection provider and operations are required")
        if get_sms_balance.definition().operation != GET_SMS_BALANCE_DEFINITION.operation:
            raise ValueError("SUBMAIL SMS connection operation identity differs")
        self.reference = reference
        self._get_sms_balance = get_sms_balance
        self._client = client

    def get_sms_balance(self) -> Query[GetSMSBalanceInput, SMSBalance]:
        return self._get_sms_balance

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "submail.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("SUBMAIL SMS connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.submail.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("SUBMAIL SMS client is required")
    return Connection(reference, client.get_sms_balance(), client)

setattr(new_connection, '__module__', 'dex_connectors.submail.connection')


def new_connection_with_operations(
    get_sms_balance: Query[GetSMSBalanceInput, SMSBalance],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_sms_balance)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.submail.connection')


class CredentialMaterialError(ValueError):
    "Stored SUBMAIL SMS credential material fails the connector credential checks, so no retry can succeed."


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
            raise CredentialMaterialError("SUBMAIL SMS stored credential does not match the connector credential fields")
        if set(material) - set(('appId', 'appKey')):
            raise CredentialMaterialError("SUBMAIL SMS stored credential does not match the connector credential fields")
        app_id = material.get("appId")
        if app_id is not None and not (isinstance(app_id, str)):
            raise CredentialMaterialError("SUBMAIL SMS credential appId has an invalid type")
        app_key = material.get("appKey")
        if app_key is not None and not (isinstance(app_key, str)):
            raise CredentialMaterialError("SUBMAIL SMS credential appKey has an invalid type")
        if not app_id:
            raise CredentialMaterialError("SUBMAIL SMS credential appId is required")
        if not app_key:
            raise CredentialMaterialError("SUBMAIL SMS credential appKey is required")
        return Credentials(app_id, SecretString(app_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("SUBMAIL SMS connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("SUBMAIL SMS project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("SUBMAIL SMS project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.submail.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("SUBMAIL SMS configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("SUBMAIL SMS configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("SUBMAIL SMS configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("SUBMAIL SMS configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
