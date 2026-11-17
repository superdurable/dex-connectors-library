# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific Chukou1 Step factory."""

from ._generated.steps import new_list_warehouses_step as new_list_warehouses_step

new_list_warehouses_step.__doc__ = (
    "Create the typed warehouse-list Step; unwired optional outcomes fail the Flow."
)
