# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# GetTokenPlanRemainsInput requests the current Token Plan usage for the connection's key.
@dataclass(frozen=True, slots=True)
class GetTokenPlanRemainsInput:
    pass


setattr(GetTokenPlanRemainsInput, '__module__', 'dex_connectors.minimax.models')


# TokenPlanModelGroup is one provider-reported quota group.
@dataclass(frozen=True, slots=True)
class TokenPlanModelGroup:
    # ModelName is the provider's group label, not necessarily a callable model ID.
    model_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'modelName'})
    # IntervalRemainingPercent is MiniMax's 5-hour window percentage when supplied.
    interval_remaining_percent: float | None = _dataclass_field(default=None, metadata={'dex_json_name': 'intervalRemainingPercent', 'dex_json_omit_default': True})
    # WeeklyRemainingPercent is MiniMax's weekly window percentage when supplied.
    weekly_remaining_percent: float | None = _dataclass_field(default=None, metadata={'dex_json_name': 'weeklyRemainingPercent', 'dex_json_omit_default': True})
    # IntervalResetAtMillis is the 5-hour window end as Unix milliseconds.
    interval_reset_at_millis: int = _dataclass_field(default=0, metadata={'dex_json_name': 'intervalResetAtMillis'})
    # WeeklyResetAtMillis is the weekly window end as Unix milliseconds.
    weekly_reset_at_millis: int = _dataclass_field(default=0, metadata={'dex_json_name': 'weeklyResetAtMillis'})
    # IntervalStatus is MiniMax's raw 5-hour window status when supplied.
    interval_status: int | None = _dataclass_field(default=None, metadata={'dex_json_name': 'intervalStatus', 'dex_json_omit_default': True})
    # WeeklyStatus is MiniMax's raw weekly window status when supplied.
    weekly_status: int | None = _dataclass_field(default=None, metadata={'dex_json_name': 'weeklyStatus', 'dex_json_omit_default': True})


setattr(TokenPlanModelGroup, '__module__', 'dex_connectors.minimax.models')


# GetTokenPlanRemainsOutput contains provider percentages and reset times without inferring remaining calls.
@dataclass(frozen=True, slots=True)
class GetTokenPlanRemainsOutput:
    # ModelGroups is empty when MiniMax reports no quota groups.
    model_groups: tuple[TokenPlanModelGroup, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'modelGroups'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'model_groups', _replace_nil_slices(self.model_groups, 1))


GetTokenPlanRemainsOutput.__annotations__['model_groups'] = tuple[TokenPlanModelGroup, ...] | None
GetTokenPlanRemainsOutput.__dataclass_fields__['model_groups'].type = tuple[TokenPlanModelGroup, ...] | None
setattr(GetTokenPlanRemainsOutput, '__module__', 'dex_connectors.minimax.models')
