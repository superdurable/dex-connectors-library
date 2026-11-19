# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

# GetServiceUsageInput has no parameters; the connection selects the storage service.
@dataclass(frozen=True, slots=True)
class GetServiceUsageInput:
    pass


setattr(GetServiceUsageInput, '__module__', 'dex_connectors.upyun.models')


# ServiceUsage is the number of bytes stored in the configured service.
@dataclass(frozen=True, slots=True)
class ServiceUsage:
    # Bytes is the nonnegative storage usage reported by UPYUN.
    bytes: int = 0


setattr(ServiceUsage, '__module__', 'dex_connectors.upyun.models')
