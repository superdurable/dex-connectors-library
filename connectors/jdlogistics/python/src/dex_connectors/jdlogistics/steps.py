# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public operation-specific Step factory."""

from ._generated.steps import new_query_waybill_trace_step as new_query_waybill_trace_step

new_query_waybill_trace_step.__doc__ = (
    "Create the JD Logistics Query Step; unwired optional branches fail the Flow."
)
