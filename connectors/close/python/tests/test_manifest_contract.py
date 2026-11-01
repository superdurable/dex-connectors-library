# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

"""Register the runnable Python example with a mock connection."""

import dex

from dex_connectors.close.closemock import Mock
from examples.close_lookup.flow import FLOW_TYPE, CloseLookupFlow


def test_example_registers_with_mock_connection() -> None:
    flow = CloseLookupFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
