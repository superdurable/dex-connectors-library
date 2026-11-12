# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Hacker News agent against a private ``dexcli dev`` and real MCP servers.

Each test starts its own ``dexcli dev`` engine on free ports, a Worker child,
and ``dex-connectors-mcp serve`` processes, and uses a unique session ID.
Waits poll durable state with a deadline; nothing sleeps for a fixed time.
"""

import json
import os
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
from pathlib import Path
from uuid import uuid4

import pytest
from dex_agents import AgentSessionClient, AgentSessionFlow, AgentSessionStatus, AgentSnapshot, MessageRole
from dex_agents.testing import (
    EXECUTE_COMPLETED_EVENT,
    AgentTestStack,
    PrivateDexEngine,
    read_step_history,
    wait_for_agent_snapshot,
)

from hackernews_agent.agent import (
    GET_ITEM_MCP_TOOL_NAME,
    GET_ITEM_TOOL_NAME,
    LIST_STORY_IDS_MCP_TOOL_NAME,
    LIST_STORY_IDS_TOOL_NAME,
    SAVE_RECORD_MCP_TOOL_NAME,
    SAVE_RECORD_TOOL_NAME,
    TOP_STORY_RECORD_NAME,
    build_hacker_news_agent_flow,
)
from hackernews_agent.connectors_mcp_server import (
    ConnectorsMcpServerProcess,
    ConnectorsMcpServerReady,
    hacker_news_serve_command,
    write_hacker_news_connections_file,
)
from hackernews_agent.main import run_hacker_news_agent
from tests.agent_worker import HACKER_NEWS_MCP_URL_ENVIRONMENT_VARIABLE, NOTES_MCP_URL_ENVIRONMENT_VARIABLE
from tests.fault_proxy import ToolCallFaultProxy

EXAMPLE_DIRECTORY = Path(__file__).resolve().parents[1]
REPOSITORY_ROOT = EXAMPLE_DIRECTORY.parents[2]
INITIAL_MESSAGE = "What is the top Hacker News story right now?"


def required_executable(environment_variable: str, default: Path, how_to_install: str) -> Path:
    """Return an executable the suite needs, or fail with how to install it."""
    path = Path(os.environ.get(environment_variable) or default)
    if not path.is_file():
        pytest.fail(f"{path} is missing: set {environment_variable} or run `{how_to_install}`")
    return path.resolve()


@pytest.fixture(scope="session", autouse=True)
def dexcli_environment() -> None:
    """Point dex_agents.testing at the repository's installed ``dexcli`` when ``DEXCLI`` is unset."""
    os.environ.setdefault("DEX_AGENTS_OPEN_BROWSER", "0")
    if "DEXCLI" not in os.environ:
        os.environ["DEXCLI"] = str(
            required_executable(
                "DEXCLI",
                REPOSITORY_ROOT / ".bin/dexcli",
                "python3 script/dex_compatibility.py install-dexcli --output .bin/dexcli",
            )
        )


@pytest.fixture
def connectors_mcp_binary() -> Path:
    return required_executable(
        "DEX_CONNECTORS_MCP",
        EXAMPLE_DIRECTORY / ".bin/dex-connectors-mcp",
        "python3 tools/install_connectors_mcp.py --output .bin/dex-connectors-mcp",
    )


@pytest.fixture
def notes_fixture_server_binary() -> Path:
    return required_executable(
        "NOTES_FIXTURE_SERVER",
        EXAMPLE_DIRECTORY / ".bin/notes_fixture_server",
        "cd connectorsmcp && GOWORK=off go build -o examples/hackernews_agent/.bin/notes_fixture_server "
        "./examples/hackernews_agent/notes_fixture_server",
    )


@asynccontextmanager
async def running_server(command: list[str], log_path: Path) -> AsyncIterator[ConnectorsMcpServerReady]:
    server = ConnectorsMcpServerProcess(command, log_path)
    ready = await server.start()
    try:
        yield ready
    finally:
        exit_code = await server.stop()
        assert exit_code == 0, f"{command[0]} exited with {exit_code}; see {log_path}"


@asynccontextmanager
async def running_hacker_news_server(binary: Path, work_dir: Path) -> AsyncIterator[ConnectorsMcpServerReady]:
    connections_file = work_dir / "connections.json"
    write_hacker_news_connections_file(connections_file)
    command = hacker_news_serve_command(binary, work_dir / "hacker-news-state", connections_file)
    async with running_server(command, work_dir / "hacker-news-mcp.log") as ready:
        assert ready.tools == [GET_ITEM_MCP_TOOL_NAME, LIST_STORY_IDS_MCP_TOOL_NAME], ready.warnings
        yield ready


async def run_first_turn(sessions: AgentSessionClient, session_id: str) -> AgentSnapshot:
    await sessions.start_session(session_id, INITIAL_MESSAGE)
    return await wait_for_agent_snapshot(
        sessions,
        session_id,
        first_turn_finished,
        description=f"session {session_id} finishes its first turn",
    )


def first_turn_finished(snapshot: AgentSnapshot) -> AgentSnapshot | None:
    state = snapshot.state
    if state is None:
        return None
    if state.status is AgentSessionStatus.FAILED:
        pytest.fail(f"the session failed: {state.last_turn_error}")
    if state.status is AgentSessionStatus.WAITING_FOR_USER_MESSAGE and state.turn_count >= 1:
        return snapshot
    return None


def tool_call_ids(snapshot: AgentSnapshot, tool_name: str) -> list[str]:
    return [
        message.tool_call_id
        for message in snapshot.messages
        if message.role is MessageRole.ASSISTANT and message.tool_name == tool_name and message.tool_call_id
    ]


def tool_output(snapshot: AgentSnapshot, tool_name: str) -> str:
    outputs = [
        message.tool_output
        for message in snapshot.messages
        if message.role is MessageRole.TOOL
        and message.tool_name == tool_name
        and message.tool_output is not None
    ]
    assert len(outputs) == 1, f"one {tool_name} result, got {outputs}"
    return outputs[0]


def final_answer(snapshot: AgentSnapshot) -> str:
    texts = [
        message.text
        for message in snapshot.messages
        if message.role is MessageRole.ASSISTANT and message.text
    ]
    assert texts, "the agent answered"
    return texts[-1]


async def call_tool_attempts(dex_address: str, session_id: str) -> list[int]:
    history = await read_step_history(dex_address, session_id)
    return [
        entry.final_attempt
        for entry in history
        if entry.event_type == EXECUTE_COMPLETED_EVENT and entry.step_type == "CallTool"
    ]


def build_stack(tmp_path: Path, worker_env: dict[str, str], flow: AgentSessionFlow) -> AgentTestStack:
    return AgentTestStack(
        tmp_path / "dex",
        [flow],
        "tests.agent_worker",
        worker_cwd=EXAMPLE_DIRECTORY,
        worker_env={**worker_env, "DEX_AGENTS_OPEN_BROWSER": "0"},
    )


async def test_the_example_entrypoint_reads_hacker_news_through_the_released_server(
    tmp_path: Path, connectors_mcp_binary: Path
) -> None:
    engine = PrivateDexEngine(tmp_path / "dex")
    await engine.start()
    try:
        work_dir = tmp_path / "example"
        work_dir.mkdir()
        session_id = f"hackernews-agent-{uuid4().hex}"
        # The entrypoint starts dex-connectors-mcp, its own Worker, and its own Client.
        snapshot = await run_hacker_news_agent(engine.address, connectors_mcp_binary, work_dir, session_id)

        assert first_turn_finished(snapshot) is snapshot
        story_ids = json.loads(tool_output(snapshot, LIST_STORY_IDS_TOOL_NAME))["ids"]
        item = json.loads(tool_output(snapshot, GET_ITEM_TOOL_NAME))
        assert len(story_ids) == 3
        assert item["id"] == story_ids[0]
        assert final_answer(snapshot) == f"Top Hacker News story {item['id']}: {item['title']}"
        assert await call_tool_attempts(engine.address, session_id) == [1, 1]
        print(f"session {session_id}: top story {item['id']} {item['title']!r}")
    finally:
        engine.kill()


async def test_a_retried_tool_step_resends_its_key_and_a_write_is_replayed(
    tmp_path: Path, connectors_mcp_binary: Path, notes_fixture_server_binary: Path
) -> None:
    stats_file = tmp_path / "notes-provider-stats.json"
    notes_command = [
        str(notes_fixture_server_binary),
        "--provider-stats-file",
        str(stats_file),
        "--",
        "--listen",
        "127.0.0.1:0",
        "--output",
        "json",
        "--exit-on-stdin-close",
        "--state-dir",
        str(tmp_path / "notes-state"),
        "--connection",
        f"example-notes/notes={SAVE_RECORD_MCP_TOOL_NAME.removeprefix('example-notes_')}",
    ]
    async with (
        running_hacker_news_server(connectors_mcp_binary, tmp_path) as hacker_news,
        running_server(notes_command, tmp_path / "notes-mcp.log") as notes,
    ):
        assert notes.tools == [SAVE_RECORD_MCP_TOOL_NAME], notes.warnings
        hacker_news_proxy = ToolCallFaultProxy(hacker_news.url, [GET_ITEM_MCP_TOOL_NAME])
        notes_proxy = ToolCallFaultProxy(notes.url, [SAVE_RECORD_MCP_TOOL_NAME])
        try:
            flow = build_hacker_news_agent_flow(hacker_news_proxy.url, notes_proxy.url)
            worker_env = {
                HACKER_NEWS_MCP_URL_ENVIRONMENT_VARIABLE: hacker_news_proxy.url,
                NOTES_MCP_URL_ENVIRONMENT_VARIABLE: notes_proxy.url,
            }
            async with build_stack(tmp_path, worker_env, flow) as stack:
                await stack.start_worker()
                session_id = f"hackernews-agent-retry-{uuid4().hex}"
                snapshot = await run_first_turn(stack.sessions(flow), session_id)

                item = json.loads(tool_output(snapshot, GET_ITEM_TOOL_NAME))
                saved = json.loads(tool_output(snapshot, SAVE_RECORD_TOOL_NAME))
                assert saved == {"value": item["title"]}
                assert final_answer(snapshot) == (
                    f"Saved the top Hacker News story as {TOP_STORY_RECORD_NAME}: {item['title']}"
                )
                # list_story_ids ran once; get_item and save_record each needed a second CallTool attempt.
                assert await call_tool_attempts(stack.engine.address, session_id) == [1, 2, 2]

                [list_call_id] = tool_call_ids(snapshot, LIST_STORY_IDS_TOOL_NAME)
                [get_item_call_id] = tool_call_ids(snapshot, GET_ITEM_TOOL_NAME)
                [save_call_id] = tool_call_ids(snapshot, SAVE_RECORD_TOOL_NAME)
                [listing] = hacker_news_proxy.recorded_calls(LIST_STORY_IDS_MCP_TOOL_NAME)
                assert listing.idempotency_key == f"{session_id}/turn-1/{list_call_id}"

                # A read keeps no call state: the retry resends the key and the server reads again.
                lost_read, retried_read = hacker_news_proxy.recorded_calls(GET_ITEM_MCP_TOOL_NAME)
                expected_read_key = f"{session_id}/turn-1/{get_item_call_id}"
                assert (lost_read.idempotency_key, retried_read.idempotency_key) == (expected_read_key,) * 2
                assert (lost_read.was_answer_lost, retried_read.was_answer_lost) == (True, False)
                assert not lost_read.is_replayed and not retried_read.is_replayed

                # A declared write runs once per key: the retry gets the stored result back.
                lost_write, replayed_write = notes_proxy.recorded_calls(SAVE_RECORD_MCP_TOOL_NAME)
                expected_write_key = f"{session_id}/turn-1/{save_call_id}"
                assert (lost_write.idempotency_key, replayed_write.idempotency_key) == (
                    expected_write_key,
                ) * 2
                assert (lost_write.was_answer_lost, replayed_write.was_answer_lost) == (True, False)
                assert not lost_write.is_replayed and replayed_write.is_replayed
                assert replayed_write.upstream_result is not None and lost_write.upstream_result is not None
                assert (
                    replayed_write.upstream_result["structuredContent"]
                    == lost_write.upstream_result["structuredContent"]
                )
                for call in (listing, lost_read, retried_read, lost_write, replayed_write):
                    print(
                        f"{call.tool_name} key={call.idempotency_key} lost={call.was_answer_lost} "
                        f"replayed={call.is_replayed}"
                    )
        finally:
            hacker_news_proxy.close()
            notes_proxy.close()
    provider_stats = json.loads(stats_file.read_text())
    print(f"fake notes provider: {provider_stats}")
    assert provider_stats == {"recordRequests": 1, "maximumAppliedRecordWrites": 1}
