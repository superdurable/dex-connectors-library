# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable ECS regions Flow with a mock connection."""

import dex

from dex_connectors.alibaba.alibabamock import Mock
from examples.regions.flow import RegionsFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and handwritten Flow agree on Dex identities."""
    flow = RegionsFlow(Mock().connection())
    assert flow.get_flow_type() == "AlibabaCloudRegions"
    dex.Registry([flow], allow_async_handlers=True)
