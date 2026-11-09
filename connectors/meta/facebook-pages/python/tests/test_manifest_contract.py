# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Facebook Pages Python Flow with a mock connection."""

import dex

from dex_connectors.meta_facebook_pages.facebookpagesmock import Mock
from examples.page_posts.flow import FLOW_TYPE, PagePostsFlow
from examples.post_publish.flow import FLOW_TYPE as PUBLISH_FLOW_TYPE
from examples.post_publish.flow import PostPublishFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and checked-in Flow agree on their Dex identities."""
    flow = PagePostsFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)


def test_publish_example_registers_with_mock_connection() -> None:
    """The publishing Step agrees with the declared Flow identity."""
    flow = PostPublishFlow(Mock().connection())
    assert flow.get_flow_type() == PUBLISH_FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
