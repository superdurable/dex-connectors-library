# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""GuGuData Chinese text segmentation Query for Dex Flows."""

from .client import Client, Config, Credentials, SegmentTextOperation
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import SegmentTextInput, SegmentTextOutput
from .steps import new_segment_text_step

__all__ = [
    "Client", "Config", "Credentials", "Connection", "SegmentTextInput",
    "SegmentTextOperation", "SegmentTextOutput", "new_connection",
    "new_connection_with_operations", "new_segment_text_step", "new_project_connection",
]
