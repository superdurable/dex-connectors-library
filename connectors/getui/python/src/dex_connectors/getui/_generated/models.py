# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetDailyPushReportInput selects a historical date in YYYY-MM-DD format.
@dataclass(frozen=True, slots=True)
class GetDailyPushReportInput:
    # Date is a prior calendar date in YYYY-MM-DD format.
    date: str = ''


setattr(GetDailyPushReportInput, '__module__', 'dex_connectors.getui.models')


# GetDailyPushReportOutput contains aggregate push totals for one app and date.
@dataclass(frozen=True, slots=True)
class GetDailyPushReportOutput:
    # Date is the requested calendar date.
    date: str = ''
    # RequestCount is the number of push requests.
    request_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'requestCount'})
    # EligibleMessageCount is the number of messages eligible for delivery.
    eligible_message_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'eligibleMessageCount'})
    # SentCount is the number of messages sent.
    sent_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'sentCount'})
    # ReceivedCount is the number of messages received.
    received_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'receivedCount'})
    # DisplayedCount is the number displayed, or -1 when unavailable.
    displayed_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'displayedCount'})
    # ClickedCount is the number clicked.
    clicked_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'clickedCount'})


setattr(GetDailyPushReportOutput, '__module__', 'dex_connectors.getui.models')
