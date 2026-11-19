# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetAccountBalanceInput has no arguments; the connection identifies the tenant.
@dataclass(frozen=True, slots=True)
class GetAccountBalanceInput:
    pass


setattr(GetAccountBalanceInput, '__module__', 'dex_connectors.dieyuyun.models')


# GetAccountBalanceOutput contains only the available CNY yuan balance.
@dataclass(frozen=True, slots=True)
class GetAccountBalanceOutput:
    # AvailableCNY preserves the provider's decimal JSON number without floating-point rounding.
    available_cny: str = _dataclass_field(default='', metadata={'dex_json_name': 'availableCNY'})


setattr(GetAccountBalanceOutput, '__module__', 'dex_connectors.dieyuyun.models')
