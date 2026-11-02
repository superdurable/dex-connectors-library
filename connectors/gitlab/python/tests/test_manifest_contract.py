# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

"""Register the runnable Python example with a mock connection."""

import dex

from dex_connectors.gitlab.gitlabmock import Mock
from examples.gitlab_lookup.flow import FLOW_TYPE, GitLabLookupFlow


def test_example_registers_with_mock_connection() -> None:
    flow = GitLabLookupFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
