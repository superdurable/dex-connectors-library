# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Port the Go Connect endpoint and project runner tests, plus durable replay of Go-recorded events."""

import asyncio
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
from datetime import UTC, datetime
from pathlib import Path

import pytest
from dex_connectors.sdk import TriggerCancellation, TriggerContext, TriggerDeliveryAbortedError
from dex_connectors.sdk.projectconfig import ObjectNotFoundError, PendingTriggerEvent, TriggerInboxKey

from dex_connectors.docusign import (
    ENVELOPE_EVENT_RECEIVED_TRIGGER_DEFINITION,
    EnvelopeCustomField,
    EnvelopeEvent,
    EnvelopeEventReceivedEndpointRunner,
    EnvelopeEventReceivedTriggerConfiguration,
    ProjectEnvelopeEventReceivedTriggerRoute,
    define_envelope_event_received_trigger_binding,
    envelope_event_received_webhook_endpoint,
    new_envelope_event_received_trigger,
    new_project_connection,
    new_project_envelope_event_received_endpoint_runner,
)
from dex_connectors.docusign.docusignmock import Mock
from tests.connect_fixture import (
    BINDING_NAME,
    CONNECTION_NAME,
    TEST_ACCOUNT_ID,
    TEST_ENVELOPE_ID,
    CollectingTarget,
    connect_body,
    load_project,
    signed,
)

KEY = TriggerInboxKey("docusign", CONNECTION_NAME, "envelopeEventReceived", BINDING_NAME)


@asynccontextmanager
async def running(runner: EnvelopeEventReceivedEndpointRunner) -> AsyncIterator[None]:
    cancellation = TriggerCancellation()
    task = asyncio.create_task(runner.run(TriggerContext(cancellation)))
    try:
        for _ in range(500):
            if runner.running_source_count() == 1 or task.done():
                break
            await asyncio.sleep(0.01)
        assert runner.running_source_count() == 1
        yield
    finally:
        cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task


@asynccontextmanager
async def outcomes_runner(
    directory: Path, configuration: dict[str, object], target: CollectingTarget, hmac_key: str | None = None
) -> AsyncIterator[EnvelopeEventReceivedEndpointRunner]:
    project = await (
        load_project(directory, {BINDING_NAME: configuration})
        if hmac_key is None
        else load_project(directory, {BINDING_NAME: configuration}, hmac_key)
    )
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        runner = new_project_envelope_event_received_endpoint_runner(
            project, connection, [ProjectEnvelopeEventReceivedTriggerRoute(BINDING_NAME, target)]
        )
        async with running(runner):
            yield runner
    finally:
        await connection.aclose()
        await project.close()


async def deliver(
    runner: EnvelopeEventReceivedEndpointRunner, body: str, headers: list[tuple[str, str]]
) -> int:
    response = await runner.handle_request("POST", "/docusign/connect", headers, body.encode())
    return response.status


async def test_records_a_verified_envelope_outcome(tmp_path: Path) -> None:
    target = CollectingTarget()
    async with outcomes_runner(tmp_path, {}, target) as runner:
        body = connect_body("envelope-voided", TEST_ENVELOPE_ID.upper(), "opp-123")
        # DocuSign sends one header per account key; the connection's key may be the second one.
        headers = signed(body, "another-account-key") + signed(body, name="X-DocuSign-Signature-2")[1:]
        assert await deliver(runner, body, headers) == 200
        event = await target.next()
    assert event.id == TEST_ENVELOPE_ID + ":envelope-voided"
    assert event.occurred_at == datetime(2026, 10, 4, 13, 0, 0, 123000, tzinfo=UTC)
    assert event.payload == EnvelopeEvent(
        "envelope-voided",
        TEST_ENVELOPE_ID,
        TEST_ACCOUNT_ID,
        event.occurred_at,
        "voided",
        "Signer left the company",
        (EnvelopeCustomField("dexSigningRequestId", "opp-123"),),
    )


async def test_rejects_forgeries_and_acknowledges_other_events(tmp_path: Path) -> None:
    target = CollectingTarget()
    async with outcomes_runner(tmp_path, {"events": ["envelope-completed"]}, target) as runner:
        completed = connect_body("envelope-completed", TEST_ENVELOPE_ID, "opp-123")
        assert await deliver(runner, completed, []) == 400, "an unsigned delivery is refused"
        assert await deliver(runner, completed, signed(completed, "not-the-key")) == 400
        tampered = completed.replace("opp-123", "opp-999")
        assert await deliver(runner, tampered, signed(completed)) == 400, "a tampered body is refused"
        unnumbered = signed(completed, name="X-DocuSign-Signature")
        assert await deliver(runner, completed, unnumbered) == 400, "only numbered signature headers count"

        recipient = completed.replace('"event":"envelope-completed"', '"event":"recipient-completed"')
        assert await deliver(runner, recipient, signed(recipient)) == 200, (
            "an unhandled event is acknowledged"
        )
        declined = connect_body("envelope-declined", TEST_ENVELOPE_ID, "opp-123")
        assert await deliver(runner, declined, signed(declined)) == 200, "a filtered event is acknowledged"
        xml_body = "<DocuSignEnvelopeInformation/>"
        assert await deliver(runner, xml_body, signed(xml_body)) == 400, (
            "a legacy XML configuration is refused"
        )
        assert await deliver(runner, " " * 5000 + completed, []) == 413

        assert await deliver(runner, completed, signed(completed)) == 200
        assert (await target.next()).id == TEST_ENVELOPE_ID + ":envelope-completed"
        assert target.events.empty()


async def test_answers_retryable_without_an_hmac_key(tmp_path: Path) -> None:
    async with outcomes_runner(tmp_path, {}, CollectingTarget(), hmac_key="") as runner:
        body = connect_body("envelope-completed", TEST_ENVELOPE_ID, "opp-123")
        assert await deliver(runner, body, signed(body, "")) == 503
    async with outcomes_runner(tmp_path, {}, CollectingTarget(), hmac_key='""') as runner:
        assert await deliver(runner, body, signed(body, "")) == 503, "a key of only quotes is blank"


async def test_strips_quotes_from_the_hmac_key(tmp_path: Path) -> None:
    target = CollectingTarget()
    async with outcomes_runner(tmp_path, {}, target, hmac_key='"quoted-key"') as runner:
        body = connect_body("envelope-completed", TEST_ENVELOPE_ID, "opp-123")
        assert await deliver(runner, body, signed(body, "quoted-key")) == 200
        assert (await target.next()).payload.event == "envelope-completed"


async def test_accepts_an_event_without_envelope_data(tmp_path: Path) -> None:
    target = CollectingTarget()
    async with outcomes_runner(tmp_path, {}, target) as runner:
        body = (
            '{"event":"envelope-completed","generatedDateTime":"","data":{"accountId":"'
            + TEST_ACCOUNT_ID
            + '","envelopeId":"'
            + TEST_ENVELOPE_ID
            + '","envelopeSummary":""}}'
        )
        assert await deliver(runner, body, signed(body)) == 200
        event = await target.next()
    assert event.payload.custom_fields == () and event.payload.status is None
    assert event.occurred_at.tzinfo is not None, "a blank generation time falls back to the receipt time"


async def test_answers_503_while_no_binding_runs(tmp_path: Path) -> None:
    project = await load_project(tmp_path, {BINDING_NAME: {}})
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        runner = new_project_envelope_event_received_endpoint_runner(
            project, connection, [ProjectEnvelopeEventReceivedTriggerRoute(BINDING_NAME, CollectingTarget())]
        )
        body = connect_body("envelope-completed", TEST_ENVELOPE_ID, "opp-123")
        assert await deliver(runner, body, signed(body)) == 503
        assert runner.running_source_count() == 0
    finally:
        await connection.aclose()
        await project.close()


async def test_replays_an_acknowledged_event_after_a_restart(tmp_path: Path) -> None:
    body = connect_body("envelope-completed", TEST_ENVELOPE_ID, "opp-123")
    async with outcomes_runner(tmp_path, {}, CollectingTarget(failures=1_000)) as runner:
        assert await deliver(runner, body, signed(body)) == 200, "the inbox stores the event before 200"
    replayed = CollectingTarget()
    async with outcomes_runner(tmp_path, {}, replayed):
        event = await replayed.next()
    assert event.id == TEST_ENVELOPE_ID + ":envelope-completed"
    assert event.payload.custom_fields == (EnvelopeCustomField("dexSigningRequestId", "opp-123"),)


async def test_replays_an_event_a_go_replica_recorded(tmp_path: Path) -> None:
    # Go's json.Marshal of the EnvelopeEvent, field order and a nanosecond generation time included.
    go_payload = (
        '{"event":"envelope-voided","envelopeId":"'
        + TEST_ENVELOPE_ID
        + '","accountId":"'
        + TEST_ACCOUNT_ID
        + '","generatedAt":"2026-10-04T13:00:00.1234567Z","status":"voided","voidedReason":"Signer left",'
        '"customFields":[{"name":"dexSigningRequestId","value":"opp-123"}]}'
    )
    project = await load_project(tmp_path, {BINDING_NAME: {}})
    inbox = project.trigger_inbox(KEY)
    event_id = TEST_ENVELOPE_ID + ":envelope-voided"
    stored = (
        '{"eventId":"'
        + event_id
        + '","occurredAt":"2026-10-04T13:00:00.1234567Z","payload":'
        + go_payload
        + "}"
    )
    await inbox.add(PendingTriggerEvent(event_id, stored.encode()))
    await project.close()
    replayed = CollectingTarget()
    async with outcomes_runner(tmp_path, {}, replayed):
        event = await replayed.next()
    assert event.payload.voided_reason == "Signer left"
    assert event.payload.generated_at == datetime(2026, 10, 4, 13, 0, 0, 123456, tzinfo=UTC)


async def test_requires_a_valid_binding_in_the_project_configuration(tmp_path: Path) -> None:
    routes = [ProjectEnvelopeEventReceivedTriggerRoute(BINDING_NAME, CollectingTarget())]
    for bindings, routes_used, error, match in (
        ({}, routes, ObjectNotFoundError, None),
        ({BINDING_NAME: {"events": ["envelope-sent"]}}, routes, ValueError, 'binding "outcomes"'),
        ({BINDING_NAME: {"event": []}}, routes, ValueError, 'binding "outcomes" configuration'),
        ({BINDING_NAME: {}}, routes + routes, ValueError, "duplicated"),
        ({BINDING_NAME: {}}, [], ValueError, "routes are required"),
        (
            {BINDING_NAME: {}},
            [ProjectEnvelopeEventReceivedTriggerRoute(" ", CollectingTarget())],
            ValueError,
            "name",
        ),
    ):
        directory = tmp_path / str(len(list(tmp_path.iterdir())))
        directory.mkdir()
        project = await load_project(directory, bindings)
        connection = new_project_connection(project, CONNECTION_NAME)
        try:
            with pytest.raises(error, match=match):
                new_project_envelope_event_received_endpoint_runner(project, connection, routes_used)
        finally:
            await connection.aclose()
            await project.close()


async def test_builds_a_standalone_trigger_and_the_static_binding_definition(tmp_path: Path) -> None:
    project = await load_project(tmp_path, {})
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        assert envelope_event_received_webhook_endpoint(
            connection
        ) is envelope_event_received_webhook_endpoint(connection), (
            "every binding of a connection shares one endpoint"
        )
        new_envelope_event_received_trigger(
            connection=connection,
            binding_name=BINDING_NAME,
            configuration=EnvelopeEventReceivedTriggerConfiguration(),
            target=CollectingTarget(),
        )
        with pytest.raises(ValueError, match="distinct"):
            new_envelope_event_received_trigger(
                connection=connection,
                binding_name=BINDING_NAME,
                configuration=EnvelopeEventReceivedTriggerConfiguration(("envelope-sent",)),
                target=CollectingTarget(),
            )
        with pytest.raises(ValueError, match="mock connection"):
            envelope_event_received_webhook_endpoint(Mock(CONNECTION_NAME).connection())
    finally:
        await connection.aclose()
        await project.close()
    binding = define_envelope_event_received_trigger_binding(
        connection_name=CONNECTION_NAME, binding_name="o"
    )
    assert binding.definition is ENVELOPE_EVENT_RECEIVED_TRIGGER_DEFINITION
    assert ENVELOPE_EVENT_RECEIVED_TRIGGER_DEFINITION.trigger.trigger_name == "envelopeEventReceived"
