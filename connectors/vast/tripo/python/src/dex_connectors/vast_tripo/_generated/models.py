# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetAccountBalanceInput has no arguments; the connection identifies the account.
@dataclass(frozen=True, slots=True)
class GetAccountBalanceInput:
    pass


setattr(GetAccountBalanceInput, '__module__', 'dex_connectors.vast_tripo.models')


# GetAccountBalanceOutput retains exact decimal credit values from the provider's JSON numbers.
@dataclass(frozen=True, slots=True)
class GetAccountBalanceOutput:
    # AvailableCredits is the available balance as a nonnegative decimal string in credits.
    available_credits: str = _dataclass_field(default='', metadata={'dex_json_name': 'availableCredits'})
    # FrozenCredits is the reserved balance as a nonnegative decimal string in credits.
    frozen_credits: str = _dataclass_field(default='', metadata={'dex_json_name': 'frozenCredits'})


setattr(GetAccountBalanceOutput, '__module__', 'dex_connectors.vast_tripo.models')
