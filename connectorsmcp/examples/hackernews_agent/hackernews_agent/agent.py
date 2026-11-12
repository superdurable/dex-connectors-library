# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The Hacker News agent: a Dex agent session whose tools come from dex-connectors-mcp.

The agent runs on the dex-agents built-in loop with a deterministic scripted
model, so it needs no model key. Every tool it calls is listed by
``McpToolSource`` from a locally started ``dex-connectors-mcp serve`` and runs
in its own Dex tool Step, with the effect the server declares in the Dex MCP
conventions ``_meta``.
"""

import json
from typing import Any

from dex_agents import AgentSessionFlow
from dex_agents.builtin_loop import (
    AgentDefinition,
    ModelRequest,
    ScriptedModelClient,
    ScriptedModelReply,
    ScriptedToolCall,
    ToolResultItem,
    builtin_agent_session_flow,
)
from dex_agents.mcp import McpServerConfig, McpStreamableHttpServerConfig, McpToolSource

#: Stable Flow type of the agent session. Open sessions depend on it.
FLOW_TYPE = "HackerNewsMcpAgent"

#: McpToolSource server name of the released dex-connectors-mcp. It prefixes the tool names.
HACKER_NEWS_SERVER_NAME = "hackernews"

#: McpToolSource server name of the optional Example Notes fixture server.
NOTES_SERVER_NAME = "notes"

#: The Dex Web connection name of the credential-free Hacker News connection.
HACKER_NEWS_CONNECTION_NAME = "hacker-news-public"

#: dex-connectors-mcp tool names, as ``tools/list`` returns them.
LIST_STORY_IDS_MCP_TOOL_NAME = "hacker-news-daily_list_story_ids"
GET_ITEM_MCP_TOOL_NAME = "hacker-news-daily_get_item"
SAVE_RECORD_MCP_TOOL_NAME = "example-notes_save_record"

#: Agent tool names: McpToolSource prefixes each MCP tool name with ``<server>__``.
LIST_STORY_IDS_TOOL_NAME = f"{HACKER_NEWS_SERVER_NAME}__{LIST_STORY_IDS_MCP_TOOL_NAME}"
GET_ITEM_TOOL_NAME = f"{HACKER_NEWS_SERVER_NAME}__{GET_ITEM_MCP_TOOL_NAME}"
SAVE_RECORD_TOOL_NAME = f"{NOTES_SERVER_NAME}__{SAVE_RECORD_MCP_TOOL_NAME}"

#: The record name the agent saves the top story title under.
TOP_STORY_RECORD_NAME = "hacker-news-top-story"

#: How many top story IDs the agent lists.
LISTED_STORY_COUNT = 3

AGENT_INSTRUCTIONS = (
    "You read Hacker News through the Hacker News tools. List the top stories, read the first one, "
    "save its title when a notes tool is offered, and answer with the title."
)


def build_hacker_news_agent_flow(
    hacker_news_mcp_url: str, notes_mcp_url: str | None = None
) -> AgentSessionFlow:
    """Build the agent session Flow.

    Build it once per process from Worker configuration, and pass the same
    instance to the Registry and to every Client call. The server URLs are
    trusted Worker configuration, never Flow input.

    Args:
        hacker_news_mcp_url: The ``url`` of the ``dex-connectors-mcp serve``
            ready line, such as ``http://127.0.0.1:8803/mcp``.
        notes_mcp_url: The ``url`` of the Example Notes fixture server, or
            ``None`` to offer only the Hacker News tools.

    Returns:
        The agent session Flow of Flow type ``FLOW_TYPE``.
    """
    servers: list[McpServerConfig] = [
        McpStreamableHttpServerConfig(name=HACKER_NEWS_SERVER_NAME, url=hacker_news_mcp_url)
    ]
    if notes_mcp_url is not None:
        servers.append(McpStreamableHttpServerConfig(name=NOTES_SERVER_NAME, url=notes_mcp_url))
    definition = AgentDefinition(
        name="hacker-news-reader",
        instructions=AGENT_INSTRUCTIONS,
        model=ScriptedModelClient(reply_as_hacker_news_reader),
        tool_sources=[McpToolSource(servers)],
    )
    return builtin_agent_session_flow(definition, flow_type=FLOW_TYPE)


def reply_as_hacker_news_reader(request: ModelRequest) -> ScriptedModelReply:
    """Answer one model call of the scripted Hacker News reader.

    The reply depends only on the request, which the runner builds from
    committed session state, so a retried ``RunModel`` sends the same calls.

    Args:
        request: The model request.

    Returns:
        The next tool call, or the final answer.
    """
    results = ScriptedModelClient.tool_results(request)
    if not results:
        return _call_tool(LIST_STORY_IDS_TOOL_NAME, {"feed": "top", "limit": LISTED_STORY_COUNT})
    last_result = results[-1]
    decoded = _decode_json_object(last_result)
    if decoded is None:
        return ScriptedModelReply(
            text=f"The {last_result.tool_name} call did not succeed: {last_result.output}"
        )
    if last_result.tool_name == LIST_STORY_IDS_TOOL_NAME:
        story_ids = decoded.get("ids") or []
        if not story_ids:
            return ScriptedModelReply(text="Hacker News listed no top stories.")
        return _call_tool(GET_ITEM_TOOL_NAME, {"id": story_ids[0]})
    if last_result.tool_name == GET_ITEM_TOOL_NAME:
        title = str(decoded.get("title", ""))
        if SAVE_RECORD_TOOL_NAME in {tool.name for tool in request.tools}:
            return _call_tool(SAVE_RECORD_TOOL_NAME, {"name": TOP_STORY_RECORD_NAME, "value": title})
        return ScriptedModelReply(text=f"Top Hacker News story {decoded.get('id')}: {title}")
    if last_result.tool_name == SAVE_RECORD_TOOL_NAME:
        return ScriptedModelReply(
            text=f"Saved the top Hacker News story as {TOP_STORY_RECORD_NAME}: {decoded.get('value')}"
        )
    return ScriptedModelReply(text=f"Unexpected tool result from {last_result.tool_name}.")


def _call_tool(tool_name: str, arguments: dict[str, Any]) -> ScriptedModelReply:
    return ScriptedModelReply(tool_calls=[ScriptedToolCall(tool_name=tool_name, arguments=arguments)])


def _decode_json_object(result: ToolResultItem) -> dict[str, Any] | None:
    """Return a successful result's JSON object; failed calls carry plain text."""
    try:
        decoded = json.loads(result.output)
    except json.JSONDecodeError:
        return None
    return decoded if isinstance(decoded, dict) else None
