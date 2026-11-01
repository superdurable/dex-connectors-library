# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

"""Register the runnable Python example with a mock connection."""

import dex

from dex_connectors.sentry.sentrymock import Mock
from examples.issue_review.flow import FLOW_TYPE, IssueReviewFlow


def test_example_registers_with_mock_connection() -> None:
    flow = IssueReviewFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
