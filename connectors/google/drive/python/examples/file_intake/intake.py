# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Route the saved fileCreated binding's events to the file intake Flow through a durable inbox."""

import dex
from dex_connectors.sdk import TriggerLogger, TriggerTarget, new_dex_flow_trigger_target
from dex_connectors.sdk.projectconfig import LoadedProject

from dex_connectors.google_drive import (
    FileEvent,
    FileTriggerRunner,
    ProjectFileTriggerRoute,
    ProjectFileTriggerRunnerConfig,
    new_project_file_trigger_runner,
)

from .flow import (
    CONNECTION_NAME,
    NEW_FILE_TRIGGER_BINDING,
    FileIntakeFlow,
    accepts_created_file,
    map_to_flow_input,
    resolve_flow_id,
)


def new_file_intake_target(dex_client: dex.AsyncClient, flow: FileIntakeFlow) -> TriggerTarget[FileEvent]:
    """Start one file intake Flow per created file."""
    return new_dex_flow_trigger_target(
        dex_client,
        flow,
        accepts_event=accepts_created_file,
        resolve_flow_id=resolve_flow_id,
        map_to_flow_input=map_to_flow_input,
    )


async def open_file_intake_runner(
    project: LoadedProject, target: TriggerTarget[FileEvent], logger: TriggerLogger | None = None
) -> FileTriggerRunner:
    """Build the durable runner of the saved binding; close it with ``aclose`` after ``run`` returns."""
    return await new_project_file_trigger_runner(
        project,
        CONNECTION_NAME,
        ProjectFileTriggerRunnerConfig(
            file_created_routes=[ProjectFileTriggerRoute(NEW_FILE_TRIGGER_BINDING, target)]
        ),
        logger=logger,
    )
