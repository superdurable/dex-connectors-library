# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Workday Python example with a mock connection."""

import dex

from dex_connectors.workday.workdaymock import Mock
from examples.worker_inspection.flow import FLOW_TYPE, WorkerFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and handwritten Flow agree on their Dex identities."""
    flow = WorkerFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
