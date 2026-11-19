# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetHolidayStatusInput identifies one Chinese calendar date in YYYY-MM-DD format.
@dataclass(frozen=True, slots=True)
class GetHolidayStatusInput:
    # Date is a single Gregorian calendar date in YYYY-MM-DD format.
    date: str = ''


setattr(GetHolidayStatusInput, '__module__', 'dex_connectors.tianapi.models')


# GetHolidayStatusOutput contains only the requested date and official day classifications.
@dataclass(frozen=True, slots=True)
class GetHolidayStatusOutput:
    # Date is the queried calendar date.
    date: str = ''
    # DayCode is 0 for a workday, 1 for a holiday, 2 for a weekend, or 3 for a makeup workday.
    day_code: int = _dataclass_field(default=0, metadata={'dex_json_name': 'dayCode'})
    # IsNotWork is 0 when working and 1 when resting.
    is_not_work: int = _dataclass_field(default=0, metadata={'dex_json_name': 'isNotWork'})


setattr(GetHolidayStatusOutput, '__module__', 'dex_connectors.tianapi.models')
