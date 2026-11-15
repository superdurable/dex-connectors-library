# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Cross-language marker run: a TypeScript RPC Trigger target against a Python RPC handler.

Run it by hand against dexcli dev after `npm ci && npm run build` in sdkts:
`uv run python integration/cross_language_marker.py`. It exits nonzero unless the TypeScript target
consumes the Python handler's UndeliverableTriggerError after one attempt.
"""

import asyncio
import os
import subprocess
import sys
import tempfile
from pathlib import Path
from uuid import uuid4

import dex

from dex_connectors.sdk import UndeliverableTriggerError

SERVER_ADDRESS = os.environ.get("DEX_SERVER_ADDRESS", "127.0.0.1:19881")
FLOW_TYPE = f"SdkpythonCrossLanguageMarker{uuid4().hex}"


class AwaitDecision(dex.Step[None]):
    """Keeps the Flow open for RPCs."""

    def get_step_type(self) -> str:
        """Name the waiting Step."""
        return "AwaitDecision"

    def execute(self, context: dex.Context, input: None) -> dex.StepDecision:
        """Wait for RPCs."""
        return dex.dead_end()


class DecisionFlow(dex.Flow[None]):
    """A Flow whose RPC rejects every Trigger event."""

    def get_flow_type(self) -> str:
        """Return a run-unique Flow type."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[None]:
        """Register the waiting Step."""
        return dex.StepList.start_step(AwaitDecision())

    @dex.rpc
    def reject(self, context: dex.Context, reason: str) -> None:
        """Reject the event for good."""
        raise UndeliverableTriggerError("the decision was already made")


async def run_cross_language_marker() -> None:
    """Serve the Python handler while the TypeScript target delivers one event."""
    flow = DecisionFlow()
    flow_id = f"cross-language-{uuid4().hex}"
    with tempfile.TemporaryDirectory() as directory:
        cache = dex.open_blob_cache(dex.BlobCacheConfig(directory, 16 * 1024 * 1024))
        registry = dex.Registry([flow], allow_async_handlers=True)
        async with dex.AsyncWorker(
            registry, cache, dex.WorkerOptions(server_address=SERVER_ADDRESS)
        ) as worker:
            worker_task = asyncio.create_task(worker.start())
            try:
                await worker.wait_pull_registered(timeout=30)
                async with dex.AsyncClient(
                    registry, cache, dex.ClientOptions(server_address=SERVER_ADDRESS)
                ) as client:
                    await client.start_flow(flow, flow_id, None)
                    script = Path(__file__).with_name("cross_language_marker.mjs")
                    completed = await asyncio.to_thread(
                        subprocess.run,
                        ["node", str(script), SERVER_ADDRESS, FLOW_TYPE, flow_id],
                        check=False,
                        timeout=120,
                    )
                    await client.stop_flow(flow_id)
                    if completed.returncode != 0:
                        sys.exit(completed.returncode)
            finally:
                await worker.stop()
                await worker_task
                cache.close()


if __name__ == "__main__":
    asyncio.run(run_cross_language_marker())
