# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Register the runnable Google Contacts Python Flow with a mock connection."""

import dex

from dex_connectors.google_contacts.contactsmock import Mock
from examples.contacts.flow import FLOW_TYPE, ContactsFlow
from examples.creation.flow import FLOW_TYPE as CREATION_FLOW_TYPE
from examples.creation.flow import CreationFlow


def test_example_registers_with_mock_connection() -> None:
    """The generated Step and checked-in Flow agree on their Dex identities."""
    flow = ContactsFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)


def test_creation_example_registers_with_mock_connection() -> None:
    """The generated create Step and checked-in Flow agree on their Dex identities."""
    flow = CreationFlow(Mock().connection())
    assert flow.get_flow_type() == CREATION_FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
