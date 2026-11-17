# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Signed Typeform responseSubmitted Trigger and durable project endpoint."""

import weakref
from collections.abc import Iterable, Sequence
from dataclasses import dataclass
from typing import Any

import dex
from dex_connectors.sdk import (
    ConnectionRef,
    TriggerBindingRef,
    TriggerCancellation,
    TriggerConfig,
    TriggerContext,
    TriggerLogger,
    TriggerRunner,
    TriggerTarget,
    new_trigger,
)
from dex_connectors.sdk.projectconfig import (
    LoadedProject,
    TriggerInboxKey,
    decode_trigger_configuration,
    new_durable_trigger_target,
)
from dex_connectors.sdk.webhooktrigger import (
    WebhookEndpoint,
    WebhookEndpointConfig,
    WebhookEndpointRunner,
    WebhookRequest,
    WebhookResponse,
)

from ._generated.definitions import CONNECTOR_ID, RESPONSE_SUBMITTED_TRIGGER_DEFINITION
from ._generated.triggers import (
    decode_response_submitted_trigger_configuration,
    define_response_submitted_trigger_binding,
)
from ._webhook import (
    decode_typeform_request,
    validate_response_submitted_configuration,
    verify_typeform_signature,
)
from .client import Client, Credentials
from .connection import Connection, new_project_connection
from .models import FormResponseEvent, ResponseSubmittedTriggerConfiguration

_TRIGGER_NAME = RESPONSE_SUBMITTED_TRIGGER_DEFINITION.trigger.trigger_name
_ENDPOINTS: weakref.WeakKeyDictionary[
    Client, dict[ConnectionRef, WebhookEndpoint[Credentials, FormResponseEvent]]
] = weakref.WeakKeyDictionary()


def _connection_client(connection: Connection) -> Client:
    client = getattr(connection, "_client", None) if connection is not None else None
    if not isinstance(client, Client):
        raise ValueError("Typeform Trigger requires a connection created with a Client")
    return client


def _webhook_endpoint(
    connection: Connection, logger: TriggerLogger | None = None
) -> WebhookEndpoint[Credentials, FormResponseEvent]:
    client = _connection_client(connection)
    by_reference = _ENDPOINTS.setdefault(client, {})
    endpoint = by_reference.get(connection.reference)
    if endpoint is not None:
        return endpoint

    async def verify_request(
        request: WebhookRequest, credentials: Credentials, cancellation: TriggerCancellation
    ) -> None:
        secret = credentials.webhook_secret.reveal() if credentials.webhook_secret is not None else ""
        verify_typeform_signature(request.body, request.header("Typeform-Signature") or "", secret)

    endpoint = WebhookEndpoint(
        WebhookEndpointConfig(
            connector_id=CONNECTOR_ID,
            trigger_name=_TRIGGER_NAME,
            connection=connection.reference,
            credentials=client.credentials,
            verify_request=verify_request,
            decode_event=decode_typeform_request,
            maximum_body_bytes=client.config.webhook_max_body_bytes or 1048576,
            logger=logger,
        )
    )
    by_reference[connection.reference] = endpoint
    return endpoint


def response_submitted_webhook_handler(
    connection: Connection,
) -> WebhookEndpoint[Credentials, FormResponseEvent]:
    """Return the shared ASGI endpoint for this connection's public HTTPS webhook URL."""
    return _webhook_endpoint(connection)


def new_response_submitted_trigger(
    *,
    connection: Connection,
    binding_name: str,
    configuration: ResponseSubmittedTriggerConfiguration,
    target: TriggerTarget[FormResponseEvent],
    logger: TriggerLogger | None = None,
) -> TriggerRunner:
    """Bind a form-filtered source to a target; wrap target durably for HTTP acknowledgment safety."""
    validate_response_submitted_configuration(configuration)
    endpoint = _webhook_endpoint(connection, logger)
    return new_trigger(
        TriggerConfig(
            definition=RESPONSE_SUBMITTED_TRIGGER_DEFINITION,
            binding=TriggerBindingRef(
                connection.reference, RESPONSE_SUBMITTED_TRIGGER_DEFINITION.trigger, binding_name
            ),
            source=endpoint.new_source(
                lambda event: not configuration.form_id or event.payload.form_id == configuration.form_id
            ),
            target=target,
            logger=logger,
        )
    )


@dataclass(frozen=True, slots=True)
class ProjectResponseSubmittedTriggerRoute:
    """One stored binding name and its application target."""

    binding_name: str
    target: TriggerTarget[FormResponseEvent]


class ResponseSubmittedEndpointRunner:
    """Serve and replay all responseSubmitted bindings on one shared endpoint."""

    def __init__(
        self,
        endpoint_runner: WebhookEndpointRunner,
        endpoint: WebhookEndpoint[Credentials, FormResponseEvent],
        connection: Connection,
    ) -> None:
        """Wrap the endpoint and own the opened connection."""
        self._runner = endpoint_runner
        self._endpoint = endpoint
        self._connection = connection

    async def __call__(self, scope: Any, receive: Any, send: Any) -> None:
        """Serve ASGI HTTP and lifespan scopes."""
        await self._runner(scope, receive, send)

    async def handle_request(
        self,
        method: str,
        url: str,
        headers: Iterable[tuple[str, str]],
        body: bytes,
        cancellation: TriggerCancellation | None = None,
    ) -> WebhookResponse:
        """Verify and persist one HTTP request in a non-ASGI host."""
        return await self._runner.handle_request(method, url, headers, body, cancellation)

    async def run(self, context: TriggerContext | None = None) -> None:
        """Replay inboxes and deliver events until cancelled."""
        await self._runner.run(context)

    def running_source_count(self) -> int:
        """Report active bindings for HTTP readiness checks."""
        return self._endpoint.running_source_count()

    async def aclose(self) -> None:
        """Close the connector-owned HTTP transport."""
        await self._connection.aclose()


async def new_project_response_submitted_endpoint_runner(
    project: LoadedProject,
    connection_name: str,
    routes: Sequence[ProjectResponseSubmittedTriggerRoute],
    logger: TriggerLogger | None = None,
) -> ResponseSubmittedEndpointRunner:
    """Open stored bindings and persist every accepted event before acknowledging Typeform."""
    if project is None or not routes:
        raise ValueError("Typeform project and responseSubmitted routes are required")
    connection = new_project_connection(project, connection_name)
    try:
        endpoint = _webhook_endpoint(connection, logger)
        runners: list[TriggerRunner] = []
        binding_names: set[str] = set()
        for route in routes:
            name = route.binding_name.strip()
            if not name or route.target is None or name in binding_names:
                raise ValueError("Typeform responseSubmitted binding name and target must be unique")
            binding_names.add(name)
            key = TriggerInboxKey(CONNECTOR_ID, connection_name, _TRIGGER_NAME, name)
            configuration = decode_trigger_configuration(
                project.configuration, key, decode_response_submitted_trigger_configuration
            )
            validate_response_submitted_configuration(configuration)
            target = new_durable_trigger_target(
                project.trigger_inbox(key), route.target, decode_payload=_decode_pending_event, logger=logger
            )
            runners.append(
                new_response_submitted_trigger(
                    connection=connection,
                    binding_name=name,
                    configuration=configuration,
                    target=target,
                    logger=logger,
                )
            )
        return ResponseSubmittedEndpointRunner(WebhookEndpointRunner(endpoint, runners), endpoint, connection)
    except BaseException:
        await connection.aclose()
        raise


def _decode_pending_event(payload_json: str, payload_bytes: bytes) -> FormResponseEvent:
    decoded = (
        dex.CodecRegistry().resolve(FormResponseEvent).decode(dex.Value(dex.WireKind.JSON, payload_json))
    )
    if not isinstance(decoded, FormResponseEvent):
        raise ValueError("pending Typeform form response is invalid")
    return decoded


__all__ = [
    "ProjectResponseSubmittedTriggerRoute",
    "ResponseSubmittedEndpointRunner",
    "decode_response_submitted_trigger_configuration",
    "define_response_submitted_trigger_binding",
    "new_project_response_submitted_endpoint_runner",
    "new_response_submitted_trigger",
    "response_submitted_webhook_handler",
    "validate_response_submitted_configuration",
]
