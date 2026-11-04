# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

"""Register the runnable Python example with a mock connection."""

import dex

from dex_connectors.netlify.netlifymock import Mock
from examples.netlify_lookup.flow import FLOW_TYPE, NetlifyLookupFlow


def test_example_registers_with_mock_connection() -> None:
    flow = NetlifyLookupFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
