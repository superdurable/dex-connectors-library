# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable local illustration of the single-dispatch provider boundary."""

import asyncio
from datetime import UTC, datetime, timedelta
from typing import cast

import dex

from dex_connectors.sdk import Call, CallID, ConnectionRef, OperationRef
from dex_connectors.sdk.singledispatch import DecisionKind, Fence, State


class ExampleContext:
    """Inert public Dex heartbeat surface for this local example."""

    def __init__(self, attempt: int = 1, previous_heartbeat: object | None = None) -> None:
        self.attempt = attempt
        self.deadline = datetime.now(UTC) + timedelta(seconds=10)
        self.previous_heartbeat = previous_heartbeat
        self.heartbeats: list[object] = []

    def has_last_heartbeat_value(self) -> bool:
        """Report whether the previous Execute attempt persisted a heartbeat."""
        return self.previous_heartbeat is not None

    def get_last_heartbeat_value(self, value_type: object) -> object:
        """Return the previous attempt's opaque heartbeat value."""
        return self.previous_heartbeat

    async def heartbeat(self, value: object) -> None:
        """Capture a non-secret checkpoint exactly as Dex would persist it."""
        self.heartbeats.append(value)


async def main() -> None:
    """Claim once, send one inert write, and read back its provider identity on retry."""
    context = ExampleContext()
    fence = Fence(_example_call(context), timedelta(seconds=10))
    assert (await fence.decide()).kind == DecisionKind.SEND_ALLOWED

    async def send(write_deadline: datetime) -> str:
        """Use the deadline when constructing a real non-replaying provider request."""
        assert write_deadline > datetime.now(UTC)
        return "INERT_PROVIDER_OBJECT"

    provider_id = await fence.dispatch(send)
    await fence.record_provider_id(provider_id)

    # The heartbeat format is internal; a retry learns the provider ID from its decision.
    retry_context = ExampleContext(2, context.heartbeats[-1])
    retry = await Fence(_example_call(retry_context), timedelta(seconds=10)).decide()
    assert retry.kind == DecisionKind.READ_BACK_REQUIRED and retry.marker is not None
    assert retry.marker.state == State.CLAIMED and retry.marker.provider_id == provider_id


def _example_call(context: ExampleContext) -> Call:
    return Call(
        CallID("5af8fafa-06ff-5b2d-bb13-04693809fabf"),
        ConnectionRef("sample", "main"), OperationRef("sample", "createItem"),
        context=cast(dex.AsyncContext, context),
    )


if __name__ == "__main__":
    asyncio.run(main())
