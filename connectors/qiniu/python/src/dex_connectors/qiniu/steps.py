# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific Qiniu Step factory."""

from ._generated.steps import new_list_buckets_step as new_list_buckets_step

new_list_buckets_step.__doc__ = "Create the typed bucket-list Step; unwired optional outcomes fail the Flow."
