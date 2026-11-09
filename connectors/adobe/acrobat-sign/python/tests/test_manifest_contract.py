# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable AcrobatSign Python Flow with its generated connection."""

import dex

from dex_connectors.adobe_acrobat_sign.adobe_acrobat_signmock import Mock
from examples.agreement_inspection.flow import FLOW_TYPE, AgreementInspectionFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and example Flow agree on their identities."""
    flow = AgreementInspectionFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
