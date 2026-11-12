# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The Worker child process the integration tests start and can kill."""

import os

from dex_agents.testing import run_agent_worker

from hackernews_agent.agent import build_hacker_news_agent_flow

#: The Hacker News MCP endpoint the Worker's McpToolSource calls.
HACKER_NEWS_MCP_URL_ENVIRONMENT_VARIABLE = "HACKER_NEWS_AGENT_HACKER_NEWS_MCP_URL"

#: The Example Notes MCP endpoint, or unset to offer only Hacker News.
NOTES_MCP_URL_ENVIRONMENT_VARIABLE = "HACKER_NEWS_AGENT_NOTES_MCP_URL"

if __name__ == "__main__":
    run_agent_worker(
        [
            build_hacker_news_agent_flow(
                os.environ[HACKER_NEWS_MCP_URL_ENVIRONMENT_VARIABLE],
                os.environ.get(NOTES_MCP_URL_ENVIRONMENT_VARIABLE),
            )
        ]
    )
