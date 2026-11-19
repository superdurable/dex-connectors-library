# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific Gitee Step factory."""

from ._generated.steps import new_get_repository_step as new_get_repository_step

new_get_repository_step.__doc__ = "Create the typed repository Step; unwired optional outcomes fail the Flow."
