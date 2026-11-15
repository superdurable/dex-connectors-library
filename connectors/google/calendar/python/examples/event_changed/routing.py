# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Route the saved eventChanged binding's changes to the event change Flow through a durable inbox."""

import dex
from dex_connectors.sdk import TriggerTarget, new_dex_flow_trigger_target
from dex_connectors.sdk.pollingtrigger import PollRunner
from dex_connectors.sdk.projectconfig import LoadedProject

from dex_connectors.google_calendar import (
    Connection,
    EventChangedEvent,
    ProjectEventChangedTriggerRoute,
    new_project_event_trigger_runner,
)

from .flow import (
    EVENT_CHANGED_TRIGGER_BINDING,
    EventChangeFlow,
    accept_every_event_change,
    map_to_flow_input,
    resolve_flow_id,
)


def new_event_change_target(
    dex_client: dex.AsyncClient, flow: EventChangeFlow
) -> TriggerTarget[EventChangedEvent]:
    """Start one EventChangeFlow per change; the change's Trigger event ID is the request ID."""
    return new_dex_flow_trigger_target(
        dex_client,
        flow,
        accepts_event=accept_every_event_change,
        resolve_flow_id=resolve_flow_id,
        map_to_flow_input=map_to_flow_input,
    )


def open_event_change_runner(
    project: LoadedProject, connection: Connection, dex_client: dex.AsyncClient, flow: EventChangeFlow
) -> PollRunner:
    """Each change is recorded in the binding's project Trigger inbox before the sync token moves past it."""
    route = ProjectEventChangedTriggerRoute(
        EVENT_CHANGED_TRIGGER_BINDING, new_event_change_target(dex_client, flow)
    )
    return new_project_event_trigger_runner(project, connection, [route])
