# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Wrike Python Flow with its generated connection."""

import dex

from dex_connectors.wrike.wrikemock import Mock
from examples.task_inspection.flow import FLOW_TYPE, TaskInspectionFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and example Flow agree on their identities."""
    flow = TaskInspectionFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
