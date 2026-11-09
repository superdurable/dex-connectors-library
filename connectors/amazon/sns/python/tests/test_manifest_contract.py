# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Amazon SNS Python Flow with its generated connection."""

import dex

from dex_connectors.amazon_sns.snsmock import Mock
from examples.topic_listing.flow import FLOW_TYPE, TopicListingFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and example Flow agree on their identities."""
    flow = TopicListingFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
