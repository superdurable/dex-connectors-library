# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_EMAIL_IDENTITY_DEFINITION
from ..models import EmailIdentityDetails, GetEmailIdentityInput

class Connection:

    __slots__ = ('_get_email_identity', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_email_identity: Query[GetEmailIdentityInput, EmailIdentityDetails],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_email_identity is None:
            raise ValueError("Amazon SES connection provider and operations are required")
        if get_email_identity.definition().operation != GET_EMAIL_IDENTITY_DEFINITION.operation:
            raise ValueError("Amazon SES connection operation identity differs")
        self.reference = reference
        self._get_email_identity = get_email_identity
        self._client = client

    def get_email_identity(self) -> Query[GetEmailIdentityInput, EmailIdentityDetails]:
        return self._get_email_identity

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "amazon_ses.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Amazon SES connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.amazon_ses.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Amazon SES client is required")
    return Connection(reference, client.get_email_identity(), client)

setattr(new_connection, '__module__', 'dex_connectors.amazon_ses.connection')


def new_connection_with_operations(
    get_email_identity: Query[GetEmailIdentityInput, EmailIdentityDetails],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_email_identity)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.amazon_ses.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Amazon SES stored credential does not match the connector credential fields")
        if set(material) - set(('access_key_id', 'secret_access_key', 'session_token')):
            raise ValueError("Amazon SES stored credential does not match the connector credential fields")
        access_key_id = material.get("access_key_id")
        if access_key_id is not None and not (isinstance(access_key_id, str)):
            raise ValueError("Amazon SES credential access_key_id has an invalid type")
        secret_access_key = material.get("secret_access_key")
        if secret_access_key is not None and not (isinstance(secret_access_key, str)):
            raise ValueError("Amazon SES credential secret_access_key has an invalid type")
        session_token = material.get("session_token")
        if session_token is not None and not (isinstance(session_token, str)):
            raise ValueError("Amazon SES credential session_token has an invalid type")
        if not access_key_id:
            raise ValueError("Amazon SES credential access_key_id is required")
        if not secret_access_key:
            raise ValueError("Amazon SES credential secret_access_key is required")
        return Credentials(SecretString(access_key_id), SecretString(secret_access_key), SecretString(session_token or ''))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Amazon SES connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Amazon SES project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Amazon SES project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.amazon_ses.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"region", "endpoint", "timeout", "maxResponseBytes"}:
        raise ValueError("Amazon SES configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "region" in value and value["region"] != '':
        region = value["region"]
        if not (isinstance(region, str)):
            raise ValueError("Amazon SES configuration region has an invalid type")
        config_arguments['region'] = region
    if "endpoint" in value and value["endpoint"] != '':
        endpoint = value["endpoint"]
        if not (isinstance(endpoint, str)):
            raise ValueError("Amazon SES configuration endpoint has an invalid type")
        config_arguments['endpoint'] = endpoint
    else:
        config_arguments['endpoint'] = ''
    if "timeout" in value and value["timeout"] != '':
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Amazon SES configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value and value["maxResponseBytes"] != '':
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Amazon SES configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
