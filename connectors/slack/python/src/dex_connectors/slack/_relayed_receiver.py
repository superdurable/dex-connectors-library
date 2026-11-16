# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Receive the Events API callbacks a Dex host relays for one shared connection."""

from __future__ import annotations

import asyncio
import json
from datetime import timedelta
from typing import Any, Final
from uuid import uuid4

from dex_connectors.sdk import (
    CallID,
    ConnectionRef,
    StaticCredentialProvider,
    TriggerCancellation,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerEvent,
    TriggerLogLevel,
    prepare_trigger_delivery,
)
from dex_connectors.sdk.projectconfig import (
    ConnectionKey,
    ConnectionNotSelectedError,
    CredentialBrokerUnavailableError,
    LoadedProject,
    RelayedTriggerNotRelayedError,
    RelayTokenRenewalDeferredError,
)
from dex_connectors.sdk.webhooktrigger import (
    WebhookEndpoint,
    WebhookEndpointConfig,
    WebhookRequest,
    WebhookVerificationUnavailableError,
)

from ._generated.definitions import CONNECTOR_ID
from ._socket_mode_runner import replay_routes, until_cancelled, wait_for_delay
from ._trigger_sources import (
    BindingIdentity,
    MessageRoute,
    PendingMessageDelivery,
    SocketEnvelope,
    decode_message_deliveries,
    deliver_message_deliveries,
)

RELAYED_TRIGGER_PATH_PREFIX: Final = "/triggers/slack/"
# The host's registration endpoint on its credential broker's origin.
_RELAYED_TRIGGER_REGISTRATION_PATH: Final = "/internal/slack-triggers/register"
# Bound of one relayed Events API callback, as the host bounds what Slack sends it.
_MAXIMUM_RELAYED_EVENT_BYTES: Final = 1 << 20
# Stands in for the Socket Mode envelope ID, which a relayed event lacks.
_RELAYED_ENVELOPE_ID: Final = "relayed"
_RELAYED_SOURCE_START_POLL: Final = timedelta(milliseconds=10)

_RelayedEvent = list[PendingMessageDelivery]


class TerminalRelayError(ValueError):
    """A refusal that no credential retry can change."""


class _RelayedDeliveryTarget:
    """Records a relayed event in every matching route's inbox, then delivers it inline in route order."""

    def __init__(self, routes: list[MessageRoute]) -> None:
        self._routes = routes

    async def prepare_trigger(self, context: TriggerContext, event: TriggerEvent[_RelayedEvent]) -> None:
        for delivery in event.payload:
            await prepare_trigger_delivery(context, delivery.target, delivery.event)

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[_RelayedEvent]) -> None:
        await deliver_message_deliveries(context, event.payload)

    async def replay_trigger_deliveries(self, context: TriggerContext) -> None:
        await replay_routes(context, [route.target for route in self._routes])


class RelayedMessageReceiver:
    """Receives one connection's relayed Events API callbacks and delivers them to the message routes."""

    def __init__(self, project: LoadedProject, identity: BindingIdentity, routes: list[MessageRoute]) -> None:
        self._project = project
        self._identity = identity
        self._routes = routes
        self._connection: ConnectionRef = identity.connection
        self._call_id = CallID(str(uuid4()))
        credentials: StaticCredentialProvider[dict[str, Any]] = StaticCredentialProvider(
            {self._connection: {}}
        )
        self.endpoint: WebhookEndpoint[dict[str, Any], _RelayedEvent] = WebhookEndpoint(
            WebhookEndpointConfig(
                connector_id=CONNECTOR_ID,
                trigger_name="relayedMessage",
                connection=self._connection,
                # The relay signature uses the scope token, so the public route reads no Slack credential.
                credentials=credentials,
                verify_request=self._verify_request,
                decode_event=self._decode_event,
                maximum_body_bytes=_MAXIMUM_RELAYED_EVENT_BYTES,
                clock=identity.client.now,
                logger=identity.options.logger,
            )
        )
        self._source = self.endpoint.new_source()

    @property
    def connection_name(self) -> str:
        """The relayed connection's name."""
        return self._connection.name

    async def select_relayed_mode(self, context: TriggerContext) -> bool:
        """Read the credential, retrying while unavailable; report whether events arrive relayed."""
        failures = 0
        while True:
            failures += 1
            try:
                credentials = await until_cancelled(
                    self._identity.client.resolve_trigger_credentials(
                        self._identity.credential_call(self._call_id)
                    ),
                    context.cancellation,
                )
                if credentials.app_token is not None and credentials.app_token.reveal():
                    return False
                if not self._project.shared_connection_id(ConnectionKey(CONNECTOR_ID, self.connection_name)):
                    raise TerminalRelayError(
                        f'Slack connection "{self.connection_name}" has no app_token for Socket Mode and is '
                        "not a shared connection whose host relays its events"
                    )
                return True
            except (TerminalRelayError, TriggerDeliveryAbortedError):
                raise
            except ConnectionNotSelectedError as error:
                raise ValueError(f'Slack connection "{self.connection_name}": {error}') from error
            except Exception:  # noqa: BLE001
                context.cancellation.raise_if_cancelled()
            delay = self._identity.reconnect_backoff(failures)
            # A credential provider's error may quote what it read, so only its absence is reported.
            self._identity.log_connection(
                context,
                TriggerLogLevel.WARN,
                "slack trigger credentials unavailable; retrying",
                {"attempt": failures, "delay_ms": delay // timedelta(milliseconds=1)},
            )
            await wait_for_delay(context.cancellation, delay)

    async def run(self, context: TriggerContext) -> None:
        """Replay the inboxes, receive relayed events, and register the connection once the source runs."""
        await replay_routes(context, [route.target for route in self._routes])
        run_cancellation = TriggerCancellation(context.cancellation)
        run_context = TriggerContext(run_cancellation).with_log_attributes(context.log_attributes)
        source_task = asyncio.ensure_future(
            self._source.run(run_context, _RelayedDeliveryTarget(self._routes))
        )
        try:
            await self._register(run_context, source_task)
        except BaseException:
            run_cancellation.cancel()
            try:
                await source_task
            except BaseException:  # noqa: BLE001
                pass  # The registration failure is the error to report.
            if context.cancellation.is_cancelled:
                raise TriggerDeliveryAbortedError() from None
            raise
        await source_task

    async def _register(self, context: TriggerContext, source_task: asyncio.Future[None]) -> None:
        """Wait until the source runs, then register, retrying while the host is unavailable."""
        while self.endpoint.running_source_count() == 0:
            _raise_if_source_stopped(source_task)
            await wait_for_delay(context.cancellation, _RELAYED_SOURCE_START_POLL)
        key = ConnectionKey(CONNECTOR_ID, self.connection_name)
        failures = 0
        while True:
            failures += 1
            try:
                await until_cancelled(
                    self._project.register_relayed_trigger(key, _RELAYED_TRIGGER_REGISTRATION_PATH),
                    context.cancellation,
                )
                self._identity.log_connection(
                    context, TriggerLogLevel.INFO, "slack relayed trigger registered"
                )
                return
            except TriggerDeliveryAbortedError:
                raise
            except RelayedTriggerNotRelayedError as error:
                raise ValueError(
                    f'Slack connection "{self.connection_name}" has no app_token for Socket Mode and its '
                    "host does not relay its events"
                ) from error
            except CredentialBrokerUnavailableError as error:
                delay = self._identity.reconnect_backoff(failures)
                self._identity.log_connection(
                    context,
                    TriggerLogLevel.WARN,
                    "slack relayed trigger registration failed; retrying",
                    {
                        "attempt": failures,
                        "delay_ms": delay // timedelta(milliseconds=1),
                        "error": str(error),
                    },
                )
            except Exception as error:
                raise ValueError(
                    f'register Slack relayed trigger for connection "{self.connection_name}": {error}'
                ) from error
            await wait_for_delay(context.cancellation, delay)
            _raise_if_source_stopped(source_task)

    async def _verify_request(
        self, request: WebhookRequest, credentials: dict[str, Any], cancellation: TriggerCancellation
    ) -> None:
        """Check the host's signature; a token that cannot be read is retryable for the host."""
        try:
            await self._project.verify_relayed_request(request.headers, request.body, request.received_at)
        except Exception as error:
            # The route is public, so a bad signature is routine; only the application's own failure warns.
            # A mismatch before the token can be reread is retryable, so a rotated token passes later.
            is_deferred = isinstance(error, RelayTokenRenewalDeferredError)
            is_unavailable = isinstance(error, CredentialBrokerUnavailableError)
            reason = (
                "token_renewal_deferred"
                if is_deferred
                else "verification_unavailable"
                if is_unavailable
                else "invalid_signature"
            )
            level = TriggerLogLevel.WARN if is_unavailable and not is_deferred else TriggerLogLevel.DEBUG
            self._identity.log_connection(
                TriggerContext(), level, "slack relayed request refused", {"reason": reason}
            )
            if is_unavailable:
                raise WebhookVerificationUnavailableError(error) from error
            raise

    def _decode_event(self, request: WebhookRequest) -> TriggerEvent[_RelayedEvent] | None:
        """Match a relayed Events API callback, shaped like a Socket Mode payload, against the routes."""
        payload = json.loads(request.body)
        envelope = SocketEnvelope(_RELAYED_ENVELOPE_ID, "events_api", payload, True, "")
        deliveries = decode_message_deliveries(TriggerContext(), self._routes, envelope)
        if not deliveries:
            return None
        return TriggerEvent(deliveries[0].event.id, deliveries[0].event.occurred_at, deliveries)


def _raise_if_source_stopped(source_task: asyncio.Future[None]) -> None:
    if not source_task.done():
        return
    error = source_task.exception()
    raise error if error is not None else TriggerDeliveryAbortedError()
