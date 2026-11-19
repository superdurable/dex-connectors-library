# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public operation-specific Step factory."""

from ._generated.steps import new_list_campaigns_step as new_list_campaigns_step

new_list_campaigns_step.__doc__ = (
    "Create the Sensors Data Query Step; unwired optional branches fail the Flow."
)
