# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# CreateEventInput associates one event with a profile email and metric.
@dataclass(frozen=True, slots=True)
class CreateEventInput:
    # Email identifies the profile. Klaviyo silently drops some test-domain events after accepting them.
    email: str = ''
    # MetricName names the event metric.
    metric_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'metricName'})
    # Properties contains optional non-secret string event properties.
    properties: dict[str, str] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})


setattr(CreateEventInput, '__module__', 'dex_connectors.klaviyo.models')


# EventAcceptance confirms submission, not completion of asynchronous processing.
@dataclass(frozen=True, slots=True)
class EventAcceptance:
    # Accepted means Klaviyo returned HTTP 202.
    accepted: bool = False


setattr(EventAcceptance, '__module__', 'dex_connectors.klaviyo.models')


# GetProfileInput identifies a Klaviyo profile.
@dataclass(frozen=True, slots=True)
class GetProfileInput:
    # ID is Klaviyo's opaque profile ID.
    id: str = ''


setattr(GetProfileInput, '__module__', 'dex_connectors.klaviyo.models')


# Profile contains the identity fields returned for a Klaviyo profile.
@dataclass(frozen=True, slots=True)
class Profile:
    # ID is Klaviyo's opaque profile ID.
    id: str = ''
    # Email is the profile's email address, when present.
    email: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(Profile, '__module__', 'dex_connectors.klaviyo.models')
