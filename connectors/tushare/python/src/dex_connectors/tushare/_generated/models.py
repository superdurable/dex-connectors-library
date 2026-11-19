# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetTradingDayInput identifies one exchange and calendar date.
@dataclass(frozen=True, slots=True)
class GetTradingDayInput:
    # Exchange is SSE, SZSE, or BSE.
    exchange: str = ''
    # Date is a calendar date in YYYYMMDD format.
    date: str = ''


setattr(GetTradingDayInput, '__module__', 'dex_connectors.tushare.models')


# GetTradingDayOutput contains only trading calendar fields.
@dataclass(frozen=True, slots=True)
class GetTradingDayOutput:
    # Exchange is the securities exchange code.
    exchange: str = ''
    # Date is the requested date in YYYYMMDD format.
    date: str = ''
    # IsOpen indicates whether the exchange traded on this date.
    is_open: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isOpen'})
    # PreviousTradingDate is the most recent prior trading date when supplied.
    previous_trading_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'previousTradingDate'})


setattr(GetTradingDayOutput, '__module__', 'dex_connectors.tushare.models')
