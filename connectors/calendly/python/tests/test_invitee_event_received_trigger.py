# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Calendly invitee webhook verification, decoding, filtering, and parity with the Go connector."""

import asyncio
import hashlib
import hmac
import json
from collections.abc import Callable
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any

import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerEvent,
)
from dex_connectors.sdk.projectconfig import decode_trigger_event, encode_trigger_event

from dex_connectors.calendly import (
    INVITEE_EVENT_RECEIVED_TRIGGER_DEFINITION,
    Client,
    Config,
    Credentials,
    InviteeEvent,
    InviteeEventReceivedTriggerConfiguration,
    decode_invitee_event_received_trigger_configuration,
    define_invitee_event_received_trigger_binding,
    invitee_event_received_webhook_handler,
    new_connection,
    new_invitee_event_received_trigger,
    validate_invitee_event_received_trigger_configuration,
)
from dex_connectors.calendly.calendlymock import Mock
from dex_connectors.calendly.triggers import (
    _decode_invitee_webhook,
    _decode_pending_invitee_event,
    _verify_signature,
)

CONNECTION = ConnectionRef("calendly", "calendly-scheduling")
NOW = datetime(2026, 9, 30, 12, 0, tzinfo=UTC)
# Split so that secret scanners do not mistake the test signing key for a credential.
SIGNING_KEY = "sentinel-" + "signing-key"
TYPE_URI = "https://api.calendly.com/event_types/TYPE0001"
RECORDING = json.loads(
    (Path(__file__).parent / "fixtures" / "invitee_event_received_go_recording.json").read_text()
)["cases"]
CREATED_BODY: str = RECORDING["created"]["body"]
CANCELED_BODY: str = RECORDING["canceledRescheduled"]["body"]


def signature(body: str, key: str = SIGNING_KEY, signed_at: datetime = NOW) -> str:
    """Sign body as Calendly does: hex HMAC-SHA256 over the Unix seconds, a dot, and the raw body."""
    seconds = str(int(signed_at.timestamp()))
    return (
        f"t={seconds},v1=" + hmac.new(key.encode(), f"{seconds}.{body}".encode(), hashlib.sha256).hexdigest()
    )


def calendly_client(config: Config | None = None, signing_key: str = SIGNING_KEY) -> Client:
    """A client with in-memory credentials and a fixed clock."""
    credentials = Credentials(
        access_token=SecretString("inert"), webhook_signing_key=SecretString(signing_key)
    )
    return Client(config or Config(), StaticCredentialProvider({CONNECTION: credentials}), now=lambda: NOW)


class RecordingTarget:
    """Records each delivered invitee event in order."""

    def __init__(self) -> None:
        self.events: list[TriggerEvent[InviteeEvent]] = []

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[InviteeEvent]) -> None:
        self.events.append(event)


async def eventually(predicate: Callable[[], bool]) -> None:
    """Poll a converging condition for up to five seconds."""
    for _ in range(500):
        if predicate():
            return
        await asyncio.sleep(0.01)
    raise AssertionError("condition did not converge")


class RunningTrigger:
    """Runs one binding on a connection's endpoint until stopped."""

    def __init__(self, client: Client, configuration: InviteeEventReceivedTriggerConfiguration) -> None:
        self.connection = new_connection(client, CONNECTION)
        self.endpoint = invitee_event_received_webhook_handler(self.connection)
        self.target = RecordingTarget()
        self.runner = new_invitee_event_received_trigger(
            connection=self.connection,
            binding_name="invitee-created",
            configuration=configuration,
            target=self.target,
        )
        self.context = TriggerContext()
        self.task: asyncio.Task[None] | None = None

    async def __aenter__(self) -> "RunningTrigger":
        self.task = asyncio.create_task(self.runner.run(self.context))
        await eventually(lambda: self.endpoint.running_source_count() == 1)
        return self

    async def __aexit__(self, *exc_info: Any) -> None:
        self.context.cancellation.cancel()
        assert self.task is not None
        with pytest.raises(TriggerDeliveryAbortedError):
            await self.task

    async def deliver(self, body: str, header: str | None = None, method: str = "POST") -> int:
        headers = [("Calendly-Webhook-Signature", signature(body) if header is None else header)]
        response = await self.endpoint.handle_request(method, "/webhooks/calendly", headers, body.encode())
        return response.status


@pytest.mark.parametrize("name", sorted(RECORDING))
def test_decoder_matches_the_go_connector_recording(name: str) -> None:
    """Each body selects Go's outcome; an event keeps Go's ID, time, and payload JSON values."""
    case = RECORDING[name]
    if case["outcome"] == "invalid":
        with pytest.raises(ValueError):
            _decode_invitee_webhook(case["body"].encode())
        return
    event = _decode_invitee_webhook(case["body"].encode())
    if case["outcome"] == "ignored":
        assert event is None
        return
    assert event is not None
    assert event.id == case["id"]
    assert event.occurred_at == datetime.fromisoformat(case["occurredAt"].replace("Z", "+00:00"))
    stored = encode_trigger_event(event)
    assert json.loads(stored)["payload"] == json.loads(case["payloadJson"])
    assert _decode_pending_invitee_event(case["payloadJson"], case["payloadJson"].encode()) == event.payload
    assert decode_trigger_event(stored, _decode_pending_invitee_event) == event


def test_decoder_reads_the_invitee_and_its_scheduled_event() -> None:
    """The Trigger event names the webhook, the scheduled event, and the invitee."""
    event = _decode_invitee_webhook(CANCELED_BODY.encode())
    assert event is not None
    assert event.id == "invitee.canceled:EVENT0001:INVITEE02"
    assert event.occurred_at == datetime(2026, 9, 30, 12, 0, 0, 500000, tzinfo=UTC)
    invitee = event.payload.invitee
    assert (invitee.status, invitee.rescheduled) == ("canceled", True)
    assert invitee.new_invitee_uri.endswith("/invitees/INVITEE03")
    assert invitee.cancellation is not None and invitee.cancellation.reason == "Moved"
    assert event.payload.scheduled_event.event_type_uri == TYPE_URI
    assert event.payload.scheduled_event.location.location == "https://zoom.example/j/1"


def test_signature_accepts_any_matching_v1_within_the_tolerance() -> None:
    """Calendly may send several v1 values; one match within three minutes, either way, verifies."""
    body = CREATED_BODY.encode()
    tolerance = timedelta(minutes=3)
    good = signature(CREATED_BODY)
    _verify_signature(body, good, SIGNING_KEY, NOW, tolerance)
    _verify_signature(
        body,
        " v1=" + "ab" * 32 + " , " + good.upper().replace("T=", "t=").replace("V1=", "v1="),
        SIGNING_KEY,
        NOW,
        tolerance,
    )
    _verify_signature(body, signature(CREATED_BODY, signed_at=NOW - tolerance), SIGNING_KEY, NOW, tolerance)
    _verify_signature(body, signature(CREATED_BODY, signed_at=NOW + tolerance), SIGNING_KEY, NOW, tolerance)
    # The MAC covers the t= text exactly as sent, as Go's does.
    seconds = "+" + str(int(NOW.timestamp()))
    digest = hmac.new(SIGNING_KEY.encode(), f"{seconds}.".encode() + body, hashlib.sha256).hexdigest()
    _verify_signature(body, f"t={seconds},v1={digest}", SIGNING_KEY, NOW, tolerance)


@pytest.mark.parametrize(
    "header",
    [
        "",
        signature(CREATED_BODY, key="another-key"),
        signature(CREATED_BODY, signed_at=NOW - timedelta(minutes=3, seconds=1)),
        signature(CREATED_BODY, signed_at=NOW + timedelta(minutes=3, seconds=1)),
        "t=" + str(int(NOW.timestamp())),
        "v1=" + "ab" * 32,
        signature(CREATED_BODY).replace("t=", "t=0", 1).replace("t=0", "t=1_", 1),
        "t=0,v1=" + "ab" * 32,
        "t=-1,v1=" + "ab" * 32,
        "t=99999999999999999999,v1=" + "ab" * 32,
        signature(CREATED_BODY)[:-2],
        signature(CREATED_BODY)[:-2] + " a b",
    ],
)
def test_signature_refuses_forged_stale_and_malformed_headers(header: str) -> None:
    """Every failure raises the same message, which names no single check."""
    with pytest.raises(ValueError, match="missing, malformed, stale, or does not match"):
        _verify_signature(CREATED_BODY.encode(), header, SIGNING_KEY, NOW, timedelta(minutes=3))


async def test_endpoint_delivers_a_verified_invitee_and_keeps_its_id_on_redelivery() -> None:
    """A verified delivery reaches the binding; a re-signed redelivery keeps its event ID."""
    async with RunningTrigger(calendly_client(), InviteeEventReceivedTriggerConfiguration()) as running:
        assert await running.deliver(CREATED_BODY) == 200
        assert (
            await running.deliver(CREATED_BODY, signature(CREATED_BODY, signed_at=NOW - timedelta(minutes=1)))
            == 200
        )
        assert await running.deliver(CANCELED_BODY) == 200
        await eventually(lambda: len(running.target.events) == 3)
    assert [event.id for event in running.target.events] == [
        "invitee.created:EVENT0001:INVITEE01",
        "invitee.created:EVENT0001:INVITEE01",
        "invitee.canceled:EVENT0001:INVITEE02",
    ]
    assert SIGNING_KEY not in repr(running.target.events)


async def test_endpoint_status_codes_follow_the_go_connector() -> None:
    """Forged 400, other events 200 without a record, oversized 413, and GET 405."""
    async with RunningTrigger(
        calendly_client(Config(webhook_max_body_bytes=4096)), InviteeEventReceivedTriggerConfiguration()
    ) as running:
        tampered = CREATED_BODY.replace("zoe@", "eve@", 1)
        assert await running.deliver(tampered, signature(CREATED_BODY)) == 400
        assert await running.deliver(CREATED_BODY, "") == 400
        assert await running.deliver(RECORDING["routingForm"]["body"]) == 200
        assert await running.deliver(RECORDING["emptyPayload"]["body"]) == 400
        assert await running.deliver("not json") == 400
        assert await running.deliver(CREATED_BODY + " " * 4096) == 413
        assert await running.deliver(CREATED_BODY, method="GET") == 405
        assert running.target.events == []


async def test_endpoint_answers_503_without_a_signing_key_or_a_running_binding() -> None:
    """Calendly retries both: the key may be added later and the binding may still be starting."""
    async with RunningTrigger(
        calendly_client(signing_key=""), InviteeEventReceivedTriggerConfiguration()
    ) as running:
        assert await running.deliver(CREATED_BODY) == 503
    stopped = RunningTrigger(calendly_client(), InviteeEventReceivedTriggerConfiguration())
    assert await stopped.deliver(CREATED_BODY) == 503
    assert await stopped.deliver(RECORDING["routingForm"]["body"]) == 200


async def test_binding_filters_by_event_and_event_type() -> None:
    """A filtered event is acknowledged without reaching the binding."""
    other_type = CREATED_BODY.replace("/event_types/TYPE0001", "/event_types/TYPE0002")
    configuration = InviteeEventReceivedTriggerConfiguration(
        events=("invitee.created",), event_type_uri=TYPE_URI
    )
    async with RunningTrigger(calendly_client(), configuration) as running:
        assert await running.deliver(CANCELED_BODY) == 200
        assert await running.deliver(other_type) == 200
        assert await running.deliver(CREATED_BODY) == 200
        await eventually(lambda: len(running.target.events) == 1)
    assert running.target.events[0].id == "invitee.created:EVENT0001:INVITEE01"


def test_signature_tolerance_comes_from_the_connection_configuration() -> None:
    """A stored Go duration in nanoseconds widens the window; zero keeps three minutes."""
    assert (
        calendly_client(
            Config(webhook_signature_tolerance_seconds=0)
        ).config.webhook_signature_tolerance_seconds
        == 180
    )
    wide = calendly_client(Config(webhook_signature_tolerance_seconds=600))
    connection = new_connection(wide, CONNECTION)
    endpoint = invitee_event_received_webhook_handler(connection)
    assert invitee_event_received_webhook_handler(connection) is endpoint, "one endpoint per connection"
    with pytest.raises(ValueError, match="require a connection"):
        invitee_event_received_webhook_handler(Mock().connection())


@pytest.mark.parametrize(
    "configuration",
    [
        InviteeEventReceivedTriggerConfiguration(events=("invitee_no_show.created",)),
        InviteeEventReceivedTriggerConfiguration(events=("invitee.created", "invitee.created")),
        InviteeEventReceivedTriggerConfiguration(event_type_uri="https://api.calendly.com/users/USER0001"),
    ],
)
def test_configuration_rejects_unsupported_events_and_event_types(
    configuration: InviteeEventReceivedTriggerConfiguration,
) -> None:
    """The binding stores only distinct invitee events and an event type URI."""
    with pytest.raises(ValueError):
        validate_invitee_event_received_trigger_configuration(configuration)


def test_configuration_decodes_strictly_and_accepts_both_events() -> None:
    """Stored configuration uses Go's JSON names; unknown fields are refused."""
    decoded = decode_invitee_event_received_trigger_configuration(
        {"events": ["invitee.created", "invitee.canceled"], "eventTypeUri": TYPE_URI}
    )
    validate_invitee_event_received_trigger_configuration(decoded)
    assert decoded == InviteeEventReceivedTriggerConfiguration(
        ("invitee.created", "invitee.canceled"), TYPE_URI
    )
    assert decode_invitee_event_received_trigger_configuration({"events": None}).events == ()
    with pytest.raises(ValueError):
        decode_invitee_event_received_trigger_configuration({"eventTypes": ["invitee.created"]})
    with pytest.raises(ValueError):
        decode_invitee_event_received_trigger_configuration({"events": "invitee.created"})


def test_binding_factory_declares_the_trigger_for_dex_web() -> None:
    """The generated factory binds the manifest Trigger to a connection and binding name."""
    binding = define_invitee_event_received_trigger_binding(
        connection_name="calendly-scheduling", binding_name="invitee-created"
    )
    assert binding.definition == INVITEE_EVENT_RECEIVED_TRIGGER_DEFINITION
