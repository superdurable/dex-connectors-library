# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetPushQuotaInput needs no operation-specific values.
@dataclass(frozen=True, slots=True)
class GetPushQuotaInput:
    pass


setattr(GetPushQuotaInput, '__module__', 'dex_connectors.oppo.models')


# GetPushQuotaOutput contains only today's OPPO Push counters.
@dataclass(frozen=True, slots=True)
class GetPushQuotaOutput:
    # TotalCount is the number of messages allowed today.
    total_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalCount'})
    # PushCount is the number already sent today.
    push_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pushCount'})
    # RemainCount is the number still allowed today.
    remain_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'remainCount'})


setattr(GetPushQuotaOutput, '__module__', 'dex_connectors.oppo.models')
