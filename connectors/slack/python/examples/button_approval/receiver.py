# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Route the stored root and button bindings to the button approval Flow through one Socket Mode runner."""

import dex
import httpx
from dex_connectors.sdk import new_dex_flow_trigger_target, new_dex_rpc_trigger_target
from dex_connectors.sdk.projectconfig import LoadedProject, TriggerInboxKey, decode_trigger_configuration

from dex_connectors.slack import (
    MessageTriggerRunner,
    ProjectButtonClickedTriggerRoute,
    ProjectChannelThreadCreatedTriggerRoute,
    SlackTriggerOptions,
    decode_button_clicked_trigger_configuration,
    decode_channel_thread_created_trigger_configuration,
    new_project_message_trigger_runner,
)

from .flow import (
    BUTTON_TRIGGER_BINDING,
    CONNECTION_NAME,
    START_TRIGGER_BINDING,
    ButtonApprovalFlow,
    map_to_flow_input,
    map_to_record_button_decision_input,
    new_button_trigger_filter,
    new_start_trigger_filter,
    resolve_button_flow_id,
    resolve_flow_id,
)


async def open_button_approval_runner(
    project: LoadedProject,
    dex_client: dex.AsyncClient,
    flow: ButtonApprovalFlow,
    http_client: httpx.AsyncClient | None = None,
    options: SlackTriggerOptions | None = None,
) -> MessageTriggerRunner:
    """Run both bindings on one runner, because Slack sends each Socket Mode event to only one connection.

    Clicks reach only Socket Mode, so the runner rejects a connection without an app_token. Close the
    runner with ``aclose``.
    """
    start_configuration = decode_trigger_configuration(
        project.configuration,
        TriggerInboxKey("slack", CONNECTION_NAME, "channelThreadCreated", START_TRIGGER_BINDING),
        decode_channel_thread_created_trigger_configuration,
    )
    button_configuration = decode_trigger_configuration(
        project.configuration,
        TriggerInboxKey("slack", CONNECTION_NAME, "buttonClicked", BUTTON_TRIGGER_BINDING),
        decode_button_clicked_trigger_configuration,
    )
    logger = options.logger if options is not None else None
    start = new_dex_flow_trigger_target(
        dex_client,
        flow,
        accepts_event=new_start_trigger_filter(start_configuration),
        resolve_flow_id=resolve_flow_id,
        map_to_flow_input=map_to_flow_input,
        logger=logger,
    )
    decide = new_dex_rpc_trigger_target(
        dex_client,
        flow.record_button_decision,
        accepts_event=new_button_trigger_filter(button_configuration),
        resolve_flow_id=resolve_button_flow_id,
        map_to_rpc_input=map_to_record_button_decision_input,
        logger=logger,
    )
    return await new_project_message_trigger_runner(
        project,
        CONNECTION_NAME,
        channel_thread_created_routes=[ProjectChannelThreadCreatedTriggerRoute(START_TRIGGER_BINDING, start)],
        button_clicked_routes=[ProjectButtonClickedTriggerRoute(BUTTON_TRIGGER_BINDING, decide)],
        http_client=http_client,
        options=options,
    )
