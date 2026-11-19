# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetAccountQuotaInput has no arguments; the connection identifies the account.
@dataclass(frozen=True, slots=True)
class GetAccountQuotaInput:
    pass


setattr(GetAccountQuotaInput, '__module__', 'dex_connectors.knownsec_zoomeye.models')


# GetAccountQuotaOutput contains subscription and quota fields only, without account identity or contact details.
@dataclass(frozen=True, slots=True)
class GetAccountQuotaOutput:
    # Plan is the subscription plan name.
    plan: str = ''
    # GeneralPoints is the available ordinary point count, encoded by ZoomEye as a decimal string.
    general_points: str = _dataclass_field(default='', metadata={'dex_json_name': 'generalPoints'})
    # ZoomEyePoints is the available entitlement point count, encoded by ZoomEye as a decimal string.
    zoomeye_points: str = _dataclass_field(default='', metadata={'dex_json_name': 'zoomeyePoints'})


setattr(GetAccountQuotaOutput, '__module__', 'dex_connectors.knownsec_zoomeye.models')
