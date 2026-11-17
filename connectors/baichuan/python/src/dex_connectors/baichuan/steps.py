# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific Baichuan Step factory."""

from ._generated.steps import new_list_knowledge_bases_step as new_list_knowledge_bases_step

new_list_knowledge_bases_step.__doc__ = "Create the list Query Step; unwired optional branches fail the Flow."
