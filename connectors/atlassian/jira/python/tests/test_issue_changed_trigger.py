# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""issueChanged cursor codec, event identity, configuration, and wiring checked against Go recordings."""

import asyncio
import json
from datetime import datetime
from pathlib import Path

import dex
import httpx
import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerCancellation,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerEvent,
)
from dex_connectors.sdk.pollingtrigger import Cursor
from dex_connectors.sdk.projectconfig import (
    InMemoryObjectStore,
    PollCursorKey,
    PollCursorStore,
    Scope,
    TriggerInbox,
    TriggerInboxKey,
    new_durable_trigger_target,
)

from dex_connectors.atlassian_jira import (
    ISSUE_CHANGED_TRIGGER_DEFINITION,
    Client,
    Config,
    Credentials,
    IssueChangedEvent,
    IssueChangedTriggerConfiguration,
    IssueChangedTriggerRoute,
    IssueTriggerRunnerConfig,
    decode_issue_changed_event_payload,
    decode_issue_changed_trigger_configuration,
    define_issue_changed_trigger_binding,
    new_connection,
    new_issue_changed_trigger,
    new_issue_trigger_runner,
    validate_issue_changed_trigger_configuration,
)
from dex_connectors.atlassian_jira.issue_change_cursor import (
    IssueChangeCursor,
    decode_issue_change_cursor,
    encode_issue_change_cursor,
    recent_seen_versions,
)
from dex_connectors.atlassian_jira.issue_change_fetcher import _new_issue_changed_event
from dex_connectors.atlassian_jira.operations import _issue_resource

from .jira_search_fake import CLOUD_ID, OPERATIONS, JiraSearchSite

GO_CASES = json.loads((Path(__file__).parent / "fixtures/go-issue-change-cases.json").read_text())
CONNECTION = ConnectionRef("atlassian", "jira-trigger-test")


def _position(document: dict[str, object]) -> IssueChangeCursor:
    return IssueChangeCursor(
        cloud_id=str(document.get("cloudId", "")),
        project=str(document.get("project", "")),
        updated_through=int(str(document.get("updatedThrough", 0))),
        seen_versions=list(document.get("seenVersions") or []),  # type: ignore[call-overload]
        page_token=str(document.get("pageToken", "")),
        search_minutes=int(str(document.get("searchMinutes", 0))),
        search_started_at=int(str(document.get("searchStartedAt", 0))),
        search_floor=int(str(document.get("searchFloor", 0))),
    )


@pytest.mark.parametrize("case", GO_CASES["encode"], ids=lambda case: case["token"][:60])
def test_cursor_encoding_matches_go_bytes(case: dict[str, object]) -> None:
    position = _position(case["position"])  # type: ignore[arg-type]
    assert encode_issue_change_cursor(position).token == case["token"]
    assert decode_issue_change_cursor(Cursor(str(case["token"]))) == position


@pytest.mark.parametrize("case", GO_CASES["decode"], ids=lambda case: case["token"])
def test_cursor_decoding_matches_go(case: dict[str, object]) -> None:
    if not case["isValid"]:
        with pytest.raises(ValueError):
            decode_issue_change_cursor(Cursor(str(case["token"])))
        return
    decoded = decode_issue_change_cursor(Cursor(str(case["token"])))
    assert decoded == _position(case["position"])  # type: ignore[arg-type]


@pytest.mark.parametrize("case", GO_CASES["seenVersions"], ids=["pruned", "bounded"])
def test_seen_versions_match_go(case: dict[str, object]) -> None:
    versions = [(str(issue_id), int(millis)) for issue_id, millis in case["versions"]]  # type: ignore[attr-defined]
    kept = recent_seen_versions(list(case["previous"]), versions, int(str(case["updatedThrough"])))  # type: ignore[call-overload]
    assert kept == case["kept"]


@pytest.mark.parametrize("case", GO_CASES["events"], ids=lambda case: case["eventId"])
def test_events_match_go_identity_and_payload(case: dict[str, object]) -> None:
    issue = _issue_resource(case["resource"], (), False)
    event = _new_issue_changed_event(CLOUD_ID, issue)
    assert event.id == case["eventId"]
    occurred_at = str(case["occurredAt"]).replace("Z", "+00:00")
    assert event.occurred_at == datetime.fromisoformat(occurred_at)
    encoded = dex.CodecRegistry().resolve(IssueChangedEvent).encode(event.payload)
    assert json.loads(str(encoded.data)) == json.loads(str(case["payload"])), "a Go replica decodes the inbox"
    payload = str(case["payload"])
    assert decode_issue_changed_event_payload(payload, payload.encode()) == event.payload


def test_configuration_validation_and_strict_decoding() -> None:
    validate_issue_changed_trigger_configuration(IssueChangedTriggerConfiguration())
    with pytest.raises(ValueError, match="numeric"):
        validate_issue_changed_trigger_configuration(IssueChangedTriggerConfiguration(project_id="OPS"))
    with pytest.raises(ValueError, match="uppercase"):
        validate_issue_changed_trigger_configuration(IssueChangedTriggerConfiguration(project_key="ops"))
    decoded = decode_issue_changed_trigger_configuration(
        {"projectId": "10000", "projectKey": "OPS", "projectName": "Operations"}
    )
    assert decoded == IssueChangedTriggerConfiguration("10000", "OPS", "Operations")
    with pytest.raises(ValueError, match="unknown field"):
        decode_issue_changed_trigger_configuration({"projectKey": "OPS", "jql": "project = OPS"})
    with pytest.raises(ValueError):
        decode_issue_changed_event_payload("{}", b"{}")


def test_binding_factory_declares_the_trigger() -> None:
    binding = define_issue_changed_trigger_binding(
        connection_name="jira-issue-watcher", binding_name="changes"
    )
    assert binding.definition == ISSUE_CHANGED_TRIGGER_DEFINITION
    assert (binding.connection_name, binding.binding_name) == ("jira-issue-watcher", "changes")


def _connection(site: JiraSearchSite) -> object:
    credentials = Credentials(
        "client-id", SecretString("secret"), SecretString("token"), SecretString("refresh")
    )
    client = Client(
        Config(cloud_id=CLOUD_ID, endpoint="http://127.0.0.1:12345", poll_interval_seconds=5),
        StaticCredentialProvider({CONNECTION: credentials}),
        httpx.AsyncClient(transport=site.transport()),
    )
    return new_connection(client, CONNECTION)


class Recorder:
    def __init__(self) -> None:
        self.events: list[TriggerEvent[IssueChangedEvent]] = []

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[IssueChangedEvent]) -> None:
        self.events.append(event)


async def _wait_until(condition: object) -> None:
    async with asyncio.timeout(10):
        while not await condition():  # type: ignore[operator]
            await asyncio.sleep(0.01)


async def test_runner_records_a_durable_baseline_then_delivers_through_the_inbox() -> None:
    site = JiraSearchSite()
    connection = _connection(site)
    objects, scope = InMemoryObjectStore(), Scope("jira-test", "live")
    key = TriggerInboxKey("jira", CONNECTION.name, "issueChanged", "changes")
    cursor = PollCursorStore(objects, scope, PollCursorKey.from_trigger_inbox_key(key))
    recorder = Recorder()
    target = new_durable_trigger_target(
        TriggerInbox(objects, scope, key), recorder, decode_payload=decode_issue_changed_event_payload
    )
    site.create(OPERATIONS, "Existing")
    runner = new_issue_trigger_runner(
        IssueTriggerRunnerConfig(
            connection,  # type: ignore[arg-type]
            [
                IssueChangedTriggerRoute(
                    "changes", IssueChangedTriggerConfiguration(project_key="OPS"), target, cursor
                )
            ],
        )
    )
    cancellation = TriggerCancellation()
    running = asyncio.create_task(runner.run(TriggerContext(cancellation)))

    async def is_seeded() -> bool:
        return (await cursor.read_poll_cursor()).state is not None

    try:
        await _wait_until(is_seeded)
        await asyncio.sleep(0.002)
        created = site.create(OPERATIONS, "Created after the baseline")
        runner.wake_poller("changes")

        async def is_delivered() -> bool:
            return bool(recorder.events)

        await _wait_until(is_delivered)
        assert [event.payload.issue.key for event in recorder.events] == [created]
        state = (await cursor.read_poll_cursor()).state
        assert state is not None and json.loads(state.token)["project"] == "key:OPS"
    finally:
        cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await running


async def test_single_binding_trigger_and_wiring_errors() -> None:
    site = JiraSearchSite()
    connection = _connection(site)
    recorder = Recorder()
    trigger = new_issue_changed_trigger(
        connection=connection,  # type: ignore[arg-type]
        binding_name="changes",
        configuration=IssueChangedTriggerConfiguration(),
        target=recorder,
    )
    cancellation = TriggerCancellation()
    running = asyncio.create_task(trigger.run(TriggerContext(cancellation)))
    await _wait_until(lambda: asyncio.sleep(0.05, result=bool(site.requests())))
    cancellation.cancel()
    with pytest.raises(TriggerDeliveryAbortedError):
        await asyncio.wait_for(running, 10)
    assert site.requests()[0].jql == "updated >= -4m ORDER BY updated DESC"
    with pytest.raises(ValueError, match="routes are required"):
        new_issue_trigger_runner(IssueTriggerRunnerConfig(connection, []))  # type: ignore[arg-type]
    with pytest.raises(ValueError, match="numeric"):
        new_issue_changed_trigger(
            connection=connection,  # type: ignore[arg-type]
            binding_name="changes",
            configuration=IssueChangedTriggerConfiguration(project_id="OPS"),
            target=recorder,
        )
