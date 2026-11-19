# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetWaybillBalanceInput needs no operation-specific values.
@dataclass(frozen=True, slots=True)
class GetWaybillBalanceInput:
    pass


setattr(GetWaybillBalanceInput, '__module__', 'dex_connectors.yunda.models')


# GetWaybillBalanceOutput contains remaining ordinary Baima waybill counts.
@dataclass(frozen=True, slots=True)
class GetWaybillBalanceOutput:
    # FreeCount is the number of free waybills remaining.
    free_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'freeCount'})
    # PaidCount is the number of paid waybills remaining.
    paid_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'paidCount'})


setattr(GetWaybillBalanceOutput, '__module__', 'dex_connectors.yunda.models')
