# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Gusto Python example with a mock connection."""

import dex

from dex_connectors.gusto.gustomock import Mock
from examples.employee.flow import FLOW_TYPE, EmployeeFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and handwritten Flow agree on their Dex identities."""
    flow = EmployeeFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
