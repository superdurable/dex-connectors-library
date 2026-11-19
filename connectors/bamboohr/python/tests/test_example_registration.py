# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check the consuming example's static connection and Step registration."""

import dex
import pytest

from dex_connectors.bamboohr.bamboohrmock import Mock
from examples.get_employee.flow import CONNECTION_NAME, FLOW_TYPE, GetEmployeeFlow


def test_example_registers_manifest_operation_and_static_connection() -> None:
    """The runnable Flow binds its named BambooHR operation and result Step."""
    flow = GetEmployeeFlow(Mock(CONNECTION_NAME).connection())
    registry = dex.Registry([flow], allow_async_handlers=True)
    assert registry.flows == (flow,)
    assert flow.get_flow_type() == FLOW_TYPE


def test_example_refuses_another_connection_name() -> None:
    """A mismatch cannot silently select another project's credentials."""
    with pytest.raises(ValueError, match="static connection name"):
        GetEmployeeFlow(Mock("other").connection())
