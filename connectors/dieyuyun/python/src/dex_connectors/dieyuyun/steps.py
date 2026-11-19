# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific Dieyuyun Step factory."""

from ._generated.steps import new_get_account_balance_step as new_get_account_balance_step

new_get_account_balance_step.__doc__ = (
    "Create the typed reader Step; unwired optional outcomes fail the Flow."
)
