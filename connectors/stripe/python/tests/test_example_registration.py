# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Verify the checked-in Stripe Flow accepts its mock connection."""

import dex

from dex_connectors.stripe.stripemock import Mock
from examples.create_card_checkout_session.flow import CardCheckoutFlow


def test_example_registers_with_mock_connection() -> None:
    flow = CardCheckoutFlow(Mock("stripe-payments").connection())
    dex.Registry([flow], allow_async_handlers=True)
