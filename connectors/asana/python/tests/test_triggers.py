# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic taskChanged fetcher, runner wiring, and manifest checks, mirroring the Go tests."""

import json
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any

import httpx
import pytest
import yaml
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerContext,
    TriggerEvent,
)
from dex_connectors.sdk.pollingtrigger import Cursor, RateLimitedError, is_cursor_invalid
from dex_connectors.sdk.projectconfig import Configuration, Scope

import dex_connectors.asana as package
from dex_connectors.asana import (
    TASK_CHANGED_TRIGGER_DEFINITION,
    Client,
    Config,
    Credentials,
    EventParentReference,
    ProjectTaskChangedTriggerRoute,
    ProjectTaskTriggerRunnerConfig,
    TaskChangedEvent,
    TaskChangedTriggerConfiguration,
    TaskChangedTriggerRoute,
    TaskReference,
    TaskTriggerRunnerConfig,
    UserReference,
    decode_task_changed_trigger_configuration,
    new_connection,
    new_task_changed_trigger,
    new_task_trigger_runner,
    validate_task_changed_trigger_configuration,
)
from dex_connectors.asana._event_json import parse_raw_json
from dex_connectors.asana._task_event_fetcher import TaskEventFetcher
from dex_connectors.asana.triggers import new_project_task_trigger_runner
from tests.asana_events_fake import ACCESS_TOKEN, ENDPOINT, AsanaEventsFake

PROJECT_ID = "1201000000000001"
REFERENCE = ConnectionRef("asana", "asana-events")
MANIFEST = yaml.safe_load((Path(__file__).parents[2] / "connector.yaml").read_text())


def new_client(http_client: httpx.AsyncClient, poll_interval_seconds: float = 5) -> Client:
    credentials = StaticCredentialProvider({REFERENCE: Credentials(SecretString(ACCESS_TOKEN))})
    return Client(
        Config(endpoint=ENDPOINT, poll_interval_seconds=poll_interval_seconds), credentials, http_client
    )


def new_fetcher(fake: AsanaEventsFake, project_id: str = PROJECT_ID) -> TaskEventFetcher:
    return TaskEventFetcher(new_client(fake.http_client()), REFERENCE, project_id)


class RecordingTarget:
    def __init__(self) -> None:
        self.events: list[TriggerEvent[TaskChangedEvent]] = []

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[TaskChangedEvent]) -> None:
        self.events.append(event)


async def test_fetcher_seeds_without_backlog_and_delivers_only_task_events() -> None:
    fake = AsanaEventsFake(PROJECT_ID)
    fake.add_task("1204000000000001", "Existing")
    fetcher = new_fetcher(fake)
    context = TriggerContext()

    seed = await fetcher.seed_cursor(context)
    assert seed.token == '{"projectId":"1201000000000001","sync":"sync-1"}', (
        "the existing task is the baseline"
    )
    assert "sync" not in fake.requests[0].query and fake.requests[0].status == 412

    fake.add_task("1204000000000002", "Replace badge reader")
    fake.add_story("1205000000000001")
    fake.complete_task("1204000000000001")
    fake.delete_task("1204000000000002")
    page = await fetcher.fetch_changes(context, seed)
    assert not page.has_more_pages
    assert page.next_cursor.token == '{"projectId":"1201000000000001","sync":"sync-5"}'
    assert len(page.changes) == 3, "the comment story is not a task event"
    added, completed, deleted = (change.payload for change in page.changes)
    assert added == TaskChangedEvent(
        project_id=PROJECT_ID,
        action="added",
        task=TaskReference(id="1204000000000002", name="Replace badge reader"),
        task_subtype="default_task",
        parent=EventParentReference(id=PROJECT_ID, resource_type="project", name="Facilities"),
        user=UserReference(id="1203000000000001", name="Ada Lovelace"),
        occurred_at="2026-03-02T09:00:01Z",
    )
    assert (completed.action, completed.changed_field, completed.change_action) == (
        "changed",
        "completed",
        "changed",
    )
    assert completed.parent is None
    assert deleted.action == "deleted"
    assert page.changes[0].occurred_at == datetime(2026, 3, 2, 9, 0, 1, tzinfo=UTC)
    # The Go fetcher derives these IDs from the same events, so Go and Python replicas share an inbox.
    assert [change.id for change in page.changes] == [
        "asana-94ef95efc2e9cbae0b7708d10be4af34",
        "asana-d60917095242b62fe9a12fac1f4d905c",
        "asana-39f1a4423dba4c43eba1bb14521a82df",
    ]

    query = fake.requests[1].query
    assert query["sync"] == "sync-1" and query["resource"] == PROJECT_ID
    assert "change.field" in query["opt_fields"]
    again = await fetcher.fetch_changes(context, seed)
    assert [change.id for change in again.changes] == [change.id for change in page.changes]


async def test_fetcher_follows_has_more_in_pages_of_one_hundred() -> None:
    fake = AsanaEventsFake(PROJECT_ID)
    fetcher = new_fetcher(fake)
    seed = await fetcher.seed_cursor(TriggerContext())
    for index in range(150):
        fake.add_task(str(1204000000001000 + index), "Bulk task")
    first = await fetcher.fetch_changes(TriggerContext(), seed)
    assert len(first.changes) == 100 and first.has_more_pages
    second = await fetcher.fetch_changes(TriggerContext(), first.next_cursor)
    assert len(second.changes) == 50 and not second.has_more_pages


async def test_fetcher_marks_expired_and_foreign_cursors_invalid() -> None:
    fake = AsanaEventsFake(PROJECT_ID)
    fetcher = new_fetcher(fake)
    seed = await fetcher.seed_cursor(TriggerContext())
    fake.add_task("1204000000000003", "Lost in the gap")
    fake.expire_sync_tokens()
    with pytest.raises(Exception, match="412") as expired:
        await fetcher.fetch_changes(TriggerContext(), seed)
    assert is_cursor_invalid(expired.value), "HTTP 412 for a stored token reseeds"
    for token in ('{"projectId":"1201000000000999","sync":"sync-0"}', "not json", "null", '{"sync":""}'):
        with pytest.raises(Exception) as rejected:
            await fetcher.fetch_changes(TriggerContext(), Cursor(token))
        assert is_cursor_invalid(rejected.value), token


async def test_fetcher_reports_rate_limits_with_retry_after() -> None:
    fake = AsanaEventsFake(PROJECT_ID)
    fake.rate_limit_next(1, "7")
    with pytest.raises(RateLimitedError) as limited:
        await new_fetcher(fake).seed_cursor(TriggerContext())
    assert limited.value.retry_after == timedelta(seconds=7)
    assert not is_cursor_invalid(limited.value)


async def test_fetcher_treats_other_failures_as_retryable_polls() -> None:
    fake = AsanaEventsFake(PROJECT_ID)
    with pytest.raises(Exception, match="HTTP 404") as rejected:
        await new_fetcher(fake, "1201000000000999").seed_cursor(TriggerContext())
    assert not is_cursor_invalid(rejected.value) and not isinstance(rejected.value, RateLimitedError)


@pytest.mark.parametrize(
    ("status", "body", "message"),
    [
        (200, b"[1]", "not valid JSON"),
        (200, b'{"data":[{"action":7}],"sync":"sync-2"}', "not valid JSON"),
        (200, b'{"data":[],"sync":"sync-2",}', "not valid JSON"),
        (200, b'{"data":[]}', "no sync token"),
        (200, json.dumps({"data": [{}] * 101, "sync": "sync-2"}).encode(), "101 events"),
        (412, b"{}", "412 without a fresh sync token"),
        (500, b"{}", "HTTP 500"),
    ],
)
async def test_fetcher_rejects_malformed_pages(status: int, body: bytes, message: str) -> None:
    def answer(request: httpx.Request) -> httpx.Response:
        return httpx.Response(status, content=body)

    fetcher = TaskEventFetcher(
        new_client(httpx.AsyncClient(transport=httpx.MockTransport(answer))), REFERENCE, PROJECT_ID
    )
    with pytest.raises(Exception, match=message) as failed:
        await fetcher.fetch_changes(
            TriggerContext(), Cursor('{"projectId":"1201000000000001","sync":"sync-1"}')
        )
    assert not is_cursor_invalid(failed.value)


def test_event_identity_matches_go_for_raw_values_repeats_and_names() -> None:
    fetcher = TaskEventFetcher(new_client(httpx.AsyncClient()), REFERENCE, PROJECT_ID)
    follower = (
        rb'{"action":"changed","created_at":"2026-03-02T09:00:00Z",'
        rb'"resource":{"gid":"1204000000000001","resource_type":"task"},'
        rb'"change":{"field":"followers","action":"added","added_value":{"gid":"1203000000000002","name":"Grace"}}}'
    )
    raw = (
        rb'[{"action":"changed","created_at":"2026-03-02T09:00:00Z","user":{"gid":"1203000000000002"},'
        rb'"parent":null,"resource":{"gid":"1204000000000001","resource_type":"task","name":"Old"},'
        rb'"change":{"field":"notes","action":"changed","new_value":"caf\u00e9 <b> \"x\"",'
        rb'"added_value":{ "gid" : 7 , "a" : [1, 2.50e1] },"removed_value":null}},'
        b"\n\t" + follower + b",\n\t" + follower + b"]"
    )
    changes = fetcher.convert_events("sync-7", parse_raw_json(raw).elements)
    # Printed by the Go connector's convertEvents for the same bytes.
    assert [change.id for change in changes] == [
        "asana-8979723e6241192efead4ea7e22b2c8b",
        "asana-356a8d7a9091992cd9fe60de0e75ce48",
        "asana-4e2030d08703f58b3d49ace8e7b3a0ba",
    ]
    renamed = raw.replace(b'"name":"Old"', b'"name":"New"')
    assert fetcher.convert_events("sync-7", parse_raw_json(renamed).elements)[0].id == changes[0].id
    assert fetcher.convert_events("sync-107", parse_raw_json(raw).elements)[0].id != changes[0].id
    with pytest.raises(Exception, match="unknown action"):
        fetcher.convert_events(
            "sync-1",
            parse_raw_json(b'[{"action":"moved","resource":{"gid":"1","resource_type":"task"}}]').elements,
        )


async def test_event_identity_and_cursor_match_go_for_folded_keys_escapes_and_html() -> None:
    page = (Path(__file__).parent / "go_parity_page.json").read_bytes()

    def answer(request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, content=page)

    client = new_client(httpx.AsyncClient(transport=httpx.MockTransport(answer)))
    fetcher = TaskEventFetcher(client, REFERENCE, "555")
    changes = fetcher.convert_events("req<&>\u2028", parse_raw_json(page).array_member("data"))
    # Printed by the Go connector's convertEvents and encodeCursor for the same bytes.
    assert [change.id for change in changes] == [
        "asana-ccc4d0ed986df566d6cb70b0dfbac239",
        "asana-5de4c7e345ddad0544db6d4118e40efa",
        "asana-73f43676a5fc5b5f33625b9b6b1f8073",
        "asana-436881099e03f0ad4014a13a36225578",
        "asana-3e3b666dec2f900a239c745fc2ae3df4",
    ]
    assert changes[3].occurred_at == datetime(2026, 11, 1, 8, 0, 1, tzinfo=UTC)
    next_page = await fetcher.fetch_changes(TriggerContext(), Cursor('{"projectId":"555","sync":"s"}'))
    expected_token = r'{"projectId":"555","sync":"s\u003c\u0026\u003e\u2028' + "\u00e9" + '"}'
    assert next_page.next_cursor.token == expected_token


def test_configuration_requires_a_project_gid() -> None:
    validate_task_changed_trigger_configuration(
        TaskChangedTriggerConfiguration(workspace_id="1200000000000001", project_id=PROJECT_ID)
    )
    for invalid in (
        TaskChangedTriggerConfiguration(),
        TaskChangedTriggerConfiguration(project_id="Facilities"),
        TaskChangedTriggerConfiguration(project_id=PROJECT_ID, workspace_id="ops"),
    ):
        with pytest.raises(ValueError):
            validate_task_changed_trigger_configuration(invalid)
    with pytest.raises(ValueError):
        decode_task_changed_trigger_configuration({"projectId": PROJECT_ID, "sectionId": "1"})


def test_runner_rejects_incomplete_routes_and_short_intervals() -> None:
    connection = new_connection(new_client(httpx.AsyncClient()), REFERENCE)
    target, project = RecordingTarget(), TaskChangedTriggerConfiguration(project_id=PROJECT_ID)
    for routes in (
        [],
        [TaskChangedTriggerRoute("", project, target)],
        [TaskChangedTriggerRoute("projectTasks", project, None)],  # type: ignore[arg-type]
        [TaskChangedTriggerRoute("projectTasks", TaskChangedTriggerConfiguration(), target)],
        [
            TaskChangedTriggerRoute("projectTasks", project, target),
            TaskChangedTriggerRoute("projectTasks", project, target),
        ],
    ):
        with pytest.raises(ValueError):
            new_task_trigger_runner(TaskTriggerRunnerConfig(connection, routes))
    with pytest.raises(ValueError, match="pollInterval must be at least 5s"):
        new_client(httpx.AsyncClient(), poll_interval_seconds=1)
    with pytest.raises(ValueError, match="projectId"):
        new_task_changed_trigger(
            connection=connection,
            binding_name="b",
            configuration=TaskChangedTriggerConfiguration(),
            target=target,
        )


def test_project_runner_requires_a_saved_project() -> None:
    connection = new_connection(new_client(httpx.AsyncClient()), REFERENCE)

    class Project:
        configuration = Configuration(Scope("asana-trigger-test", "live"), 1, ())

    config = ProjectTaskTriggerRunnerConfig(
        [ProjectTaskChangedTriggerRoute("projectTasks", RecordingTarget())]
    )
    with pytest.raises(ValueError, match="has no saved project"):
        new_project_task_trigger_runner(Project(), connection, config)  # type: ignore[arg-type]


def test_python_trigger_matches_manifest_and_descriptor() -> None:
    assert MANIFEST["spec"]["languages"]["python"]["triggers"] == ["taskChanged"]
    (manifest_trigger,) = MANIFEST["spec"]["triggers"]
    assert TASK_CHANGED_TRIGGER_DEFINITION.trigger.trigger_name == manifest_trigger["name"]
    assert TASK_CHANGED_TRIGGER_DEFINITION.description == manifest_trigger["description"]
    descriptor: dict[str, Any] = json.loads(
        Path(package.__file__).with_name("dex-connector.json").read_text()
    )
    (factory,) = [factory for factory in descriptor["factories"] if factory["kind"] == "trigger"]
    assert (
        factory["name"] == "define_task_changed_trigger_binding" and factory["triggerName"] == "taskChanged"
    )
    assert callable(getattr(package, factory["name"]))
