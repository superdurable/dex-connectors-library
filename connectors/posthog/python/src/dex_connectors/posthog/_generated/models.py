# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# CaptureEventInput is one event with stable identity and time across retries.
@dataclass(frozen=True, slots=True)
class CaptureEventInput:
    # Event is a nonempty custom event name.
    event: str = ''
    # DistinctID is the stable person or anonymous identity.
    distinct_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'distinctId'})
    # Timestamp is the event time in RFC3339 format and must remain fixed across retries.
    timestamp: str = ''
    # Properties contains non-secret string dimensions, excluding identity and person mutations.
    properties: dict[str, str] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})


setattr(CaptureEventInput, '__module__', 'dex_connectors.posthog.models')


# CapturedEvent reports acceptance for processing, not confirmed ingestion.
@dataclass(frozen=True, slots=True)
class CapturedEvent:
    # EventID is the stable UUID sent to PostHog for retry deduplication.
    event_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'eventId'})
    # Accepted is true only when PostHog acknowledged the event without a quota limit.
    accepted: bool = False


setattr(CapturedEvent, '__module__', 'dex_connectors.posthog.models')


# ListInsightsInput selects a project and bounded offset page of saved insights.
@dataclass(frozen=True, slots=True)
class ListInsightsInput:
    # ProjectID is the positive ID shown in the PostHog project URL or settings.
    project_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'projectId'})
    # Offset is the zero-based page offset.
    offset: int = 0
    # Limit is the maximum count from 1 through 100; zero uses 20.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListInsightsInput, '__module__', 'dex_connectors.posthog.models')


# InsightMetadata contains only a saved insight's identity and display name.
@dataclass(frozen=True, slots=True)
class InsightMetadata:
    # ID is the stable numeric insight ID.
    id: int = 0
    # Name is the untrusted display name.
    name: str = ''


setattr(InsightMetadata, '__module__', 'dex_connectors.posthog.models')


# InsightsPage is one bounded saved-insight page without analysis results.
@dataclass(frozen=True, slots=True)
class InsightsPage:
    # Insights contains the selected metadata page.
    insights: tuple[InsightMetadata, ...] = ()
    # HasMore indicates a provider next page.
    has_more: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMore'})
    # Offset is the requested zero-based offset.
    offset: int = 0
    # Limit is the requested maximum count.
    limit: int = 0

    def __post_init__(self) -> None:
        object.__setattr__(self, 'insights', _replace_nil_slices(self.insights, 1))


InsightsPage.__annotations__['insights'] = tuple[InsightMetadata, ...] | None
InsightsPage.__dataclass_fields__['insights'].type = tuple[InsightMetadata, ...] | None
setattr(InsightsPage, '__module__', 'dex_connectors.posthog.models')
