# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific Dedao Step factory."""

from ._generated.steps import new_get_note_task_status_step as new_get_note_task_status_step

new_get_note_task_status_step.__doc__ = (
    "Create the typed reader Step; unwired optional outcomes fail the Flow."
)
