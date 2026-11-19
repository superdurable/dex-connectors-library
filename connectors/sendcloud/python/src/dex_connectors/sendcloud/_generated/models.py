# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetAccountBalanceInput has no arguments; the credential selects the account.
@dataclass(frozen=True, slots=True)
class GetAccountBalanceInput:
    pass


setattr(GetAccountBalanceInput, '__module__', 'dex_connectors.sendcloud.models')


# GetAccountBalanceOutput contains provider-reported balances and quota counters without account contact information.
@dataclass(frozen=True, slots=True)
class GetAccountBalanceOutput:
    # AvailableBalance is SendCloud's avaliableBalance decimal value, preserved without a currency assumption.
    available_balance: str = _dataclass_field(default='', metadata={'dex_json_name': 'availableBalance'})
    # Balance is SendCloud's total balance decimal value, preserved without a currency assumption.
    balance: str = ''
    # DailyQuota is SendCloud's request allowance for the current day.
    daily_quota: int = _dataclass_field(default=0, metadata={'dex_json_name': 'dailyQuota'})
    # TodayUsedQuota is SendCloud's used request count for the current day.
    today_used_quota: int = _dataclass_field(default=0, metadata={'dex_json_name': 'todayUsedQuota'})


setattr(GetAccountBalanceOutput, '__module__', 'dex_connectors.sendcloud.models')
