# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify Stripe Checkout webhook signatures, filters, and durable replay."""

import asyncio
import hashlib
import hmac
import json
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
from datetime import UTC, datetime, timedelta
from pathlib import Path

import pytest
from dex_connectors.sdk import TriggerCancellation, TriggerContext, TriggerDeliveryAbortedError, TriggerEvent
from dex_connectors.sdk.projectconfig import TriggerInboxKey, load_from_environment

from dex_connectors.stripe import (
    ProjectCheckoutSessionUpdatedTriggerRoute,
    new_project_checkout_session_webhook_runtime,
    new_project_connection,
)

CONNECTION_NAME = "stripe-payments"
BINDING_NAME = "checkout-events"
KEY = TriggerInboxKey("stripe", CONNECTION_NAME, "checkoutSessionUpdated", BINDING_NAME)
SECRET = "whsec_inert_" + "signing_secret"


class CollectingTarget:
    """Collect decoded Checkout Session events."""

    def __init__(self) -> None:
        self.events: asyncio.Queue[TriggerEvent[object]] = asyncio.Queue()

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[object]) -> None:
        """Queue the event after durable preparation."""
        await self.events.put(event)


async def load_project(directory: Path, configuration: dict[str, object] | None = None, secret: str = SECRET):
    """Load inert Stripe credentials and a static Checkout Session binding."""
    path = directory / "connections.json"
    path.write_text(
        json.dumps(
            {
                "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                "connections": [
                    {
                        "connectorId": "stripe",
                        "connectionName": CONNECTION_NAME,
                        "modulePath": "github.com/superdurable/dex-connectors-library/connectors/stripe",
                        "provider": "stripe",
                        "authMethodId": "api-key-with-webhook",
                        "configuration": {"webhookMaxBodyBytes": 4096},
                        "credentials": {
                            "secret_key": "sk_test_inert",
                            **({"webhook_secret": secret} if secret else {}),
                        },
                    }
                ],
                "triggerBindings": [
                    {
                        "connectorId": "stripe",
                        "connectionName": CONNECTION_NAME,
                        "triggerName": "checkoutSessionUpdated",
                        "bindingName": BINDING_NAME,
                        "configuration": configuration or {},
                    }
                ],
            }
        )
    )
    path.chmod(0o600)
    return await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": str(path)})


@asynccontextmanager
async def running(
    directory: Path, target: object, configuration: dict[str, object] | None = None, secret: str = SECRET
) -> AsyncIterator:
    project = await load_project(directory, configuration, secret)
    connection = new_project_connection(project, CONNECTION_NAME)
    runtime = new_project_checkout_session_webhook_runtime(
        project, connection, [ProjectCheckoutSessionUpdatedTriggerRoute(BINDING_NAME, target)]
    )
    cancellation = TriggerCancellation()
    task = asyncio.create_task(runtime.run(TriggerContext(cancellation)))
    try:

        async def ready() -> None:
            while runtime.running_source_count() != 1:
                if task.done():
                    await task
                await asyncio.sleep(0.01)

        await asyncio.wait_for(ready(), 5)
        yield runtime, project
    finally:
        cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task
        await connection.aclose()
        await project.close()


def body(event_type: str = "checkout.session.completed", event_id: str = "evt_123") -> bytes:
    """Encode a Stripe event carrying only safe Checkout Session fields."""
    return json.dumps(
        {
            "id": event_id,
            "object": "event",
            "type": event_type,
            "created": 1700000000,
            "data": {
                "object": {
                    "id": "cs_test_123",
                    "object": "checkout.session",
                    "client_reference_id": "registration-123",
                    "payment_status": "paid",
                    "status": "complete",
                    "payment_intent": {"id": "pi_123"},
                    "currency": "usd",
                    "amount_total": 7500,
                    "metadata": {"registration_id": "registration-123"},
                    "customer_email": "sentinel@example.com",
                    "client_secret": "SENTINEL_SECRET",
                }
            },
        }
    ).encode()


def signed(payload: bytes, key: str = SECRET, timestamp: int | None = None) -> list[tuple[str, str]]:
    """Sign the raw bytes using Stripe's timestamp and v1 HMAC format."""
    seconds = timestamp if timestamp is not None else int(datetime.now(UTC).timestamp())
    signature = hmac.new(key.encode(), f"{seconds}.".encode() + payload, hashlib.sha256).hexdigest()
    return [("Stripe-Signature", f"t={seconds},v0=ignored,v1={'0' * 64},v1={signature}")]


@pytest.mark.asyncio
async def test_signed_event_and_filter(tmp_path: Path) -> None:
    target = CollectingTarget()
    async with running(tmp_path, target, {"eventTypes": ["checkout.session.completed"]}) as (
        runtime,
        project,
    ):
        payload = body()
        assert (
            await runtime.handle_request("POST", "/stripe/events", signed(payload), payload)
        ).status == 200
        event = await asyncio.wait_for(target.events.get(), 5)
        assert event.id == "evt_123"
        assert event.payload.type == "checkout.session.completed"
        assert event.payload.session.id == "cs_test_123"
        assert event.payload.session.payment_intent_id == "pi_123"
        assert event.payload.session.metadata == {"registration_id": "registration-123"}
        assert "sentinel@example.com" not in repr(event.payload)
        assert "SENTINEL_SECRET" not in repr(event.payload)
        ignored = body("checkout.session.expired", "evt_ignored")
        assert (
            await runtime.handle_request("POST", "/stripe/events", signed(ignored), ignored)
        ).status == 200
        other = body("payment_intent.succeeded", "evt_other")
        assert (await runtime.handle_request("POST", "/stripe/events", signed(other), other)).status == 200
        assert target.events.empty()
        assert await project.trigger_inbox(KEY).pending() == ()


@pytest.mark.asyncio
async def test_signature_age_body_and_method_rejections(tmp_path: Path) -> None:
    async with running(tmp_path, CollectingTarget()) as (runtime, _):
        payload = body()
        assert (await runtime.handle_request("GET", "/stripe/events", [], b"")).status == 405
        assert (await runtime.handle_request("POST", "/stripe/events", [], payload)).status == 400
        assert (
            await runtime.handle_request("POST", "/stripe/events", signed(payload, "wrong"), payload)
        ).status == 400
        assert (
            await runtime.handle_request("POST", "/stripe/events", signed(payload), payload + b" ")
        ).status == 400
        stale = int((datetime.now(UTC) - timedelta(minutes=6)).timestamp())
        future = int((datetime.now(UTC) + timedelta(minutes=6)).timestamp())
        for timestamp in (stale, future):
            assert (
                await runtime.handle_request(
                    "POST", "/stripe/events", signed(payload, timestamp=timestamp), payload
                )
            ).status == 400
        oversized = b"x" * 5000
        assert (await runtime.handle_request("POST", "/stripe/events", [], oversized)).status == 413
        malformed = json.dumps(
            {
                "id": "evt_bad",
                "object": "event",
                "type": "checkout.session.completed",
                "created": 1,
                "data": {"object": {"id": "bad"}},
            }
        ).encode()
        assert (
            await runtime.handle_request("POST", "/stripe/events", signed(malformed), malformed)
        ).status == 400


@pytest.mark.asyncio
async def test_restart_replays_an_acknowledged_event(tmp_path: Path) -> None:
    class FailingTarget:
        async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[object]) -> None:
            raise RuntimeError("target unavailable")

    payload = body(event_id="evt_restart")
    async with running(tmp_path, FailingTarget()) as (runtime, project):
        assert (
            await runtime.handle_request("POST", "/stripe/events", signed(payload), payload)
        ).status == 200
        assert len(await project.trigger_inbox(KEY).pending()) == 1
    target = CollectingTarget()
    async with running(tmp_path, target):
        event = await asyncio.wait_for(target.events.get(), 5)
    assert event.id == "evt_restart"
    assert event.payload.session.client_reference_id == "registration-123"


@pytest.mark.asyncio
async def test_unavailable_binding_and_secret(tmp_path: Path) -> None:
    project = await load_project(tmp_path)
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        runtime = new_project_checkout_session_webhook_runtime(
            project, connection, [ProjectCheckoutSessionUpdatedTriggerRoute(BINDING_NAME, CollectingTarget())]
        )
        payload = body()
        assert (
            await runtime.handle_request("POST", "/stripe/events", signed(payload), payload)
        ).status == 503
    finally:
        await connection.aclose()
        await project.close()


@pytest.mark.asyncio
async def test_missing_signing_secret_requests_retry(tmp_path: Path) -> None:
    """An API-key-only connection cannot verify webhooks and answers 503."""
    async with running(tmp_path, CollectingTarget(), secret="") as (runtime, _):
        payload = body()
        assert (
            await runtime.handle_request("POST", "/stripe/events", signed(payload), payload)
        ).status == 503


def test_configuration_and_duration_validation() -> None:
    """Reject unsupported event lists and invalid or nonpositive replay windows."""
    from dex_connectors.stripe import (
        CheckoutSessionUpdatedTriggerConfiguration,
        validate_checkout_session_updated_trigger_configuration,
    )
    from dex_connectors.stripe.triggers import _duration_seconds

    for selected in (("charge.succeeded",), ("checkout.session.completed", "checkout.session.completed")):
        with pytest.raises(ValueError):
            validate_checkout_session_updated_trigger_configuration(
                CheckoutSessionUpdatedTriggerConfiguration(selected)
            )
    assert _duration_seconds("5m") == 300
    assert _duration_seconds("1m30s") == 90
    assert _duration_seconds(300_000_000_000) == 300
    for duration in ("", "0s", "bad", "-5m", 0, -1):
        with pytest.raises(ValueError):
            _duration_seconds(duration)


def test_checkout_example_configuration_guidance() -> None:
    """Audit the visible authorization fields, defaults, and static binding UI."""
    import yaml

    from examples.checkout_session_event.flow import CheckoutEventFlow

    manifest_path = Path(__file__).resolve().parents[2] / "connector.yaml"
    manifest = yaml.safe_load(manifest_path.read_text())
    configuration = manifest["spec"]["configuration"]["fields"]
    assert {field["name"] for field in configuration} == {
        "endpoint",
        "maxResponseBytes",
        "webhookMaxBodyBytes",
        "webhookSignatureTolerance",
    }
    for field in configuration:
        assert field["default"] is not None
        assert "Blank" in field["description"] or "blank" in field["description"]
    methods = {method["id"]: method for method in manifest["spec"]["auth"]["methods"]}
    webhook_method = methods["api-key-with-webhook"]
    assert webhook_method["guide"]["startURL"] == "https://dashboard.stripe.com/apikeys"
    assert {field["name"] for field in webhook_method["fields"]} == {"secret_key", "webhook_secret"}
    for field in webhook_method["fields"]:
        description = field["description"]
        assert "https://dashboard.stripe.com/" in description
        assert "Secret" in description and "blank" in description
    assert "checkout.session.expired" in " ".join(webhook_method["guide"]["steps"])
    binding = CheckoutEventFlow.__new__(CheckoutEventFlow).get_connector_trigger_bindings()[0]
    assert binding.connection_name == CONNECTION_NAME
    assert binding.binding_name == BINDING_NAME
    assert binding.configuration_ui is None, "the binding accepts all four supported events by default"
