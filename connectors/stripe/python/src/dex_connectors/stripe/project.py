# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Open Stripe API clients from Dex project configuration and private credentials."""

from collections.abc import Mapping
from typing import Any

import httpx
from dex_connectors.sdk import Call, SecretString
from dex_connectors.sdk.projectconfig import LoadedProject
from dex_connectors.sdk.projectconfig.connections import ConnectionKey

from .client import Client
from .models import Config, Credentials


class _ProjectCredentialProvider:
    def __init__(self, project: LoadedProject, key: ConnectionKey, auth_method_id: str) -> None:
        self._project = project
        self._key = key
        self._auth_method_id = auth_method_id

    async def resolve(self, call: Call) -> Credentials:
        if (
            call.operation.connector_id != self._key.connector_id
            or call.connection.name != self._key.connection_name
        ):
            raise ValueError("connector call differs from configured project connection")
        material = await self._project.resolve_credential(self._key, str(call.id))
        values = dict(material)
        if self._auth_method_id and not values.get("auth_method"):
            values["auth_method"] = self._auth_method_id
        return decode_credentials(values)


def decode_credentials(material: dict[str, Any]) -> Credentials:
    """Validate the shared Stripe credential JSON without exposing secret text.

    Connections predating auth methods use the webhook method, as in the Go
    connector. An API-key-only connection needs no signing secret.
    """
    if not isinstance(material, dict) or set(material) - {"auth_method", "secret_key", "webhook_secret"}:
        raise ValueError("Stripe connection credentials are invalid")
    method = material.get("auth_method", "") or "api-key-with-webhook"
    secret_key = material.get("secret_key", "")
    webhook_secret = material.get("webhook_secret", "")
    if not isinstance(method, str) or not isinstance(secret_key, str) or not isinstance(webhook_secret, str):
        raise ValueError("Stripe connection credentials are invalid")
    if method not in ("api-key-only", "api-key-with-webhook") or not secret_key:
        raise ValueError("Stripe connection credentials are invalid")
    if method == "api-key-with-webhook" and not webhook_secret:
        raise ValueError("Stripe connection credentials are invalid")
    return Credentials(
        SecretString(secret_key), SecretString(webhook_secret) if webhook_secret else None, method
    )


def new_project_client(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Client:
    """Open a declared Stripe connection with call-time private credential lookup.

    The loaded project owns its snapshot and credential store. This function
    validates ordinary settings now; each operation resolves current private
    material from ``ConnectionStore`` when its provider call begins.
    """
    if project is None or not connection_name.strip():
        raise ValueError("Stripe project and connection name are required")
    matches = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == "stripe" and entry.connection_name == connection_name
    ]
    if len(matches) != 1 or matches[0].provider != "stripe":
        raise ValueError("Stripe project connection was not found")
    settings: Mapping[str, object] = matches[0].configuration
    if set(settings) - {"endpoint", "maxResponseBytes", "webhookMaxBodyBytes", "webhookSignatureTolerance"}:
        raise ValueError("Stripe connection configuration has unknown fields")
    defaults = Config()
    endpoint = settings.get("endpoint") or defaults.endpoint
    max_response_bytes = settings.get("maxResponseBytes") or defaults.max_response_bytes
    webhook_max_body_bytes = settings.get("webhookMaxBodyBytes") or defaults.webhook_max_body_bytes
    webhook_tolerance = settings.get("webhookSignatureTolerance") or defaults.webhook_signature_tolerance
    if (
        not isinstance(endpoint, str)
        or not isinstance(max_response_bytes, int)
        or isinstance(max_response_bytes, bool)
        or not isinstance(webhook_max_body_bytes, int)
        or isinstance(webhook_max_body_bytes, bool)
        or webhook_max_body_bytes < 0
        or not isinstance(webhook_tolerance, (str, int))
        or isinstance(webhook_tolerance, bool)
    ):
        raise ValueError("Stripe connection configuration is invalid")
    config = Config(endpoint, max_response_bytes, webhook_max_body_bytes, webhook_tolerance)
    key = ConnectionKey("stripe", connection_name)
    credentials = _ProjectCredentialProvider(project, key, matches[0].auth_method_id)
    return Client(config, credentials, http_client)
