# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetAccountInfoInput has no input because the API Key identifies its own account or quota.
@dataclass(frozen=True, slots=True)
class GetAccountInfoInput:
    pass


setattr(GetAccountInfoInput, '__module__', 'dex_connectors.apizero.models')


# GetAccountInfoOutput exposes only the documented value available to this Key scope.
@dataclass(frozen=True, slots=True)
class GetAccountInfoOutput:
    # Scope is account for a standard Key or key for a prepaid Key.
    scope: str = ''
    # BalanceYuan is the wallet balance in yuan for a standard Key only.
    balance_yuan: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'balanceYuan', 'dex_json_omit_default': True})
    # KeyQuotaRemaining is the remaining calls for a prepaid Key only.
    key_quota_remaining: int | None = _dataclass_field(default=None, metadata={'dex_json_name': 'keyQuotaRemaining', 'dex_json_omit_default': True})


setattr(GetAccountInfoOutput, '__module__', 'dex_connectors.apizero.models')
