# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable DropboxSign Python Flow with its generated connection."""

import dex

from dex_connectors.dropbox_sign.dropboxsignmock import Mock
from examples.signature_request_inspection.flow import FLOW_TYPE, SignatureRequestInspectionFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and example Flow agree on their identities."""
    flow = SignatureRequestInspectionFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
