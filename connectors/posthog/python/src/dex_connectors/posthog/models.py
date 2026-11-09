# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed event and insight models generated from the Go provider contract."""

from ._generated.models import CapturedEvent as CapturedEvent
from ._generated.models import CaptureEventInput as CaptureEventInput
from ._generated.models import InsightMetadata as InsightMetadata
from ._generated.models import InsightsPage as InsightsPage
from ._generated.models import ListInsightsInput as ListInsightsInput

CaptureEventInput.__doc__ = "A stable event identity and RFC3339 timestamp for deduplicated capture."
CapturedEvent.__doc__ = "PostHog's acceptance for processing, with the stable provider UUID."
InsightMetadata.__doc__ = "The public identity and display name of one saved insight."
InsightsPage.__doc__ = "A bounded page of saved insight identities."
ListInsightsInput.__doc__ = "A positive project ID and zero-based offset page request."
