# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify the checked-in Gmail Flow accepts its mock connection."""

import dex

from dex_connectors.google_gmail.gmailmock import Mock
from examples.thread_reply.flow import ThreadReplyFlow


def test_example_registers_with_mock_connection() -> None:
    flow = ThreadReplyFlow(Mock("gmail-inbox").connection)
    dex.Registry([flow], allow_async_handlers=True)
