# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Azure Blob Storage Python Flow with its generated connection."""

import dex

from dex_connectors.microsoft_azure_blob.azureblobmock import Mock
from examples.blob_download.flow import FLOW_TYPE, BlobDownloadFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and example Flow agree on their identities."""
    flow = BlobDownloadFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
