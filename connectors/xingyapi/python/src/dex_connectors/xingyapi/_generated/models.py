# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetInstanceOnlineStatusInput identifies an existing managed Enterprise WeChat instance.
@dataclass(frozen=True, slots=True)
class GetInstanceOnlineStatusInput:
    # InstanceGUID is the inst_xxxx identifier shown on the provider's 实例 page.
    instance_guid: str = _dataclass_field(default='', metadata={'dex_json_name': 'instanceGUID'})


setattr(GetInstanceOnlineStatusInput, '__module__', 'dex_connectors.xingyapi.models')


# GetInstanceOnlineStatusOutput reports whether a managed account is online.
@dataclass(frozen=True, slots=True)
class GetInstanceOnlineStatusOutput:
    # IsOnline is true when the provider returns code 0 and a nonzero account ID.
    is_online: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isOnline'})
    # AccountID is the provider's decimal uin when online; it is empty when offline.
    account_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'accountID'})


setattr(GetInstanceOnlineStatusOutput, '__module__', 'dex_connectors.xingyapi.models')
