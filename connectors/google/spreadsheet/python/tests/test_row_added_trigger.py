# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Port the Go rowAdded runner tests: the memory-cursor Trigger, route validation, and durable restarts."""

import asyncio
import json
from collections.abc import Callable, Coroutine
from pathlib import Path
from typing import Any

import httpx
import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerEvent,
)
from dex_connectors.sdk.pollingtrigger import new_memory_cursor_store
from dex_connectors.sdk.projectconfig import (
    InMemoryObjectStore,
    ObjectNotFoundError,
    PollCursorKey,
    PollCursorStore,
    Scope,
    TriggerInbox,
    TriggerInboxKey,
    load_dex_web_project,
    new_durable_trigger_target,
)

from dex_connectors.google_spreadsheet import (
    Client,
    Config,
    Connection,
    Credentials,
    ProjectRowAddedTriggerRoute,
    RowAddedEvent,
    RowAddedTriggerConfiguration,
    RowAddedTriggerRoute,
    RowAddedTriggerRunnerConfig,
    decode_row_added_event_payload,
    new_connection,
    new_project_row_added_trigger_runner,
    new_row_added_trigger,
    new_row_added_trigger_runner,
)
from examples.new_row_intake.sheets_fake import SHEETS_ENDPOINT, SheetsFake

LEADS_HEADER = ["email", "name", "status"]
REFERENCE = ConnectionRef("google", "sheets-leads")
LEADS = RowAddedTriggerConfiguration("sheet-1", "Leads")


class RecordingRowTarget:
    """Collects delivered row numbers."""

    def __init__(self) -> None:
        self.rows: list[int] = []

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[RowAddedEvent]) -> None:
        self.rows.append(event.payload.row_number)


def new_test_connection(sheet: SheetsFake) -> Connection:
    credentials = Credentials(
        "client", SecretString(""), SecretString("SENTINEL-ACCESS-TOKEN"), SecretString("")
    )
    client = Client(
        Config(SHEETS_ENDPOINT, poll_interval_seconds=1),
        StaticCredentialProvider({REFERENCE: credentials}),
        httpx.AsyncClient(transport=sheet.transport()),
    )
    return new_connection(client, REFERENCE)


async def run_until(
    run: Callable[[TriggerContext], Coroutine[Any, Any, None]], condition: Callable[[], bool]
) -> None:
    """Run in the background and cancel once condition holds, within 20 seconds."""
    context = TriggerContext()
    task = asyncio.create_task(run(context))
    try:
        async with asyncio.timeout(20):
            while not condition():
                if task.done():
                    await task
                await asyncio.sleep(0.02)
    finally:
        context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task


async def test_memory_trigger_seeds_then_delivers_appended_rows() -> None:
    sheet = SheetsFake("Leads", LEADS_HEADER, ["existing"])
    target = RecordingRowTarget()
    trigger = new_row_added_trigger(
        connection=new_test_connection(sheet), binding_name="new-leads", configuration=LEADS, target=target
    )
    sheet.append_rows_after_requests(1, ["added"])
    await run_until(trigger.run, lambda: len(target.rows) == 1)
    assert target.rows == [3]
    with pytest.raises(ValueError, match="spreadsheetId and sheetName are required"):
        new_row_added_trigger(
            connection=new_test_connection(sheet),
            binding_name="new-leads",
            configuration=RowAddedTriggerConfiguration(),
            target=target,
        )


def test_runner_rejects_incomplete_routes() -> None:
    connection = new_test_connection(SheetsFake("Leads"))
    valid = RowAddedTriggerRoute("new-leads", LEADS, new_memory_cursor_store(), RecordingRowTarget())
    new_row_added_trigger_runner(RowAddedTriggerRunnerConfig(connection, [valid]))
    incomplete = {
        "binding": RowAddedTriggerRoute("", LEADS, valid.cursor, valid.target),
        "cursor": RowAddedTriggerRoute("new-leads", LEADS, None, valid.target),  # type: ignore[arg-type]
        "target": RowAddedTriggerRoute("new-leads", LEADS, valid.cursor, None),  # type: ignore[arg-type]
        "configuration": RowAddedTriggerRoute(
            "new-leads", RowAddedTriggerConfiguration(), valid.cursor, valid.target
        ),
    }
    for name, route in incomplete.items():
        with pytest.raises(ValueError):
            new_row_added_trigger_runner(RowAddedTriggerRunnerConfig(connection, [route]))
        assert name
    duplicate = RowAddedTriggerRoute("new-leads", LEADS, new_memory_cursor_store(), valid.target)
    with pytest.raises(ValueError):
        new_row_added_trigger_runner(RowAddedTriggerRunnerConfig(connection, [valid, duplicate]))
    with pytest.raises(ValueError):
        new_row_added_trigger_runner(RowAddedTriggerRunnerConfig(connection, []))
    with pytest.raises(ValueError):
        new_row_added_trigger_runner(RowAddedTriggerRunnerConfig(None, [valid]))  # type: ignore[arg-type]


async def test_durable_runner_keeps_its_cursor_across_restarts() -> None:
    sheet = SheetsFake("Leads", LEADS_HEADER, ["existing"])
    objects, scope = InMemoryObjectStore(), Scope("sheets", "preview")
    key = TriggerInboxKey("google-sheets", "sheets-leads", "rowAdded", "new-leads")
    inbox = TriggerInbox(objects, scope, key)
    target = RecordingRowTarget()

    def new_runner_run() -> Callable[[TriggerContext], Coroutine[Any, Any, None]]:
        durable = new_durable_trigger_target(inbox, target, decode_payload=decode_row_added_event_payload)
        cursor = PollCursorStore(objects, scope, PollCursorKey.from_trigger_inbox_key(key))
        route = RowAddedTriggerRoute("new-leads", LEADS, cursor, durable)
        return new_row_added_trigger_runner(
            RowAddedTriggerRunnerConfig(new_test_connection(sheet), [route])
        ).run

    sheet.append_rows_after_requests(1, ["first"])
    await run_until(new_runner_run(), lambda: len(target.rows) == 1)
    # Rows added while no runner polls arrive after the restart; nothing is seeded again or replayed.
    sheet.append_rows(["while-stopped-1"], ["while-stopped-2"])
    await run_until(new_runner_run(), lambda: len(target.rows) == 3)
    assert target.rows == [3, 4, 5]
    assert not await inbox.pending()


async def test_project_runner_decodes_saved_bindings(tmp_path: Path) -> None:
    path = tmp_path / "connections.json"
    binding = {
        "connectorId": "google-sheets",
        "connectionName": "sheets-leads",
        "triggerName": "rowAdded",
        "bindingName": "new-leads",
        "configuration": {"spreadsheetId": "sheet-1", "sheetName": "Leads"},
    }
    connection = {
        "connectorId": "google-sheets",
        "connectionName": "sheets-leads",
        "modulePath": "github.com/superdurable/dex-connectors-library/connectors/google/spreadsheet/go",
        "provider": "google",
        "authMethodId": "oauth",
        "configuration": {"endpoint": SHEETS_ENDPOINT, "pollInterval": 1_000_000_000},
        "credentials": {
            "oauth_client_id": "client",
            "oauth_client_secret": "s",
            "access_token": "a",
            "refresh_token": "r",
        },
    }
    document = {
        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
        "connections": [connection],
        "triggerBindings": [binding, {**binding, "bindingName": "bad", "configuration": {"sheetName": 3}}],
    }
    path.write_text(json.dumps(document))
    path.chmod(0o600)
    project = await load_dex_web_project(path)
    try:
        runner = await new_project_row_added_trigger_runner(
            project, "sheets-leads", [ProjectRowAddedTriggerRoute("new-leads", RecordingRowTarget())]
        )
        await runner.aclose()
        with pytest.raises(ObjectNotFoundError):
            await new_project_row_added_trigger_runner(
                project,
                "sheets-leads",
                [ProjectRowAddedTriggerRoute("unsaved-binding", RecordingRowTarget())],
            )
        with pytest.raises(ValueError, match="'bad' configuration"):
            await new_project_row_added_trigger_runner(
                project, "sheets-leads", [ProjectRowAddedTriggerRoute("bad", RecordingRowTarget())]
            )
    finally:
        await project.close()
