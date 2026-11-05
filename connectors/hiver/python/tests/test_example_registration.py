# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Validate the checked-in Hiver Flow's static connection and Step wiring."""

import dex
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

import dex_connectors.hiver as hiver
from examples.claim_conversation.flow import ClaimConversationFlow
from examples.inbox_page.flow import CONNECTION_NAME, InboxPageFlow


def test_example_registration() -> None:
    """Construct the public example with all generated operation bindings."""
    reference = ConnectionRef("hiver", CONNECTION_NAME)
    client = hiver.Client(
        hiver.Config(request_interval_milliseconds=1),
        StaticCredentialProvider({reference: hiver.Credentials(SecretString("inertHiverTestKey"))}),
    )
    flow = InboxPageFlow(hiver.new_connection(client, reference))
    assert flow.get_flow_type() == "HiverInboxPagePython"
    assert dex.Registry([flow], allow_async_handlers=True) is not None
    claim = ClaimConversationFlow(hiver.new_connection(client, reference))
    assert claim.get_flow_type() == "HiverClaimConversationPython"
    assert dex.Registry([claim], allow_async_handlers=True) is not None
