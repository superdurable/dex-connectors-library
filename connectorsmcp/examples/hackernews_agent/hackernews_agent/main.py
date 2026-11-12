# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run one Hacker News agent turn against a running ``dexcli dev``.

    uv run python -m hackernews_agent.main --connectors-mcp .bin/dex-connectors-mcp

The command starts ``dex-connectors-mcp serve``, a Worker, and a Client in
this process, starts a session with a unique ID, waits for its first turn, and
prints the stored conversation, then stops the session.
"""

import argparse
import asyncio
import tempfile
import time
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

from dex import (
    AsyncClient,
    AsyncWorker,
    BlobCacheConfig,
    ClientOptions,
    PullWorkerOptions,
    Registry,
    StopFlowOptions,
    WorkerOptions,
    open_blob_cache,
)
from dex_agents import AgentSessionClient, AgentSessionStatus, AgentSnapshot

from hackernews_agent.agent import build_hacker_news_agent_flow
from hackernews_agent.connectors_mcp_server import (
    ConnectorsMcpServerProcess,
    hacker_news_serve_command,
    write_hacker_news_connections_file,
)

DEFAULT_DEX_ADDRESS = "127.0.0.1:8801"
TURN_TIMEOUT = timedelta(seconds=90)
SNAPSHOT_POLL_INTERVAL = timedelta(milliseconds=250)
WORKER_REGISTRATION_TIMEOUT = timedelta(seconds=30)
BLOB_CACHE_BYTES = 64 * 1024 * 1024
INITIAL_MESSAGE = "What is the top Hacker News story right now?"


async def run_hacker_news_agent(
    dex_address: str, connectors_mcp: Path, work_dir: Path, session_id: str
) -> AgentSnapshot:
    """Run one agent turn and return the session snapshot after it.

    Args:
        dex_address: The Dex Server address of ``dexcli dev``.
        connectors_mcp: The released ``dex-connectors-mcp`` executable.
        work_dir: A private directory for the server state, logs, and BlobCache.
        session_id: A new, unique session ID; it is the Flow ID.

    Returns:
        The snapshot once the first turn finished, before the session was stopped.
    """
    connections_file = work_dir / "connections.json"
    write_hacker_news_connections_file(connections_file)
    server = ConnectorsMcpServerProcess(
        hacker_news_serve_command(connectors_mcp, work_dir / "connectors-mcp-state", connections_file),
        work_dir / "dex-connectors-mcp.log",
    )
    ready = await server.start()
    print(f"dex-connectors-mcp {ready.version} at {ready.url} offers {ready.tools}")
    try:
        flow = build_hacker_news_agent_flow(ready.url)
        registry = Registry([flow], allow_async_handlers=True)
        cache = open_blob_cache(BlobCacheConfig(str(work_dir / "blob-cache"), BLOB_CACHE_BYTES))
        worker = AsyncWorker(
            registry,
            cache,
            WorkerOptions(server_address=dex_address, pull=PullWorkerOptions(max_concurrent_tasks=8)),
        )
        client = AsyncClient(registry, cache, ClientOptions(server_address=dex_address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=WORKER_REGISTRATION_TIMEOUT.total_seconds())
            sessions = AgentSessionClient(client, flow)
            await sessions.start_session(session_id, INITIAL_MESSAGE)
            print(f"started session {session_id}")
            snapshot = await wait_for_first_turn(sessions, session_id)
            # A session waits for the next message forever; the example ends after one turn.
            await client.stop_flow(session_id, StopFlowOptions(reason="the example finished its turn"))
            return snapshot
        finally:
            await worker.close()
            await worker_task
            await client.close()
            cache.close()
    finally:
        await server.stop()


async def wait_for_first_turn(sessions: AgentSessionClient, session_id: str) -> AgentSnapshot:
    """Poll the session snapshot until its first turn finished, within ``TURN_TIMEOUT``.

    Args:
        sessions: The session Client.
        session_id: The session's Flow ID.

    Returns:
        The snapshot that shows the finished turn.

    Raises:
        TimeoutError: If the turn did not finish in time.
    """
    deadline = time.monotonic() + TURN_TIMEOUT.total_seconds()
    while True:
        snapshot = await sessions.get_agent_snapshot(session_id)
        state = snapshot.state
        if state is not None and (
            state.status is AgentSessionStatus.FAILED
            or (state.status is AgentSessionStatus.WAITING_FOR_USER_MESSAGE and state.turn_count >= 1)
        ):
            return snapshot
        if time.monotonic() >= deadline:
            raise TimeoutError(f"session {session_id} did not finish its first turn in {TURN_TIMEOUT}")
        await asyncio.sleep(SNAPSHOT_POLL_INTERVAL.total_seconds())


def print_conversation(snapshot: AgentSnapshot) -> None:
    """Print the stored messages of a snapshot, one line each.

    Args:
        snapshot: The session snapshot.
    """
    for message in snapshot.messages:
        if message.tool_arguments is not None:
            print(f"{message.sequence:>3} {message.role}: call {message.tool_name} {message.tool_arguments}")
        elif message.tool_output is not None:
            print(f"{message.sequence:>3} {message.role}: {message.tool_name} -> {message.tool_output[:200]}")
        else:
            print(f"{message.sequence:>3} {message.role}: {message.text}")


def main() -> None:
    """Parse the command line and run one turn."""
    parser = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    parser.add_argument("--dex-address", default=DEFAULT_DEX_ADDRESS, help="Dex Server address of dexcli dev")
    parser.add_argument(
        "--connectors-mcp", type=Path, required=True, help="the dex-connectors-mcp executable"
    )
    arguments = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="hackernews-agent-") as work_dir:
        session_id = f"hackernews-agent-{uuid4().hex}"
        snapshot = asyncio.run(
            run_hacker_news_agent(
                arguments.dex_address, arguments.connectors_mcp.resolve(), Path(work_dir), session_id
            )
        )
    print_conversation(snapshot)
    if snapshot.state is None or snapshot.state.status is AgentSessionStatus.FAILED:
        raise SystemExit(
            f"the session failed: {snapshot.state.last_turn_error if snapshot.state else 'no state'}"
        )


if __name__ == "__main__":
    main()
