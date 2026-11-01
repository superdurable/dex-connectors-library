# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Klaviyo profiles and events."""

from dataclasses import dataclass, field


@dataclass(frozen=True, slots=True)
class GetProfileInput:
    """Klaviyo profile ID; it must not contain /, ?, &, or #."""

    id: str = ""


@dataclass(frozen=True, slots=True)
class Profile:
    """Profile ID and email address, which can be blank."""

    id: str = ""
    email: str = field(default="", metadata={"dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class CreateEventInput:
    """Profile email, metric name, and optional string event properties."""

    email: str = ""
    metric_name: str = field(default="", metadata={"dex_json_name": "metricName"})
    properties: dict[str, str] = field(default_factory=dict, metadata={"dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class EventAcceptance:
    """Whether Klaviyo accepted the event for asynchronous processing."""

    accepted: bool = False
