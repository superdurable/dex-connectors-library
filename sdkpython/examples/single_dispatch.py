# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable local illustration of the single-dispatch provider boundary."""

import asyncio
from datetime import UTC, datetime, timedelta
from typing import Any, cast

import dex

from dex_connectors.sdk import Call, CallID, ConnectionRef, OperationRef
from dex_connectors.sdk.singledispatch import DecisionKind, Fence, Marker, State


class ExampleContext:
    """Inert public Dex heartbeat surface for this local example."""

    attempt = 1

    def __init__(self) -> None:
        self.deadline = datetime.now(UTC) + timedelta(seconds=10)
        self.heartbeats: list[object] = []

    def has_last_heartbeat_value(self) -> bool:
        """Report that this is the first Execute attempt."""
        return False

    def get_last_heartbeat_value(self, value_type: type[dict[Any, Any]]) -> None:
        """Return no earlier checkpoint for the example's first attempt."""
        return None

    async def heartbeat(self, value: object) -> None:
        """Capture a non-secret checkpoint exactly as Dex would persist it."""
        self.heartbeats.append(value)


async def main() -> None:
    """Claim once, send one inert write, and retain its provider identity."""
    context = ExampleContext()
    call = Call(
        CallID("5af8fafa-06ff-5b2d-bb13-04693809fabf"),
        ConnectionRef("sample", "main"), OperationRef("sample", "createItem"),
        context=cast(dex.AsyncContext, context),
    )
    fence = Fence(call, timedelta(seconds=10))
    assert (await fence.decide()).kind == DecisionKind.SEND_ALLOWED

    async def send(write_deadline: datetime) -> str:
        """Use the deadline when constructing a real non-replaying provider request."""
        assert write_deadline > datetime.now(UTC)
        return "INERT_PROVIDER_OBJECT"

    provider_id = await fence.dispatch(send)
    await fence.record_provider_id(provider_id)
    marker = Marker.from_wire(context.heartbeats[-1])
    assert marker.state == State.CLAIMED and marker.provider_id == provider_id


if __name__ == "__main__":
    asyncio.run(main())
