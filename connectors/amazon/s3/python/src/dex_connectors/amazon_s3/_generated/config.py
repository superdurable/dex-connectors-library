# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    endpoint: str
    default_bucket: str
    region: str = "us-east-1"
    addressing_style: str = "auto"
    list_page_size: int = 100
    max_response_bytes: int = 4194304
    max_text_bytes: int = 1048576
    max_upload_bytes: int = 1048576

setattr(Config, '__module__', 'dex_connectors.amazon_s3.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    access_key_id: SecretString
    secret_access_key: SecretString
    session_token: SecretString

setattr(Credentials, '__module__', 'dex_connectors.amazon_s3.client')
