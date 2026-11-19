# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetChargeStatusInput identifies a Charge created in the API Key's test or live environment.
@dataclass(frozen=True, slots=True)
class GetChargeStatusInput:
    # ChargeID is the Ping++ Charge identifier, beginning with ch_.
    charge_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'chargeId'})


setattr(GetChargeStatusInput, '__module__', 'dex_connectors.pingpp.models')


# GetChargeStatusOutput contains only payment-state flags, not payment credentials or customer details.
@dataclass(frozen=True, slots=True)
class GetChargeStatusOutput:
    # ChargeID is the verified Charge identifier.
    charge_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'chargeId'})
    # Paid indicates whether Ping++ recorded a successful payment.
    paid: bool = False
    # Refunded indicates whether the Charge has refund information.
    refunded: bool = False
    # Reversed indicates whether the Charge was reversed.
    reversed: bool = False
    # LiveMode indicates whether this Charge belongs to the live environment.
    live_mode: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'liveMode'})


setattr(GetChargeStatusOutput, '__module__', 'dex_connectors.pingpp.models')
