# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

"""Register the Bitbucket example and preserve public mock identity."""

import pickle

import dex

from dex_connectors.atlassian_bitbucket.bitbucketmock import Mock, list_repositories_default_answer
from examples.resource_read.flow import FLOW_TYPE, ResourceReadFlow


def test_example_registers_with_mock_connection() -> None:
    flow = ResourceReadFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)


def test_mock_identity_and_helpers_remain_public() -> None:
    assert Mock().connection().reference.name == "bitbucket"
    assert pickle.loads(pickle.dumps(list_repositories_default_answer)) is list_repositories_default_answer
