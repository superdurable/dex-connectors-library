# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

# GetSubscriberCountInput has no user-supplied parameters.
@dataclass(frozen=True, slots=True)
class GetSubscriberCountInput:
    pass


setattr(GetSubscriberCountInput, '__module__', 'dex_connectors.wxpusher.models')


# SubscriberCountResult contains the provider's total number of subscription records.
@dataclass(frozen=True, slots=True)
class SubscriberCountResult:
    # Total is the number of subscription records; one user may create multiple records.
    total: int = 0


setattr(SubscriberCountResult, '__module__', 'dex_connectors.wxpusher.models')
