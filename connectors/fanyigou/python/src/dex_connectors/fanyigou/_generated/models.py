# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetAccountBalanceInput needs no operation-specific values.
@dataclass(frozen=True, slots=True)
class GetAccountBalanceInput:
    pass


setattr(GetAccountBalanceInput, '__module__', 'dex_connectors.fanyigou.models')


# GetAccountBalanceOutput contains document-translation resource balance.
@dataclass(frozen=True, slots=True)
class GetAccountBalanceOutput:
    # DeductType is 0 for points or 1 for pages.
    deduct_type: int = _dataclass_field(default=0, metadata={'dex_json_name': 'deductType'})
    # Balance is the remaining quantity in the units selected by DeductType.
    balance: int = 0


setattr(GetAccountBalanceOutput, '__module__', 'dex_connectors.fanyigou.models')
