# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetTokenUsageInput has no input because the token identifies its own quota.
@dataclass(frozen=True, slots=True)
class GetTokenUsageInput:
    pass


setattr(GetTokenUsageInput, '__module__', 'dex_connectors.ip138.models')


# GetTokenUsageOutput contains only documented usage and quota counts.
@dataclass(frozen=True, slots=True)
class GetTokenUsageOutput:
    # CurrentHourRequests is the number of requests in the current hour.
    current_hour_requests: int = _dataclass_field(default=0, metadata={'dex_json_name': 'currentHourRequests'})
    # HourlyAllowance is the purchased number of calls per hour.
    hourly_allowance: int = _dataclass_field(default=0, metadata={'dex_json_name': 'hourlyAllowance'})
    # PackageQuota is the package call quota reported by iP138.
    package_quota: int = _dataclass_field(default=0, metadata={'dex_json_name': 'packageQuota'})


setattr(GetTokenUsageOutput, '__module__', 'dex_connectors.ip138.models')
