# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific Aqara Step factory."""

from ._generated.steps import new_list_devices_step as new_list_devices_step

new_list_devices_step.__doc__ = "Create the device Query Step; unwired optional branches fail."
