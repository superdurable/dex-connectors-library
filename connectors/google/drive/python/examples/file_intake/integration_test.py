# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start one file intake Flow per created file through a real Dex Worker, across a runner restart."""

import asyncio
import json
import os
import tempfile
from collections.abc import Awaitable, Callable
from datetime import UTC, datetime, timedelta
from pathlib import Path
from uuid import uuid4

import dex
import pytest
from dex_connectors.sdk import TriggerContext, TriggerDeliveryAbortedError, TriggerEvent, TriggerTarget
from dex_connectors.sdk.projectconfig import LoadedProject, load_dex_web_project

from dex_connectors.google_drive import FileEvent, new_project_connection

from .drive_change_log import DriveChangeLog, DriveFile, drive_now
from .flow import (
    CONNECTION_NAME,
    NEW_FILE_TRIGGER_BINDING,
    FileIntakeFlow,
    FileIntakeOutcome,
    resolve_flow_id,
)
from .intake import new_file_intake_target, open_file_intake_runner

ACCESS_TOKEN = "drive-trigger-integration-token"
WATCHED_FOLDER_ID = "fld_inbox"
MODULE_PATH = "github.com/superdurable/dex-connectors-library/connectors/google/drive/go"


def write_project(directory: Path, endpoint: str) -> Path:
    """Write the owner-only connections.json that Dex Web saves for the connection and binding."""
    path = directory / "connections.json"
    expires_at = (datetime.now(UTC) + timedelta(hours=1)).isoformat().replace("+00:00", "Z")
    path.write_text(
        json.dumps(
            {
                "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                "connections": [
                    {
                        "connectorId": "google-drive",
                        "authMethodId": "google-oauth",
                        "provider": "google",
                        "modulePath": MODULE_PATH,
                        "connectionName": CONNECTION_NAME,
                        "configuration": {"endpoint": endpoint, "pollInterval": 1_000_000_000},
                        "credentials": {
                            "oauth_client_id": "client.apps.googleusercontent.com",
                            "oauth_client_secret": "client-secret",
                            "access_token": ACCESS_TOKEN,
                            "refresh_token": "refresh-token",
                        },
                        "credentialExpiresAt": expires_at,
                    }
                ],
                "triggerBindings": [
                    {
                        "connectorId": "google-drive",
                        "connectionName": CONNECTION_NAME,
                        "triggerName": "fileCreated",
                        "bindingName": NEW_FILE_TRIGGER_BINDING,
                        "configuration": {
                            "folderId": WATCHED_FOLDER_ID,
                            "folderName": "Inbox",
                            "mimeType": "application/pdf",
                        },
                    }
                ],
            }
        )
    )
    path.chmod(0o600)
    return path


class CountingTarget:
    """Counts the Flow target's successful deliveries per event."""

    def __init__(self, target: TriggerTarget[FileEvent]) -> None:
        self._target = target
        self.deliveries: dict[str, int] = {}

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[FileEvent]) -> None:
        await self._target.handle_trigger(context, event)
        self.deliveries[event.id] = self.deliveries.get(event.id, 0) + 1


async def eventually(condition: Callable[[], bool], seconds: float = 30) -> None:
    async with asyncio.timeout(seconds):
        while not condition():
            await asyncio.sleep(0.05)


async def run_until(
    project: LoadedProject, target: CountingTarget, work: Callable[[], Awaitable[None]]
) -> None:
    """Run the durable runner exactly as main.py builds it while work runs, then stop it."""
    runner = await open_file_intake_runner(project, target)
    context = TriggerContext()
    task = asyncio.create_task(runner.run(context))
    try:
        await work()
    finally:
        context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await asyncio.wait_for(task, 10)
        await runner.aclose()


async def wait_for_intake(dex_client: dex.AsyncClient, file_id: str) -> FileIntakeOutcome:
    """Poll until the Trigger started the file's Flow, then wait for its outcome."""
    flow_id = resolve_flow_id(TriggerEvent("", datetime.now(UTC), FileEvent(file_id=file_id)))
    for _ in range(1200):
        try:
            result = await asyncio.wait_for(dex_client.wait_for_flow(flow_id, timedelta(seconds=20)), 30)
        except dex.FlowNotFoundError:
            await asyncio.sleep(0.05)
            continue
        assert result.status == dex.FlowStatus.COMPLETED, result.error_message
        return result.single_output(FileIntakeOutcome)
    raise AssertionError(f"the Trigger never started {flow_id}")


@pytest.mark.asyncio
async def test_file_intake_starts_one_flow_per_created_file_across_restarts_with_real_dex() -> None:
    """Backlog, filtered, and edited files start nothing; created files start one Flow each."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    run_id = uuid4().hex[:12]

    def file_id(name: str) -> str:
        return f"{name}_{run_id}"

    change_log = DriveChangeLog(ACCESS_TOKEN)
    hour_ago = drive_now() - timedelta(hours=1)
    backlog = DriveFile(
        file_id("backlog"), "Backlog.pdf", "application/pdf", WATCHED_FOLDER_ID, hour_ago, hour_ago
    )
    change_log.put(backlog)
    with tempfile.TemporaryDirectory(prefix="google-drive-python-file-intake-") as directory:
        # The project outlives each runner, as a replica's project storage does.
        project = await load_dex_web_project(write_project(Path(directory), change_log.url))
        connection = new_project_connection(project, CONNECTION_NAME)
        flow = FileIntakeFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 26))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=30)
            async with dex.AsyncClient(
                registry, cache, dex.ClientOptions(server_address=server_address)
            ) as client:
                target = CountingTarget(new_file_intake_target(client, flow))

                async def first_run() -> None:
                    await eventually(lambda: change_log.list_pages() >= 1)
                    change_log.create_file(
                        file_id("invoice"), "Invoice.pdf", "application/pdf", WATCHED_FOLDER_ID
                    )
                    change_log.create_file(
                        file_id("notes"), "Notes", "application/vnd.google-apps.document", WATCHED_FOLDER_ID
                    )
                    change_log.create_file(
                        file_id("elsewhere"), "Elsewhere.pdf", "application/pdf", "fld_other"
                    )
                    change_log.modify_file(file_id("backlog"), drive_now())
                    invoice = await wait_for_intake(client, file_id("invoice"))
                    assert invoice.event_id == file_id("invoice") + "@created"
                    assert invoice.reported.name == "Invoice.pdf"
                    assert invoice.reported.parents == (WATCHED_FOLDER_ID,)
                    assert invoice.current is not None and invoice.current.id == file_id("invoice")

                await run_until(project, target, first_run)
                # A file created while the runner is stopped starts its Flow after the restart.
                change_log.create_file(file_id("scan"), "Scan.pdf", "application/pdf", WATCHED_FOLDER_ID)

                async def second_run() -> None:
                    scan = await wait_for_intake(client, file_id("scan"))
                    assert scan.reported.name == "Scan.pdf"
                    pages = change_log.list_pages()
                    await eventually(lambda: change_log.list_pages() >= pages + 2)

                await run_until(project, target, second_run)
                for filtered in ("backlog", "notes", "elsewhere"):
                    flow_id = resolve_flow_id(
                        TriggerEvent("", datetime.now(UTC), FileEvent(file_id=file_id(filtered)))
                    )
                    with pytest.raises(dex.FlowNotFoundError):
                        await client.describe_flow(flow_id)
                assert target.deliveries == {
                    file_id("invoice") + "@created": 1,
                    file_id("scan") + "@created": 1,
                }
                assert change_log.requests("/drive/v3/changes/startPageToken") == 1, (
                    "the restart resumed the cursor"
                )
        finally:
            await worker.stop()
            await worker_task
            cache.close()
            await connection.aclose()
            await project.close()
            change_log.close()
