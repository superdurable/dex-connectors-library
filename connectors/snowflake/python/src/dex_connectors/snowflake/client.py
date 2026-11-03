# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Snowflake SQL API transport, authorization, and connection settings."""

from __future__ import annotations

import base64
import hashlib
import json
import re
from collections.abc import Callable
from dataclasses import replace
from datetime import UTC, datetime, timedelta
from typing import TYPE_CHECKING
from urllib.parse import urlsplit

import httpx
from cryptography.exceptions import UnsupportedAlgorithm
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import padding, rsa
from cryptography.hazmat.primitives.hashes import SHA256
from dex_connectors.sdk import Call, CredentialProvider
from dex_connectors.sdk.providerhttp import (
    is_header_safe_credential,
    new_provider_http_client,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials

if TYPE_CHECKING:
    from .operations import CancelStatementOperation, GetStatementResultOperation, SubmitStatementOperation

_ORGANIZATION_ACCOUNT = re.compile(r"^[A-Za-z][A-Za-z0-9]*-[A-Za-z][A-Za-z0-9_]*$")
_ACCOUNT_LOCATOR = re.compile(r"^[A-Za-z][A-Za-z0-9]*(\.[A-Za-z0-9]+(-[A-Za-z0-9]+)*){0,2}$")
_CONTEXT_LIMIT = 255


class CredentialError(Exception):
    """A safe credential failure that must select the defect branch."""


class Client:
    """Immutable Snowflake settings and a call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        local_provider_url: str = "",
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Snowflake configuration and credential source are required")
        config = replace(
            config,
            statement_timeout_seconds=config.statement_timeout_seconds or 3600,
            max_rows=config.max_rows or 1000,
            max_response_bytes=config.max_response_bytes or 1048576,
        )
        account = config.account_identifier
        if (
            not account
            or account.strip() != account
            or len(account) > 253
            or any(character in account for character in "/:@?#")
            or "://" in account
            or "snowflakecomputing.com" in account.lower()
        ):
            raise ValueError("Snowflake accountIdentifier must be an account identifier alone")
        base_account = account.lower().removesuffix(".privatelink")
        first_segment = base_account.split(".", 1)[0]
        if not (
            _ORGANIZATION_ACCOUNT.fullmatch(base_account)
            or ("-" not in first_segment and _ACCOUNT_LOCATOR.fullmatch(base_account))
        ):
            raise ValueError("Snowflake accountIdentifier is invalid")
        self.jwt_account = first_segment.upper()
        self.base_url = "https://" + account.lower() + ".snowflakecomputing.com"
        if local_provider_url:
            parsed = urlsplit(local_provider_url)
            if (
                parsed.scheme not in ("http", "https")
                or parsed.hostname not in ("localhost", "127.0.0.1", "::1")
                or parsed.path
                or parsed.query
                or parsed.fragment
                or parsed.username
                or parsed.password
            ):
                raise ValueError("Snowflake local provider URL must be loopback without a path")
            self.base_url = local_provider_url.rstrip("/")
        for field in ("warehouse", "role", "database", "schema"):
            value = getattr(config, field)
            if value and (
                value.strip() != value
                or len(value.encode()) > _CONTEXT_LIMIT
                or '"' in value
                or any(ord(character) < 32 or ord(character) == 127 for character in value)
            ):
                raise ValueError(f"Snowflake {field} is invalid")
        if not 1 <= config.statement_timeout_seconds <= 604800:
            raise ValueError("Snowflake statementTimeoutSeconds is invalid")
        if not 1 <= config.max_rows <= 100000 or not 1 <= config.max_response_bytes <= 16777216:
            raise ValueError("Snowflake result limits are invalid")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(20)
        self.now = now or (lambda: datetime.now(UTC))

    def submit_statement(self) -> SubmitStatementOperation:
        """Bind the submitStatement Mutation."""
        from .operations import SubmitStatementOperation

        return SubmitStatementOperation(self)

    def get_statement_result(self) -> GetStatementResultOperation:
        """Bind the getStatementResult Query."""
        from .operations import GetStatementResultOperation

        return GetStatementResultOperation(self)

    def cancel_statement(self) -> CancelStatementOperation:
        """Bind the cancelStatement Mutation."""
        from .operations import CancelStatementOperation

        return CancelStatementOperation(self)

    async def aclose(self) -> None:
        """Close the provider HTTP client after Worker shutdown."""
        await self.http_client.aclose()

    async def send(self, call: Call, method: str, target: str, body: object | None = None) -> httpx.Response:
        """Send one authenticated SQL API request without redirects."""
        try:
            credentials = await self.credentials.resolve(call)
        except Exception as error:
            raise CredentialError("Snowflake connection credentials are unavailable") from error
        if credentials.auth_method_id == "programmatic-access-token":
            token = (
                credentials.programmatic_access_token.reveal()
                if credentials.programmatic_access_token
                else ""
            )
            if not is_header_safe_credential(token):
                raise CredentialError("the Snowflake programmatic access token is blank or invalid")
            token_type = "PROGRAMMATIC_ACCESS_TOKEN"
        elif credentials.auth_method_id == "key-pair":
            token = self._sign_key_pair_jwt(credentials)
            token_type = "KEYPAIR_JWT"
        else:
            raise CredentialError("the Snowflake connection selects no supported authentication method")
        headers = {
            "Authorization": "Bearer " + token,
            "X-Snowflake-Authorization-Token-Type": token_type,
            "Accept": "application/json",
            "User-Agent": "dex-snowflake-connector/1.0",
        }
        if body is not None:
            headers["Content-Type"] = "application/json"
        request = self.http_client.build_request(
            method,
            self.base_url + target,
            headers=headers,
            content=json.dumps(body, separators=(",", ":")).encode() if body is not None else None,
        )
        response, _ = await send_traced(self.http_client, request)
        return response

    def _sign_key_pair_jwt(self, credentials: Credentials) -> str:
        user = credentials.user or ""
        if (
            not user
            or user.strip() != user
            or len(user.encode()) > 255
            or '"' in user
            or any(ord(character) < 32 or ord(character) == 127 for character in user)
        ):
            raise CredentialError("the Snowflake key-pair user name is invalid")
        key_text = credentials.private_key.reveal() if credentials.private_key else ""
        if "BEGIN ENCRYPTED PRIVATE KEY" in key_text:
            raise CredentialError("the Snowflake private key is encrypted")
        try:
            key = serialization.load_pem_private_key(key_text.encode(), password=None)
        except (ValueError, TypeError, UnsupportedAlgorithm):
            raise CredentialError(
                "the Snowflake private key is not an unencrypted PEM RSA private key"
            ) from None
        if not isinstance(key, rsa.RSAPrivateKey):
            raise CredentialError("the Snowflake private key is not an RSA private key")
        public = key.public_key().public_bytes(
            serialization.Encoding.DER, serialization.PublicFormat.SubjectPublicKeyInfo
        )
        fingerprint = "SHA256:" + base64.b64encode(hashlib.sha256(public).digest()).decode()
        qualified = self.jwt_account + "." + user.upper()
        now = self.now().astimezone(UTC)
        claims = {
            "iss": qualified + "." + fingerprint,
            "sub": qualified,
            "iat": int(now.timestamp()),
            "exp": int((now + timedelta(minutes=59)).timestamp()),
        }

        def encode(value: bytes) -> str:
            return base64.urlsafe_b64encode(value).decode().rstrip("=")

        unsigned = (
            encode(b'{"alg":"RS256","typ":"JWT"}')
            + "."
            + encode(json.dumps(claims, separators=(",", ":")).encode())
        )
        signature = key.sign(unsigned.encode(), padding.PKCS1v15(), SHA256())
        return unsigned + "." + encode(signature)
