# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetBalanceInput has no fields because the connected domestic SMS account identifies the balance.
@dataclass(frozen=True, slots=True)
class GetBalanceInput:
    pass


setattr(GetBalanceInput, '__module__', 'dex_connectors.chuanglan.models')


# GetBalanceOutput reports the remaining number of domestic SMS messages.
@dataclass(frozen=True, slots=True)
class GetBalanceOutput:
    # RemainingMessages is the nonnegative message count returned by Chuanglan.
    remaining_messages: int = _dataclass_field(default=0, metadata={'dex_json_name': 'remainingMessages'})


setattr(GetBalanceOutput, '__module__', 'dex_connectors.chuanglan.models')
