# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

"""Register the runnable Python example with a mock connection."""

import dex

from dex_connectors.klaviyo.klaviyomock import Mock
from examples.profile_event.flow import FLOW_TYPE, ProfileEventFlow


def test_example_registers_with_mock_connection() -> None:
    flow = ProfileEventFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
