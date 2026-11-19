# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public models generated from the Go connector contract."""

from ._generated.models import QueryWaybillTraceInput as QueryWaybillTraceInput
from ._generated.models import WaybillEvent as WaybillEvent
from ._generated.models import WaybillTrace as WaybillTrace

QueryWaybillTraceInput.__doc__ = "JD Logistics QueryWaybillTraceInput model."
WaybillTrace.__doc__ = "JD Logistics WaybillTrace model."
WaybillEvent.__doc__ = "JD Logistics tracking scan without personal details."
