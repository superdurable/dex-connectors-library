# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetBalanceInput requires no selector; the API Key identifies the account.
@dataclass(frozen=True, slots=True)
class GetBalanceInput:
    pass


setattr(GetBalanceInput, '__module__', 'dex_connectors.tokenfactory.models')


# Balance contains the current TokenFactory balance and API Key status.
@dataclass(frozen=True, slots=True)
class Balance:
    # BalanceYuan is the account snapshot in Chinese yuan and can be negative.
    balance_yuan: float = _dataclass_field(default=0.0, metadata={'dex_json_name': 'balanceYuan'})
    # IsActive reports whether the authenticated API Key's account is active.
    is_active: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isActive'})
    # Unit is the fixed CNY currency code returned by TokenFactory.
    unit: str = ''


setattr(Balance, '__module__', 'dex_connectors.tokenfactory.models')
