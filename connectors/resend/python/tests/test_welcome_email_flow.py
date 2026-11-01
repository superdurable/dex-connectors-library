# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Construct the welcome email example against the manifest-backed mock."""

import dex

from dex_connectors.resend.resendmock import Mock
from examples.welcome_email.flow import (
    FLOW_TYPE,
    WelcomeEmailFlow,
    WelcomeEmailRequest,
    map_to_send_email_input,
)


def test_example_registers_with_mock_connection() -> None:
    flow = WelcomeEmailFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
    request = WelcomeEmailRequest("Acme <hello@example.test>", ("reader@example.test",), "Hi", "<p>Hi</p>")
    assert map_to_send_email_input(request).from_address == "Acme <hello@example.test>"
