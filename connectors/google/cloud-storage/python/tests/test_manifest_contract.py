# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Google Cloud Storage Python Flow with a mock connection."""

import dex

from dex_connectors.google_cloud_storage.cloudstoragemock import Mock
from examples.download.flow import FLOW_TYPE as DOWNLOAD_FLOW_TYPE
from examples.download.flow import DownloadFlow
from examples.objects.flow import FLOW_TYPE, ObjectsFlow
from examples.upload.flow import FLOW_TYPE as UPLOAD_FLOW_TYPE
from examples.upload.flow import UploadFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and checked-in Flow agree on their Dex identities."""
    flow = ObjectsFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)


def test_download_example_registers_with_mock_connection() -> None:
    """The generated download Step and checked-in Flow agree on their Dex identities."""
    flow = DownloadFlow(Mock().connection())
    assert flow.get_flow_type() == DOWNLOAD_FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)


def test_upload_example_registers_with_mock_connection() -> None:
    """The generated upload Step and checked-in Flow agree on their Dex identities."""
    flow = UploadFlow(Mock().connection())
    assert flow.get_flow_type() == UPLOAD_FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
