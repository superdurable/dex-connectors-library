# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Route the stored rowAdded binding's rows to the intake Flow through its durable project inbox."""

import dex
from dex_connectors.sdk import TriggerLogger, TriggerTarget, new_dex_flow_trigger_target
from dex_connectors.sdk.projectconfig import LoadedProject

from dex_connectors.google_spreadsheet import (
    ProjectRowAddedTriggerRoute,
    RowAddedEvent,
    RowAddedTriggerRunner,
    new_project_row_added_trigger_runner,
)

from .flow import (
    CONNECTION_NAME,
    NEW_ROW_TRIGGER_BINDING,
    NewRowIntakeFlow,
    accept_added_row,
    map_to_flow_input,
    resolve_flow_id,
)


def new_added_row_target(dex_client: dex.AsyncClient, flow: NewRowIntakeFlow) -> TriggerTarget[RowAddedEvent]:
    """Start one intake Flow per added row, with the Trigger event ID as the Flow ID suffix."""
    return new_dex_flow_trigger_target(
        dex_client,
        flow,
        accepts_event=accept_added_row,
        resolve_flow_id=resolve_flow_id,
        map_to_flow_input=map_to_flow_input,
    )


async def open_row_intake_runner(
    project: LoadedProject,
    dex_client: dex.AsyncClient,
    flow: NewRowIntakeFlow,
    logger: TriggerLogger | None = None,
) -> RowAddedTriggerRunner:
    """Create the durable runner for the stored binding; close it with ``aclose`` after ``run`` returns."""
    return await new_project_row_added_trigger_runner(
        project,
        CONNECTION_NAME,
        [ProjectRowAddedTriggerRoute(NEW_ROW_TRIGGER_BINDING, new_added_row_target(dex_client, flow))],
        logger=logger,
    )
