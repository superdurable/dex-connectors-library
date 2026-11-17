# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific FastGPT Step factory."""

from ._generated.steps import new_get_dataset_details_step as new_get_dataset_details_step

new_get_dataset_details_step.__doc__ = (
    "Create the typed reader Step; unwired optional outcomes fail the Flow."
)
