# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific FIT2CLOUD Step factory."""

from ._generated.steps import new_list_posts_step as new_list_posts_step

new_list_posts_step.__doc__ = "Create the typed article-list Step; unwired optional outcomes fail the Flow."
