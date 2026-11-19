# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetBehaviorSummaryInput selects one completed reporting date.
@dataclass(frozen=True, slots=True)
class GetBehaviorSummaryInput:
    # Date is a past date in YYYY-MM-DD format, within 90 days; yesterday is available after 10:00 China Standard Time.
    date: str = ''


setattr(GetBehaviorSummaryInput, '__module__', 'dex_connectors.douyin.models')


# GetBehaviorSummaryOutput contains only aggregate mini app activity counts.
@dataclass(frozen=True, slots=True)
class GetBehaviorSummaryOutput:
    # ActiveUserCount is the number of active users in the requested interval.
    active_user_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'activeUserCount'})
    # NewUserCount is the number of new users in the requested interval.
    new_user_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'newUserCount'})
    # TotalUserCount is the reported total user count.
    total_user_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalUserCount'})
    # OpenCount is the number of mini app openings.
    open_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'openCount'})
    # ShareCount is the number of shares.
    share_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'shareCount'})


setattr(GetBehaviorSummaryOutput, '__module__', 'dex_connectors.douyin.models')
