# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetAccountBalanceInput has no arguments; the connection identifies the account.
@dataclass(frozen=True, slots=True)
class GetAccountBalanceInput:
    pass


setattr(GetAccountBalanceInput, '__module__', 'dex_connectors.textin.models')


# GetAccountBalanceOutput contains only the available T-coin amount and payment type.
@dataclass(frozen=True, slots=True)
class GetAccountBalanceOutput:
    # AvailableTCoins preserves the provider's decimal JSON number without floating-point rounding.
    available_t_coins: str = _dataclass_field(default='', metadata={'dex_json_name': 'availableTCoins'})
    # PayType is 1 for prepaid and 2 for postpaid.
    pay_type: int = _dataclass_field(default=0, metadata={'dex_json_name': 'payType'})


setattr(GetAccountBalanceOutput, '__module__', 'dex_connectors.textin.models')
