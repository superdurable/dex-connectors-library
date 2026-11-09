# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_AGREEMENT_DEFINITION
from ..models import AgreementStatus, GetAgreementInput

class Connection:

    __slots__ = ('_get_agreement', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_agreement: Query[GetAgreementInput, AgreementStatus],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_agreement is None:
            raise ValueError("Adobe Acrobat Sign connection provider and operations are required")
        if get_agreement.definition().operation != GET_AGREEMENT_DEFINITION.operation:
            raise ValueError("Adobe Acrobat Sign connection operation identity differs")
        self.reference = reference
        self._get_agreement = get_agreement
        self._client = client

    def get_agreement(self) -> Query[GetAgreementInput, AgreementStatus]:
        return self._get_agreement

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "adobe_acrobat_sign.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Adobe Acrobat Sign connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.adobe_acrobat_sign.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Adobe Acrobat Sign client is required")
    return Connection(reference, client.get_agreement(), client)

setattr(new_connection, '__module__', 'dex_connectors.adobe_acrobat_sign.connection')


def new_connection_with_operations(
    get_agreement: Query[GetAgreementInput, AgreementStatus],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_agreement)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.adobe_acrobat_sign.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Adobe Acrobat Sign stored credential does not match the connector credential fields")
        if set(material) - set(('integration_key',)):
            raise ValueError("Adobe Acrobat Sign stored credential does not match the connector credential fields")
        integration_key = material.get("integration_key")
        if integration_key is not None and not (isinstance(integration_key, str)):
            raise ValueError("Adobe Acrobat Sign credential integration_key has an invalid type")
        if not integration_key:
            raise ValueError("Adobe Acrobat Sign credential integration_key is required")
        return Credentials(SecretString(integration_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Adobe Acrobat Sign connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Adobe Acrobat Sign project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Adobe Acrobat Sign project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.adobe_acrobat_sign.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Adobe Acrobat Sign configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Adobe Acrobat Sign configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Adobe Acrobat Sign configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Adobe Acrobat Sign configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
