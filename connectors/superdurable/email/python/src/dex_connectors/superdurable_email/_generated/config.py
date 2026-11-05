# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

from dex_connectors.sdk import SecretString


@dataclass(frozen=True, slots=True)
class Config:

    imap_host: str
    smtp_host: str
    from_address: str
    from_name: str
    imap_port: int = 993
    imap_security: str = "implicitTLS"
    smtp_port: int = 465
    smtp_security: str = "implicitTLS"

setattr(Config, '__module__', 'dex_connectors.superdurable_email.client')


@dataclass(frozen=True, slots=True)
class Credentials:

    username: str
    password: SecretString
    smtp_username: str
    smtp_password: SecretString

setattr(Credentials, '__module__', 'dex_connectors.superdurable_email.client')
