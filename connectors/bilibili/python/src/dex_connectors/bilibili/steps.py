# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public operation-specific Step factory."""

from ._generated.steps import new_get_authorized_user_step as new_get_authorized_user_step

new_get_authorized_user_step.__doc__ = (
    "Create the Bilibili Query Step; unwired optional branches fail the Flow."
)
