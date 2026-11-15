# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic file intake mapping, the static binding, and saved binding configuration."""

import json
from datetime import UTC, datetime
from pathlib import Path

import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider, TriggerEvent
from dex_connectors.sdk.projectconfig import load_dex_web_project

import dex_connectors.google_drive as drive
from examples.file_intake import main
from examples.file_intake.flow import (
    CONNECTION_NAME,
    NEW_FILE_TRIGGER_BINDING,
    FileIntakeFlow,
    FileIntakeInput,
    accepts_created_file,
    map_to_flow_input,
    resolve_flow_id,
)
from examples.file_intake.intake import open_file_intake_runner

MODULE_PATH = "github.com/superdurable/dex-connectors-library/connectors/google/drive/go"
CREATED_AT = datetime(2026, 11, 1, 12, 0, tzinfo=UTC)


def connection(name: str = CONNECTION_NAME) -> drive.Connection:
    reference = ConnectionRef("google", name)
    provider = StaticCredentialProvider({reference: drive.Credentials(access_token=SecretString("inert"))})
    return drive.new_connection(drive.Client(drive.Config(), provider), reference)


def test_maps_one_created_file_event_to_one_flow() -> None:
    file = drive.FileEvent(
        file_id="1AbC", name="Invoice.pdf", mime_type="application/pdf", created_time=CREATED_AT
    )
    event = TriggerEvent("1AbC@created", CREATED_AT, file)
    assert accepts_created_file(event)
    assert not accepts_created_file(TriggerEvent("1AbC@modified@1", CREATED_AT, file))
    assert resolve_flow_id(event) == "google-drive-python-file-intake-google-drive-files-1AbC"
    assert map_to_flow_input(event) == FileIntakeInput(event_id="1AbC@created", file=file)


def test_flow_declares_the_static_file_created_binding() -> None:
    flow = FileIntakeFlow(connection())
    (binding,) = flow.get_connector_trigger_bindings()
    assert binding.definition == drive.FILE_CREATED_TRIGGER_DEFINITION
    assert (binding.connection_name, binding.binding_name) == (CONNECTION_NAME, NEW_FILE_TRIGGER_BINDING)
    assert flow.get_flow_type() == "GoogleDriveFileIntakePython"
    with pytest.raises(ValueError):
        FileIntakeFlow(connection("other"))
    assert callable(main.main)


def write_project(directory: Path, binding_configuration: object) -> Path:
    path = directory / "connections.json"
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
                        "configuration": {"endpoint": "https://drive.example"},
                        "credentials": {"access_token": "inert", "refresh_token": "inert"},
                    }
                ],
                "triggerBindings": [
                    {
                        "connectorId": "google-drive",
                        "connectionName": CONNECTION_NAME,
                        "triggerName": "fileCreated",
                        "bindingName": NEW_FILE_TRIGGER_BINDING,
                        "configuration": binding_configuration,
                    }
                ],
            }
        )
    )
    path.chmod(0o600)
    return path


class InertTarget:
    async def handle_trigger(self, context: object, event: object) -> None:
        raise AssertionError("no event is delivered")


@pytest.mark.parametrize(
    "configuration",
    [
        {"folderId": "fld_inbox", "mimeType": "application/pdf", "unknown": True},
        {"folderId": 7},
        {"folderId": "https://drive.google.com/drive/folders/x"},
        {"mimeType": "PDF"},
    ],
)
async def test_project_runner_rejects_invalid_saved_configuration(
    tmp_path: Path, configuration: object
) -> None:
    project = await load_dex_web_project(write_project(tmp_path, configuration))
    try:
        with pytest.raises(ValueError):
            await open_file_intake_runner(project, InertTarget())
    finally:
        await project.close()


async def test_project_runner_decodes_saved_configuration(tmp_path: Path) -> None:
    saved = {"folderId": "fld_inbox", "folderName": "Inbox", "mimeType": "application/pdf"}
    project = await load_dex_web_project(write_project(tmp_path, saved))
    try:
        runner = await open_file_intake_runner(project, InertTarget())
        await runner.aclose()
        with pytest.raises(ValueError):
            await drive.new_project_file_trigger_runner(
                project, CONNECTION_NAME, drive.ProjectFileTriggerRunnerConfig()
            )
    finally:
        await project.close()
