# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public operation-specific Tuya Step factory."""

from ._generated.steps import new_get_device_details_step as new_get_device_details_step

new_get_device_details_step.__doc__ = "Create the Tuya Query Step; unwired optional branches fail the Flow."
