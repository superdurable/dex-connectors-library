# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Record one event in the local Dex Web inbox, then exit before any delivery succeeds.

It behaves as a process that stops right after acknowledging a provider event.
"""

import asyncio
import sys
from datetime import UTC, datetime
from typing import Any

from dex_connectors.sdk import TriggerContext, TriggerEvent, prepare_trigger_delivery
from dex_connectors.sdk.projectconfig import (
    TriggerInboxKey,
    load_from_environment,
    new_durable_trigger_target,
)

KEY = TriggerInboxKey("sample", "workspace", "itemCreated", "start")


class StoppedWorker:
    """A target whose Worker has stopped."""

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[Any]) -> None:
        """Fail every delivery, so the event stays pending."""
        raise RuntimeError("Worker stopped")


async def record_and_stop(configuration_file: str, event_id: str, thread: str) -> None:
    """Record the event durably and leave it pending."""
    project = await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": configuration_file})
    target = new_durable_trigger_target(project.trigger_inbox(KEY), StoppedWorker())
    event = TriggerEvent(event_id, datetime.now(UTC), {"channel": "C1", "thread": thread})
    await prepare_trigger_delivery(TriggerContext(), target, event)
    try:
        await target.handle_trigger(TriggerContext(), event)
    except RuntimeError:
        return


if __name__ == "__main__":
    asyncio.run(record_and_stop(sys.argv[1], sys.argv[2], sys.argv[3]))
