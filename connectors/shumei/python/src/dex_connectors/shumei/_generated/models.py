# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetBalanceInput requires no selector; the accessKey identifies the account.
@dataclass(frozen=True, slots=True)
class GetBalanceInput:
    pass


setattr(GetBalanceInput, '__module__', 'dex_connectors.shumei.models')


# Balance contains Shumei's current SaaS account balance.
@dataclass(frozen=True, slots=True)
class Balance:
    # BalanceYuan is the provider-reported amount in Chinese yuan.
    balance_yuan: float = _dataclass_field(default=0.0, metadata={'dex_json_name': 'balanceYuan'})


setattr(Balance, '__module__', 'dex_connectors.shumei.models')
