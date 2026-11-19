# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Go-derived Apifox request and summary models."""

from ._generated.models import GetProjectAPISummaryInput as GetProjectAPISummaryInput
from ._generated.models import GetProjectAPISummaryOutput as GetProjectAPISummaryOutput

GetProjectAPISummaryInput.__doc__ = "Positive decimal project ID from Apifox project settings."
GetProjectAPISummaryOutput.__doc__ = "Only title, version, and endpoint count; no document content."
