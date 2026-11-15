# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Route the saved cardChanged binding's card actions to the Flow through a durable inbox and poll cursor."""

import dex
from dex_connectors.sdk import TriggerLogger, new_dex_flow_trigger_target
from dex_connectors.sdk.projectconfig import LoadedProject

from dex_connectors.atlassian_trello import (
    CardTriggerRunner,
    ProjectCardChangedTriggerRoute,
    new_project_card_trigger_runner,
)

from .flow import (
    CARD_CHANGED_TRIGGER_BINDING,
    CONNECTION_NAME,
    CardChangeFlow,
    accepts_every_card_change,
    map_to_flow_input,
    resolve_flow_id,
)


async def open_card_trigger_runner(
    project: LoadedProject,
    dex_client: dex.AsyncClient,
    flow: CardChangeFlow,
    logger: TriggerLogger | None = None,
) -> CardTriggerRunner:
    """Create the binding's poll runner; close it with ``aclose`` after ``run`` returns."""
    target = new_dex_flow_trigger_target(
        dex_client,
        flow,
        accepts_event=accepts_every_card_change,
        resolve_flow_id=resolve_flow_id,
        map_to_flow_input=map_to_flow_input,
        logger=logger,
    )
    return await new_project_card_trigger_runner(
        project,
        CONNECTION_NAME,
        [ProjectCardChangedTriggerRoute(CARD_CHANGED_TRIGGER_BINDING, target)],
        logger=logger,
    )
