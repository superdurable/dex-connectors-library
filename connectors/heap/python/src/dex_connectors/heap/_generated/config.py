# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class Config:

    app_id: str
    base_url: str = "https://heapanalytics.com"
    timeout_seconds: float = 15
    max_response_bytes: int = 1048576

setattr(Config, '__module__', 'dex_connectors.heap.client')
