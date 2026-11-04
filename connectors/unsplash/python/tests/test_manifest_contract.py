# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

"""Register the runnable Python example with a mock connection."""

import dex

from dex_connectors.unsplash.unsplashmock import Mock
from examples.photo_search.flow import FLOW_TYPE, PhotoSearchFlow


def test_example_registers_with_mock_connection() -> None:
    flow = PhotoSearchFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
