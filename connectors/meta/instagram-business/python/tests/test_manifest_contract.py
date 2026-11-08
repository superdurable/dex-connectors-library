# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Instagram Business Python Flow with a mock connection."""

import dex

from dex_connectors.meta_instagram_business.instagrambusinessmock import Mock
from examples.media_list.flow import FLOW_TYPE, MediaListFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and checked-in Flow agree on their Dex identities."""
    flow = MediaListFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
