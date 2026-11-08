# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Upstash Redis Python Flow with its generated connection."""

import dex

from dex_connectors.upstash_redis.redismock import Mock
from examples.key_lookup.flow import FLOW_TYPE, KeyLookupFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and example Flow agree on their identities."""
    flow = KeyLookupFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
