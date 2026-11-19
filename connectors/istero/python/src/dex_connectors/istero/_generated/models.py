# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetAccountStatusInput requests the account state associated with the configured Token and ISID.
@dataclass(frozen=True, slots=True)
class GetAccountStatusInput:
    pass


setattr(GetAccountStatusInput, '__module__', 'dex_connectors.istero.models')


# GetAccountStatusOutput contains only the account ID and daily call counts.
@dataclass(frozen=True, slots=True)
class GetAccountStatusOutput:
    # ISID is the numeric ISAS account ID.
    isid: int = 0
    # TodayUsed is the number of calls made today.
    today_used: int = _dataclass_field(default=0, metadata={'dex_json_name': 'todayUsed'})
    # TodayRemaining is the remaining daily call allowance.
    today_remaining: int = _dataclass_field(default=0, metadata={'dex_json_name': 'todayRemaining'})


setattr(GetAccountStatusOutput, '__module__', 'dex_connectors.istero.models')
