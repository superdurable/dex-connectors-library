# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific DataEyes Step factory."""

from ._generated.steps import new_read_web_page_step as new_read_web_page_step

new_read_web_page_step.__doc__ = "Create the typed reader Step; unwired optional outcomes fail the Flow."
