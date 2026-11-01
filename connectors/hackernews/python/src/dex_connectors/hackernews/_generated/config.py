# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated connection settings and credential values."""

from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class Config:
    """Hacker News Daily connection settings from the manifest."""

    base_url: str = "https://hacker-news.firebaseio.com/v0"
    timeout_seconds: float = 15
    max_response_bytes: int = 1048576

setattr(Config, '__module__', 'dex_connectors.hackernews.client')
