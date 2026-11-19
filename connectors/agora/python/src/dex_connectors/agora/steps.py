# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific Agora Step factory."""

from ._generated.steps import new_list_running_agents_step as new_list_running_agents_step

new_list_running_agents_step.__doc__ = (
    "Create the typed agent-list Step; an unwired optional outcome fails the Flow."
)
