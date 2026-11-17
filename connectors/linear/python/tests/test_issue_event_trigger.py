# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify Linear Issue webhook signing, freshness, filtering, and durable restart replay."""

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

from dex_connectors.linear import (
    ProjectIssueEventReceivedTriggerRoute,
    new_project_connection,
    new_project_issue_event_received_endpoint_runner,
)

CONNECTION_NAME = "linear-workspace"
BINDING_NAME = "issue-events"
KEY = TriggerInboxKey("linear", CONNECTION_NAME, "issueEventReceived", BINDING_NAME)
SECRET = "inert-linear-" + "webhook-signing-secret"
ISSUE_ID = "2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4"
TEAM_ID = "7f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4"
STATE_ID = "8f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4"
OTHER_TEAM_ID = "9f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4"


class CollectingTarget:
    """Queue delivered Issue events for assertions."""

    def __init__(self) -> None:
        self.events: asyncio.Queue[TriggerEvent[object]] = asyncio.Queue()

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[object]) -> None:
        """Collect one event."""
        await self.events.put(event)


async def load_project(directory: Path, configuration: dict[str, object] | None = None, secret: str = SECRET):
    """Load one inert Linear connection and issueEventReceived binding."""
    path = directory / "connections.json"
    path.write_text(
        json.dumps(
            {
                "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                "connections": [
                    {
                        "connectorId": "linear",
                        "connectionName": CONNECTION_NAME,
                        "modulePath": "github.com/superdurable/dex-connectors-library/connectors/linear",
                        "provider": "linear",
                        "configuration": {},
                        "authMethodId": "personal-api-key",
                        "credentials": {"api_key": "lin_api_inert_key", "webhook_signing_secret": secret},
                    }
                ],
                "triggerBindings": [
                    {
                        "connectorId": "linear",
                        "connectionName": CONNECTION_NAME,
                        "triggerName": "issueEventReceived",
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
    runner = new_project_issue_event_received_endpoint_runner(
        project, connection, [ProjectIssueEventReceivedTriggerRoute(BINDING_NAME, target)]
    )
    cancellation = TriggerCancellation()
    task = asyncio.create_task(runner.run(TriggerContext(cancellation)))
    try:

        async def ready() -> None:
            while runner.running_source_count() != 1:
                if task.done():
                    await task
                await asyncio.sleep(0.01)

        await asyncio.wait_for(ready(), 5)
        yield runner, project
    finally:
        cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task
        await connection.aclose()
        await project.close()


def body(
    *,
    action: str = "update",
    entity_type: str = "Issue",
    team_id: str = TEAM_ID,
    timestamp: datetime | None = None,
    updated_at: str = "2026-10-04T11:59:58.123Z",
) -> bytes:
    """Encode one Issue webhook with description and email sentinels that must be discarded."""
    sent = timestamp or datetime.now(UTC)
    return json.dumps(
        {
            "action": action,
            "type": entity_type,
            "createdAt": "2026-10-04T11:59:59.000Z",
            "organizationId": "d4e5f6a7-b8c9-4d0e-9f1a-2b3c4d5e6f7a",
            "webhookId": "e5f6a7b8-c9d0-4e1f-8a2b-3c4d5e6f7a8b",
            "webhookTimestamp": int(sent.timestamp() * 1000),
            "actor": {"id": ISSUE_ID, "name": "Alice", "email": "sentinel@example.com", "type": "user"},
            "updatedFrom": {"stateId": STATE_ID, "description": "SENTINEL old description"},
            "data": {
                "id": ISSUE_ID,
                "identifier": "ENG-7",
                "number": 7,
                "title": "Fire panel wiring",
                "url": "https://linear.app/acme/issue/ENG-7",
                "priority": 2,
                "description": "SENTINEL description",
                "labelIds": [],
                "teamId": team_id,
                "team": {"id": team_id, "key": "ENG", "name": "Engineering"},
                "stateId": STATE_ID,
                "state": {"id": STATE_ID, "name": "In Progress", "type": "started"},
                "assigneeId": ISSUE_ID,
                "assignee": {"id": ISSUE_ID, "name": "Alice", "email": "sentinel@example.com"},
                "createdAt": "2026-10-01T09:00:00.000Z",
                "updatedAt": updated_at,
            },
        }
    ).encode()


def signed(payload: bytes, key: str = SECRET) -> list[tuple[str, str]]:
    """Sign the exact raw request body with Linear's hex HMAC-SHA256."""
    return [("Linear-Signature", hmac.new(key.encode(), payload, hashlib.sha256).hexdigest())]


@pytest.mark.asyncio
async def test_signed_event_and_filtered_entities(tmp_path: Path) -> None:
    target = CollectingTarget()
    async with running(tmp_path, target, {"actions": ["update"], "teamId": TEAM_ID}) as (runner, project):
        payload = body()
        assert (await runner.handle_request("POST", "/linear/issues", signed(payload), payload)).status == 200
        event = await asyncio.wait_for(target.events.get(), 5)
        assert event.id == f"update:{ISSUE_ID}:1791115198123"
        assert event.payload.issue.team.id == TEAM_ID
        assert event.payload.issue.team.key == "ENG"
        assert event.payload.issue.state.name == "In Progress"
        assert event.payload.issue.assignee_id == ISSUE_ID
        assert event.payload.actor.name == "Alice"
        assert event.payload.previous_state_id == STATE_ID
        assert event.payload.updated_fields == ("description", "stateId")
        assert "SENTINEL" not in repr(event.payload)
        assert "sentinel@example.com" not in repr(event.payload)
        for filtered in (body(entity_type="Comment"), body(action="create"), body(team_id=OTHER_TEAM_ID)):
            assert (
                await runner.handle_request("POST", "/linear/issues", signed(filtered), filtered)
            ).status == 200
        assert target.events.empty()
        assert await project.trigger_inbox(KEY).pending() == ()


@pytest.mark.asyncio
async def test_signature_freshness_and_size_rejections(tmp_path: Path) -> None:
    async with running(tmp_path, CollectingTarget()) as (runner, _):
        payload = body()
        assert (await runner.handle_request("POST", "/linear/issues", [], payload)).status == 400
        assert (
            await runner.handle_request("POST", "/linear/issues", signed(payload, "wrong"), payload)
        ).status == 400
        assert (
            await runner.handle_request("POST", "/linear/issues", signed(payload), payload + b" ")
        ).status == 400
        for stale in (datetime.now(UTC) - timedelta(seconds=61), datetime.now(UTC) + timedelta(seconds=61)):
            stale_body = body(timestamp=stale)
            assert (
                await runner.handle_request("POST", "/linear/issues", signed(stale_body), stale_body)
            ).status == 400
        malformed = b'{"action":"create","type":"Issue","webhookTimestamp":'
        assert (
            await runner.handle_request("POST", "/linear/issues", signed(malformed), malformed)
        ).status == 400
        oversized = body().replace(b"Fire panel wiring", b"x" * 2_000_000)
        assert (await runner.handle_request("POST", "/linear/issues", [], oversized)).status == 413


@pytest.mark.asyncio
async def test_missing_secret_and_restart_replay(tmp_path: Path) -> None:
    payload = body()
    async with running(tmp_path, CollectingTarget(), secret="") as (runner, _):
        assert (await runner.handle_request("POST", "/linear/issues", signed(payload), payload)).status == 503

    class FailingTarget:
        async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[object]) -> None:
            raise RuntimeError("target unavailable")

    async with running(tmp_path, FailingTarget()) as (runner, project):
        assert (await runner.handle_request("POST", "/linear/issues", signed(payload), payload)).status == 200

        async def persisted() -> None:
            while not await project.trigger_inbox(KEY).pending():
                await asyncio.sleep(0.01)

        await asyncio.wait_for(persisted(), 5)
    target = CollectingTarget()
    async with running(tmp_path, target):
        event = await asyncio.wait_for(target.events.get(), 5)
    assert event.id == f"update:{ISSUE_ID}:1791115198123"


def test_trigger_configuration_and_example_ui() -> None:
    """Enumerate the visible team picker and reject invalid stored filters."""
    from dex_connectors.linear import (
        IssueEventReceivedTriggerConfiguration,
        validate_issue_event_received_trigger_configuration,
    )
    from examples.issue_event.flow import IssueEventFlow

    for configuration in (
        IssueEventReceivedTriggerConfiguration(("create", "create")),
        IssueEventReceivedTriggerConfiguration(("archive",)),
        IssueEventReceivedTriggerConfiguration((), "ENG"),
    ):
        with pytest.raises(ValueError):
            validate_issue_event_received_trigger_configuration(configuration)
    validate_issue_event_received_trigger_configuration(
        IssueEventReceivedTriggerConfiguration(("create", "remove"), TEAM_ID)
    )
    binding = IssueEventFlow.__new__(IssueEventFlow).get_connector_trigger_bindings()[0]
    assert binding.connection_name == CONNECTION_NAME
    assert binding.binding_name == BINDING_NAME
    assert binding.configuration_ui is not None
    assert len(binding.configuration_ui.units) == 1
    unit = binding.configuration_ui.units[0]
    assert unit.id == "team" and unit.unit_id == "teamPicker"
    assert unit.bindings[0].json_pointer == "/teamId"
    assert "https://linear.app/settings/api" in unit.description
    assert "Settings > Teams" in unit.description and "Leave it empty" in unit.description


@pytest.mark.asyncio
async def test_endpoint_retries_until_binding_runs(tmp_path: Path) -> None:
    """A valid signed delivery is answered 503 before the configured binding is running."""
    project = await load_project(tmp_path)
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        runner = new_project_issue_event_received_endpoint_runner(
            project, connection, [ProjectIssueEventReceivedTriggerRoute(BINDING_NAME, CollectingTarget())]
        )
        payload = body()
        assert runner.running_source_count() == 0
        assert (await runner.handle_request("POST", "/linear/issues", signed(payload), payload)).status == 503
    finally:
        await connection.aclose()
        await project.close()
