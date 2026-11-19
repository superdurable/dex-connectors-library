# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public operation-specific Step factory."""

from ._generated.steps import new_describe_domain_step as new_describe_domain_step

new_describe_domain_step.__doc__ = (
    "Create the BaishanCloud Query Step; unwired optional branches fail the Flow."
)
