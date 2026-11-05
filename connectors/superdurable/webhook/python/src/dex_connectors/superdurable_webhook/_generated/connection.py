# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, SEND_EVENT_DEFINITION
from ..models import SendEventInput, SendEventOutput

class Connection:

    __slots__ = ('_send_event', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        send_event: Mutation[SendEventInput, SendEventOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or send_event is None:
            raise ValueError("Webhook connection provider and operations are required")
        if send_event.definition().operation != SEND_EVENT_DEFINITION.operation:
            raise ValueError("Webhook connection operation identity differs")
        self.reference = reference
        self._send_event = send_event
        self._client = client

    def send_event(self) -> Mutation[SendEventInput, SendEventOutput]:
        return self._send_event

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "superdurable_webhook.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Webhook connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.superdurable_webhook.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Webhook client is required")
    return Connection(reference, client.send_event(), client)

setattr(new_connection, '__module__', 'dex_connectors.superdurable_webhook.connection')


def new_connection_with_operations(
    send_event: Mutation[SendEventInput, SendEventOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, send_event)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.superdurable_webhook.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Webhook stored credential does not match the connector credential fields")
        if set(material) - set(('signing_secret',)):
            raise ValueError("Webhook stored credential does not match the connector credential fields")
        signing_secret = material.get("signing_secret")
        if signing_secret is not None and not (isinstance(signing_secret, str)):
            raise ValueError("Webhook credential signing_secret has an invalid type")
        if not signing_secret:
            raise ValueError("Webhook credential signing_secret is required")
        return Credentials(SecretString(signing_secret))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Webhook connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Webhook project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Webhook project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.superdurable_webhook.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"verification", "signatureHeader", "signaturePrefix", "signatureEncoding", "timestampToleranceSeconds", "tokenHeader", "eventIdHeader", "eventIdPointer", "maxBodyBytes", "forwardedHeaders", "deliveryUrl"}:
        raise ValueError("Webhook configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "verification" in value:
        verification = value["verification"]
        if not (isinstance(verification, str) and verification in ('hmacSha256', 'standardWebhooks', 'sharedToken')):
            raise ValueError("Webhook configuration verification has an invalid type")
        config_arguments['verification'] = verification
    if "signatureHeader" in value:
        signature_header = value["signatureHeader"]
        if not (isinstance(signature_header, str)):
            raise ValueError("Webhook configuration signatureHeader has an invalid type")
        config_arguments['signature_header'] = signature_header
    if "signaturePrefix" in value:
        signature_prefix = value["signaturePrefix"]
        if not (isinstance(signature_prefix, str)):
            raise ValueError("Webhook configuration signaturePrefix has an invalid type")
        config_arguments['signature_prefix'] = signature_prefix
    else:
        config_arguments['signature_prefix'] = ''
    if "signatureEncoding" in value:
        signature_encoding = value["signatureEncoding"]
        if not (isinstance(signature_encoding, str) and signature_encoding in ('hex', 'base64')):
            raise ValueError("Webhook configuration signatureEncoding has an invalid type")
        config_arguments['signature_encoding'] = signature_encoding
    if "timestampToleranceSeconds" in value:
        timestamp_tolerance_seconds = value["timestampToleranceSeconds"]
        if not (type(timestamp_tolerance_seconds) is int):
            raise ValueError("Webhook configuration timestampToleranceSeconds has an invalid type")
        config_arguments['timestamp_tolerance_seconds'] = timestamp_tolerance_seconds
    if "tokenHeader" in value:
        token_header = value["tokenHeader"]
        if not (isinstance(token_header, str)):
            raise ValueError("Webhook configuration tokenHeader has an invalid type")
        config_arguments['token_header'] = token_header
    if "eventIdHeader" in value:
        event_id_header = value["eventIdHeader"]
        if not (isinstance(event_id_header, str)):
            raise ValueError("Webhook configuration eventIdHeader has an invalid type")
        config_arguments['event_id_header'] = event_id_header
    else:
        config_arguments['event_id_header'] = ''
    if "eventIdPointer" in value:
        event_id_pointer = value["eventIdPointer"]
        if not (isinstance(event_id_pointer, str)):
            raise ValueError("Webhook configuration eventIdPointer has an invalid type")
        config_arguments['event_id_pointer'] = event_id_pointer
    else:
        config_arguments['event_id_pointer'] = ''
    if "maxBodyBytes" in value:
        max_body_bytes = value["maxBodyBytes"]
        if not (type(max_body_bytes) is int):
            raise ValueError("Webhook configuration maxBodyBytes has an invalid type")
        config_arguments['max_body_bytes'] = max_body_bytes
    if "forwardedHeaders" in value:
        forwarded_headers = value["forwardedHeaders"]
        if not (isinstance(forwarded_headers, list) and all(isinstance(item, str) for item in forwarded_headers)):
            raise ValueError("Webhook configuration forwardedHeaders has an invalid type")
        config_arguments['forwarded_headers'] = tuple(forwarded_headers)
    else:
        config_arguments['forwarded_headers'] = ()
    if "deliveryUrl" in value:
        delivery_url = value["deliveryUrl"]
        if not (isinstance(delivery_url, str)):
            raise ValueError("Webhook configuration deliveryUrl has an invalid type")
        config_arguments['delivery_url'] = delivery_url
    else:
        config_arguments['delivery_url'] = ''
    return Config(**config_arguments)
