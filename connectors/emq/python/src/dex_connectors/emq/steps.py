# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific EMQX Cloud Step factory."""

from ._generated.steps import new_list_deployments_step as new_list_deployments_step

new_list_deployments_step.__doc__ = (
    "Create the typed warehouse-list Step; unwired optional outcomes fail the Flow."
)
