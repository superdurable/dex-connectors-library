# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific eSign Step factory."""

from ._generated.steps import new_get_sign_flow_status_step as new_get_sign_flow_status_step

new_get_sign_flow_status_step.__doc__ = (
    "Create the typed reader Step; unwired optional outcomes fail the Flow."
)
