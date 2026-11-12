# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Google Cloud Storage Python Flow with a mock connection."""

import json
from pathlib import Path

import dex

import dex_connectors.google_cloud_storage as google_cloud_storage
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


def test_list_objects_input_schema_requires_only_the_bucket() -> None:
    document = json.loads((Path(google_cloud_storage.__file__).parent / "dex-connector.json").read_text())
    schemas = {factory["operationId"]: factory["inputSchema"] for factory in document["factories"]}
    # Blank prefix lists every object, zero pageSize selects 100, and blank pageToken reads the first page.
    assert schemas["listObjects"]["required"] == ["bucket"]
