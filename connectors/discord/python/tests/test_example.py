# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Validate the runnable Discord Flow registration and static connection."""

import dex
import pytest
from dex_connectors.sdk import ConnectionRef, SecretString, StaticCredentialProvider

from dex_connectors.discord import Client, Config, Credentials, new_connection
from examples.channel_announcement.flow import CONNECTION_NAME, ChannelAnnouncementFlow


def test_example_registers_with_static_connection() -> None:
    """Register generated Step types and the completed message Attribute."""
    reference = ConnectionRef("discord", CONNECTION_NAME)
    client = Client(
        Config(), StaticCredentialProvider({reference: Credentials(SecretString("inert.test.token"))})
    )
    flow = ChannelAnnouncementFlow(new_connection(client, reference))
    assert flow.get_flow_type() == "DiscordChannelAnnouncementPython"
    dex.Registry([flow], allow_async_handlers=True)


def test_example_rejects_wrong_connection_name() -> None:
    """Prevent a Flow from silently using another bot connection."""
    reference = ConnectionRef("discord", "other")
    client = Client(
        Config(), StaticCredentialProvider({reference: Credentials(SecretString("inert.test.token"))})
    )
    with pytest.raises(ValueError, match="static connection"):
        ChannelAnnouncementFlow(new_connection(client, reference))
