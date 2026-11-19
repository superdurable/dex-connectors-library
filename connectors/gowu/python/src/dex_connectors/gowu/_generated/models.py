# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetAccessClientInput requests information for the app bound to the connection.
@dataclass(frozen=True, slots=True)
class GetAccessClientInput:
    pass


setattr(GetAccessClientInput, '__module__', 'dex_connectors.gowu.models')


# AccessClientResult contains the app remark and cumulative API call count.
@dataclass(frozen=True, slots=True)
class AccessClientResult:
    # Remark is the provider's optional application description.
    remark: str = ''
    # APICallCount is the cumulative API call count reported by Gowu.
    api_call_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'apiCallCount'})


setattr(AccessClientResult, '__module__', 'dex_connectors.gowu.models')
