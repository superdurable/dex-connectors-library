# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Route the saved issueChanged binding's changes to the Flow through a durable inbox and poll cursor."""

import dex
from dex_connectors.sdk import TriggerLogger, new_dex_flow_trigger_target
from dex_connectors.sdk.projectconfig import LoadedProject

from dex_connectors.atlassian_jira import (
    IssueTriggerRunner,
    ProjectIssueChangedTriggerRoute,
    new_project_issue_trigger_runner,
)

from .flow import (
    CONNECTION_NAME,
    ISSUE_CHANGED_TRIGGER_BINDING,
    IssueChangeFlow,
    accepts_every_issue_change,
    map_to_flow_input,
    resolve_flow_id,
)


async def open_issue_trigger_runner(
    project: LoadedProject,
    dex_client: dex.AsyncClient,
    flow: IssueChangeFlow,
    logger: TriggerLogger | None = None,
) -> IssueTriggerRunner:
    """Create the binding's poll runner; close it with ``aclose`` after ``run`` returns."""
    target = new_dex_flow_trigger_target(
        dex_client,
        flow,
        accepts_event=accepts_every_issue_change,
        resolve_flow_id=resolve_flow_id,
        map_to_flow_input=map_to_flow_input,
        logger=logger,
    )
    return await new_project_issue_trigger_runner(
        project,
        CONNECTION_NAME,
        [ProjectIssueChangedTriggerRoute(ISSUE_CHANGED_TRIGGER_BINDING, target)],
        logger=logger,
    )
