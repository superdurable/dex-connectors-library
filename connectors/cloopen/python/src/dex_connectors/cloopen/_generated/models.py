# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

# GetBalanceInput has no fields because the connected main account identifies the balance.
@dataclass(frozen=True, slots=True)
class GetBalanceInput:
    pass


setattr(GetBalanceInput, '__module__', 'dex_connectors.cloopen.models')


# GetBalanceOutput reports the provider's account balance without changing its units.
@dataclass(frozen=True, slots=True)
class GetBalanceOutput:
    # Balance is Cloopen's nonnegative decimal balance string in provider units.
    balance: str = ''


setattr(GetBalanceOutput, '__module__', 'dex_connectors.cloopen.models')
