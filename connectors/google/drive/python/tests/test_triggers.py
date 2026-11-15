# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The shared file Trigger runner and the per-Trigger factories against a fake Drive change log."""

import asyncio
import re
from collections.abc import AsyncIterator, Callable, Iterator
from contextlib import asynccontextmanager
from datetime import timedelta
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
from dex_connectors.sdk.pollingtrigger import new_memory_cursor_store

import dex_connectors.google_drive as drive
from examples.file_intake.drive_change_log import DriveChangeLog, DriveFile, ScriptedResponse, drive_now

ACCESS_TOKEN = "drive-trigger-test-token"
CONNECTION = ConnectionRef("google", "drive-files")


class RecordingFileTarget:
    """A non-durable target that consumes every event."""

    def __init__(self) -> None:
        self.event_ids: list[str] = []

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[drive.FileEvent]) -> None:
        self.event_ids.append(event.id)


@pytest.fixture
def change_log() -> Iterator[DriveChangeLog]:
    fake = DriveChangeLog(ACCESS_TOKEN)
    yield fake
    fake.close()


@pytest.fixture
async def connection(change_log: DriveChangeLog) -> AsyncIterator[drive.Connection]:
    provider = StaticCredentialProvider(
        {CONNECTION: drive.Credentials(access_token=SecretString(ACCESS_TOKEN))}
    )
    client = drive.Client(drive.Config(endpoint=change_log.url, poll_interval_seconds=1), provider)
    connection = drive.new_connection(client, CONNECTION)
    yield connection
    await connection.aclose()


class Running:
    def __init__(self, runner: Any) -> None:
        self.context = TriggerContext()
        self.task = asyncio.create_task(runner.run(self.context))


@asynccontextmanager
async def running(runner: Any) -> AsyncIterator[Running]:
    started = Running(runner)
    try:
        yield started
    finally:
        started.context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await started.task


async def eventually(condition: Callable[[], bool], seconds: float = 10) -> None:
    async with asyncio.timeout(seconds):
        while not condition():
            await asyncio.sleep(0.02)


def millis(value: Any) -> str:
    return str(int(value.timestamp() * 1000))


async def test_runner_shares_one_feed_and_routes_each_binding_its_events(
    change_log: DriveChangeLog, connection: drive.Connection
) -> None:
    hour_ago = drive_now() - timedelta(hours=1)
    for file_id in ("old_report", "old_notes", "old_draft", "backlog_upload"):
        change_log.put(DriveFile(file_id, file_id, "application/pdf", "folder_a", hour_ago, hour_ago))
    created_everywhere, created_pdf_in_a = RecordingFileTarget(), RecordingFileTarget()
    updated_everywhere, trashed_in_a = RecordingFileTarget(), RecordingFileTarget()
    pdf_in_a = drive.FileTriggerConfiguration(
        folder_id="folder_a", folder_name="A", mime_type="application/pdf"
    )
    runner = drive.new_file_trigger_runner(
        drive.FileTriggerRunnerConfig(
            connection,
            new_memory_cursor_store(),
            file_created_routes=[
                drive.FileTriggerRoute("every-new-file", created_everywhere),
                drive.FileTriggerRoute("new-pdf-in-a", created_pdf_in_a, pdf_in_a),
            ],
            file_updated_routes=[drive.FileTriggerRoute("every-edit", updated_everywhere)],
            file_trashed_routes=[
                drive.FileTriggerRoute(
                    "trash-in-a", trashed_in_a, drive.FileTriggerConfiguration(folder_id="folder_a")
                )
            ],
        )
    )
    async with running(runner):
        await eventually(lambda: change_log.list_pages() >= 1)
        change_log.create_file("new_pdf_a", "Invoice.pdf", "application/pdf", "folder_a")
        change_log.create_file("new_doc_b", "Notes", "application/vnd.google-apps.document", "folder_b")
        change_log.create_file("new_pdf_b", "Other.pdf", "application/pdf", "folder_b")
        edited_at = drive_now()
        change_log.modify_file("old_report", edited_at)
        change_log.touch_file("old_notes")
        change_log.trash_file("old_draft")
        want_created = ["new_pdf_a@created", "new_doc_b@created", "new_pdf_b@created"]
        await eventually(
            lambda: (
                created_everywhere.event_ids == want_created
                and len(updated_everywhere.event_ids) == 1
                and len(trashed_in_a.event_ids) == 1
            )
        )
        assert created_pdf_in_a.event_ids == ["new_pdf_a@created"]
        assert updated_everywhere.event_ids == ["old_report@modified@" + millis(edited_at)]
        assert re.fullmatch(r"old_draft@trashed@\d+", trashed_in_a.event_ids[0])
        # Later polls deliver nothing again, and every binding shares one seed and one list per poll.
        pages = change_log.list_pages()
        await eventually(lambda: change_log.list_pages() >= pages + 2)
        assert created_everywhere.event_ids == want_created
        assert len(updated_everywhere.event_ids) == 1
        assert change_log.requests("/drive/v3/changes/startPageToken") == 1


async def test_file_trashed_uses_shared_drive_trash_time_and_my_drive_change_time(
    change_log: DriveChangeLog, connection: drive.Connection
) -> None:
    hour_ago = drive_now() - timedelta(hours=1)
    change_log.put(
        DriveFile("shared_file", "Shared", "application/pdf", "folder_a", hour_ago, hour_ago, "drive_1")
    )
    change_log.put(DriveFile("my_file", "Mine", "application/pdf", "folder_a", hour_ago, hour_ago))
    trashed = RecordingFileTarget()
    runner = drive.new_file_trigger_runner(
        drive.FileTriggerRunnerConfig(
            connection,
            new_memory_cursor_store(),
            file_trashed_routes=[drive.FileTriggerRoute("trash", trashed)],
        )
    )
    async with running(runner):
        await eventually(lambda: change_log.list_pages() >= 1)
        change_log.trash_file("shared_file")
        change_log.trash_file("my_file")
        await eventually(lambda: len(set(trashed.event_ids)) == 2)
        # A shared drive file keeps its trashedTime, so a later change repeats its event ID, which a Dex
        # Flow target deduplicates; My Drive reports no trashedTime, so a later change is a new event.
        change_log.touch_file("shared_file")
        change_log.touch_file("my_file")
        await eventually(lambda: len(set(trashed.event_ids)) == 3)
        pages = change_log.list_pages()
        await eventually(lambda: change_log.list_pages() >= pages + 2)
        unique = set(trashed.event_ids)
        assert len(unique) == 3
        shared = [value for value in unique if re.fullmatch(r"shared_file@trashed@\d+", value)]
        mine = [value for value in unique if re.fullmatch(r"my_file@trashed@\d+", value)]
        assert (len(shared), len(mine)) == (1, 2)


async def test_runner_reseeds_after_drive_rejects_the_stored_page_token(
    change_log: DriveChangeLog, connection: drive.Connection
) -> None:
    created = RecordingFileTarget()
    runner = drive.new_file_trigger_runner(
        drive.FileTriggerRunnerConfig(
            connection,
            new_memory_cursor_store(),
            file_created_routes=[drive.FileTriggerRoute("every-new-file", created)],
        )
    )
    change_log.script(ScriptedResponse(400))
    async with running(runner):
        await eventually(lambda: change_log.requests("/drive/v3/changes/startPageToken") == 2)
        change_log.create_file("after_reset", "After.pdf", "application/pdf", "folder_a")
        await eventually(lambda: created.event_ids == ["after_reset@created"])


async def test_runner_pauses_on_rate_limit_and_then_delivers(
    change_log: DriveChangeLog, connection: drive.Connection
) -> None:
    created = RecordingFileTarget()
    runner = drive.new_file_trigger_runner(
        drive.FileTriggerRunnerConfig(
            connection,
            new_memory_cursor_store(),
            file_created_routes=[drive.FileTriggerRoute("new", created)],
        )
    )
    async with running(runner):
        await eventually(lambda: change_log.list_pages() >= 1)
        change_log.script(ScriptedResponse(429, retry_after="1"))
        change_log.create_file("after_limit", "After.pdf", "application/pdf", "folder_a")
        await eventually(lambda: created.event_ids == ["after_limit@created"])
        assert change_log.requests("/drive/v3/changes/startPageToken") == 1


async def test_generated_style_file_created_trigger_polls_with_its_own_cursor(
    change_log: DriveChangeLog, connection: drive.Connection
) -> None:
    created = RecordingFileTarget()
    runner = drive.new_file_created_trigger(
        connection=connection,
        connection_name=CONNECTION.name,
        binding_name="new-docs",
        configuration=drive.FileTriggerConfiguration(mime_type="application/vnd.google-apps.document"),
        target=created,
    )
    async with running(runner):
        await eventually(lambda: change_log.list_pages() >= 1)
        change_log.create_file("sheet", "Budget", "application/vnd.google-apps.spreadsheet", "folder_a")
        change_log.create_file("doc", "Plan", "application/vnd.google-apps.document", "folder_a")
        await eventually(lambda: created.event_ids == ["doc@created"])
    with pytest.raises(ValueError):
        drive.new_file_trashed_trigger(
            connection=connection,
            connection_name=CONNECTION.name,
            binding_name="bad",
            configuration=drive.FileTriggerConfiguration(mime_type="PDF"),
            target=created,
        )
    with pytest.raises(ValueError):
        drive.new_file_updated_trigger(
            connection=connection,
            connection_name="other",
            binding_name="edits",
            configuration=drive.FileTriggerConfiguration(),
            target=created,
        )


async def test_new_file_trigger_runner_rejects_incomplete_configuration(connection: drive.Connection) -> None:
    cursor, target = new_memory_cursor_store(), RecordingFileTarget()
    route = drive.FileTriggerRoute("new", target)
    mock = drive.new_connection_with_operations(
        connection.search_files(),
        connection.get_file(),
        connection.read_file_text(),
        connection.upload_file(),
        CONNECTION,
    )
    for name, config in {
        "mock connection": drive.FileTriggerRunnerConfig(mock, cursor, file_created_routes=[route]),
        "no cursor": drive.FileTriggerRunnerConfig(connection, None, file_created_routes=[route]),  # type: ignore[arg-type]
        "no routes": drive.FileTriggerRunnerConfig(connection, cursor),
        "no target": drive.FileTriggerRunnerConfig(
            connection,
            cursor,
            file_created_routes=[drive.FileTriggerRoute("new", None)],  # type: ignore[arg-type]
        ),
        "blank binding": drive.FileTriggerRunnerConfig(
            connection, cursor, file_created_routes=[drive.FileTriggerRoute("  ", target)]
        ),
        "invalid folder": drive.FileTriggerRunnerConfig(
            connection,
            cursor,
            file_created_routes=[
                drive.FileTriggerRoute("new", target, drive.FileTriggerConfiguration(folder_id="a b"))
            ],
        ),
        "duplicate binding": drive.FileTriggerRunnerConfig(
            connection, cursor, file_created_routes=[route, route]
        ),
    }.items():
        with pytest.raises(ValueError):
            drive.new_file_trigger_runner(config)
        assert name
    drive.new_file_trigger_runner(
        drive.FileTriggerRunnerConfig(
            connection, cursor, file_created_routes=[route], file_trashed_routes=[route]
        )
    )
