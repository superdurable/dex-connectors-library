# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific FluRouter Step factory."""

from ._generated.steps import new_list_models_step as new_list_models_step

new_list_models_step.__doc__ = "Create the typed model-list Step; unwired optional outcomes fail the Flow."
