# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Route the saved ticketChanged binding to the ticket change Flow through a durable inbox."""

import dex
from dex_connectors.sdk import TriggerLogger, new_dex_flow_trigger_target
from dex_connectors.sdk.pollingtrigger import PollRunner
from dex_connectors.sdk.projectconfig import LoadedProject

from dex_connectors.zendesk_support import (
    Connection,
    ProjectTicketChangedTriggerRoute,
    ProjectTicketTriggerRunnerConfig,
    new_project_ticket_trigger_runner,
)

from .flow import (
    TICKET_CHANGED_TRIGGER_BINDING,
    TicketChangedFlow,
    accepts_every_ticket_change,
    map_to_ticket_change,
    resolve_ticket_change_flow_id,
)


def new_ticket_change_trigger_runner(
    project: LoadedProject,
    connection: Connection,
    dex_client: dex.AsyncClient,
    flow: TicketChangedFlow,
    logger: TriggerLogger | None = None,
) -> PollRunner:
    """Poll the account and start one Flow per ticket change.

    Each change is recorded in the binding's project Trigger inbox before the export cursor moves
    past it.
    """
    target = new_dex_flow_trigger_target(
        dex_client,
        flow,
        accepts_event=accepts_every_ticket_change,
        resolve_flow_id=resolve_ticket_change_flow_id,
        map_to_flow_input=map_to_ticket_change,
    )
    route = ProjectTicketChangedTriggerRoute(TICKET_CHANGED_TRIGGER_BINDING, target)
    return new_project_ticket_trigger_runner(
        project, connection, ProjectTicketTriggerRunnerConfig([route], logger=logger)
    )
