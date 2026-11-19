# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

# GetAccountBalanceInput has no parameters; the connection identifies the account.
@dataclass(frozen=True, slots=True)
class GetAccountBalanceInput:
    pass


setattr(GetAccountBalanceInput, '__module__', 'dex_connectors.yunpian.models')


# AccountBalance contains only the amount remaining in the Yunpian account.
@dataclass(frozen=True, slots=True)
class AccountBalance:
    # Balance is the remaining amount reported by Yunpian, in account currency units.
    balance: float = 0.0


setattr(AccountBalance, '__module__', 'dex_connectors.yunpian.models')
