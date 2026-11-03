# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""One native TDS connection per SQL Server operation attempt."""

from __future__ import annotations

import math
import os
import socket
import ssl
import threading
from collections.abc import Callable
from dataclasses import replace
from ipaddress import ip_address
from typing import TYPE_CHECKING, Any

import certifi
import pytds  # type: ignore[import-untyped]
from cryptography import x509
from dex_connectors.sdk import CredentialProvider
from pytds import tls

from ._generated.config import Config, Credentials
from ._generated.definitions import CONNECTOR_ID, EXECUTE_DEFINITION, QUERY_DEFINITION

if TYPE_CHECKING:
    from .operations import ExecuteStatementOperation, QueryRowsOperation, _AttemptDeadline

_SESSION_SETTINGS = (
    "SET LANGUAGE us_english",
    "SET DATEFORMAT ymd",
    "SET XACT_ABORT ON",
    "SET NOCOUNT OFF",
    "SET IMPLICIT_TRANSACTIONS OFF",
    "SET ANSI_NULLS ON",
    "SET ANSI_PADDING ON",
    "SET ANSI_WARNINGS ON",
    "SET ARITHABORT ON",
    "SET CONCAT_NULL_YIELDS_NULL ON",
    "SET QUOTED_IDENTIFIER ON",
    "SET NUMERIC_ROUNDABORT OFF",
    "SET TEXTSIZE 2147483647",
    "SET TRANSACTION ISOLATION LEVEL READ COMMITTED",
)
_TLS_HOST_VALIDATION = threading.local()
_ORIGINAL_HOST_VALIDATOR = tls.validate_host
_ORIGINAL_SOCKET_CONNECTION = socket.create_connection


def _verify_server_hostname(cert: Any, name: bytes) -> bool:
    certificate = cert.to_cryptography()
    try:
        subject_alternative_names = certificate.extensions.get_extension_for_class(
            x509.SubjectAlternativeName
        ).value
    except x509.ExtensionNotFound:
        return False
    try:
        host = name.decode("ascii")
    except UnicodeDecodeError:
        return False
    try:
        address = ip_address(host)
    except ValueError:
        host_labels = host.lower().split(".")
        for dns_name in subject_alternative_names.get_values_for_type(x509.DNSName):
            pattern_labels = dns_name.lower().split(".")
            if len(pattern_labels) == len(host_labels) and (
                pattern_labels == host_labels
                or (pattern_labels[0] == "*" and pattern_labels[1:] == host_labels[1:])
            ):
                return True
        return False
    return address in subject_alternative_names.get_values_for_type(x509.IPAddress)


def _dispatch_host_validation(cert: Any, name: bytes) -> bool:
    if getattr(_TLS_HOST_VALIDATION, "is_sql_server_connection", False):
        return _verify_server_hostname(cert, name)
    return bool(_ORIGINAL_HOST_VALIDATOR(cert, name))


tls.validate_host = _dispatch_host_validation


def _dispatch_socket_connection(*args: Any, **kwargs: Any) -> socket.socket:
    deadline = getattr(_TLS_HOST_VALIDATION, "deadline", None)
    if deadline is not None:
        deadline.check()
        arguments = list(args)
        if len(arguments) > 1:
            arguments[1] = min(arguments[1], deadline.remaining_seconds())
        elif "timeout" in kwargs:
            kwargs["timeout"] = min(kwargs["timeout"], deadline.remaining_seconds())
        args = tuple(arguments)
    connected_socket = _ORIGINAL_SOCKET_CONNECTION(*args, **kwargs)
    if deadline is not None:
        deadline.attach_socket(connected_socket)
    return connected_socket


socket.create_connection = _dispatch_socket_connection


class Client:
    """Validate settings and open one authenticated TDS session for each call.

    The credential provider resolves the password immediately before each attempt.
    ``connection_factory`` is constructor injection for native-driver tests.
    """

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: object | None = None,
        *,
        connection_factory: Callable[..., Any] = pytds.connect,
    ) -> None:
        if config is None or credentials is None or connection_factory is None:
            raise ValueError("SQL Server configuration, credentials, and driver are required")
        if http_client is not None:
            raise ValueError("SQL Server uses a native TDS driver, not an HTTP client")
        defaults = Config(config.host, config.database, config.user)
        config = replace(
            config,
            port=config.port or defaults.port,
            encrypt=config.encrypt or defaults.encrypt,
            connect_timeout_seconds=config.connect_timeout_seconds or defaults.connect_timeout_seconds,
            statement_timeout_seconds=config.statement_timeout_seconds or defaults.statement_timeout_seconds,
            max_rows=config.max_rows or defaults.max_rows,
            max_response_bytes=config.max_response_bytes or defaults.max_response_bytes,
        )
        if (
            not config.host
            or config.host != config.host.strip()
            or any(character in config.host for character in "/\\,;@'\"= ")
            or any(ord(character) < 32 or ord(character) == 127 for character in config.host)
            or "://" in config.host
            or config.host.lower().startswith("tcp:")
        ):
            raise ValueError("SQL Server host must be one DNS name or IP address without a port or instance")
        if ":" in config.host:
            try:
                ip_address(config.host)
            except ValueError as error:
                raise ValueError("SQL Server host must not include a port") from error
            if "%" in config.host:
                raise ValueError("SQL Server host must be an IP address without a scope identifier")
        if not 1 <= config.port <= 65535:
            raise ValueError("SQL Server port must be from 1 through 65535")
        if (
            any(
                not value
                or value != value.strip()
                or len(value) > 128
                or any(ord(char) < 32 for char in value)
                for value in (config.database, config.user)
            )
            or "\\" in config.user
        ):
            raise ValueError("SQL Server database and SQL authentication user are required")
        if (
            config.encrypt == "disable"
            and config.host != "localhost"
            and not (_is_loopback_address(config.host))
        ):
            raise ValueError("SQL Server encrypt disable is allowed only for a loopback host")
        if config.encrypt in ("strict", "trust-server-certificate"):
            raise ValueError(f"SQL Server Python driver does not support encrypt={config.encrypt}")
        if config.encrypt not in ("mandatory", "disable"):
            raise ValueError("SQL Server encrypt mode is invalid")
        if (
            not math.isfinite(config.connect_timeout_seconds)
            or not math.isfinite(config.statement_timeout_seconds)
            or config.connect_timeout_seconds <= 0
            or config.statement_timeout_seconds < 0.001
        ):
            raise ValueError("SQL Server timeouts must be positive; statementTimeout is at least 1ms")
        if int(config.statement_timeout_seconds * 1000 + 0.999) > (1 << 31) - 1:
            raise ValueError("SQL Server statementTimeout exceeds SQL Server LOCK_TIMEOUT range")
        if not 1 <= config.max_rows <= 100_000 or not 1 <= config.max_response_bytes <= 16 << 20:
            raise ValueError("SQL Server row and response bounds are invalid")
        self.config = config
        self.credentials = credentials
        self.connection_factory = connection_factory

    def query(self) -> QueryRowsOperation:
        """Bind the read operation to this client."""
        from .operations import QueryRowsOperation

        return QueryRowsOperation(self)

    def execute(self) -> ExecuteStatementOperation:
        """Bind the transactional write operation to this client."""
        from .operations import ExecuteStatementOperation

        return ExecuteStatementOperation(self)

    async def aclose(self) -> None:
        """Release client resources; individual calls already close their sessions."""

    def open_connection(self, password: str, deadline: _AttemptDeadline | None = None) -> Any:
        """Open and pin a native session with per-connection certificate verification."""
        ca_file: str | None = None
        if self.config.encrypt == "mandatory":
            configured_ca_file = os.environ.get("SQLSERVER_SSL_CA")
            ca_file = configured_ca_file or ssl.get_default_verify_paths().cafile
            if not configured_ca_file and (not ca_file or not os.path.isfile(ca_file)):
                ca_file = certifi.where()
            if not ca_file or not os.path.isfile(ca_file):
                raise ValueError("SQL Server trusted CA bundle is unavailable")
            try:
                trust_context = ssl.create_default_context(cafile=ca_file)
                if trust_context.cert_store_stats()["x509"] == 0:
                    raise ValueError("SQL Server trusted CA bundle has no certificates")
            except (OSError, ValueError, ssl.SSLError) as error:
                raise ValueError("SQL Server trusted CA bundle is unreadable") from error
        attempt_timeout_seconds = (
            self.config.connect_timeout_seconds + self.config.statement_timeout_seconds + 5
        )
        connection_settings = dict(
            server=self.config.host,
            port=self.config.port,
            database=self.config.database,
            user=self.config.user,
            password=password,
            login_timeout=self.config.connect_timeout_seconds,
            timeout=attempt_timeout_seconds,
            cafile=ca_file,
            validate_host=True,
            enc_login_only=False,
            disable_connect_retry=True,
            autocommit=False,
            appname="dex-sql-server-connector",
        )
        if self.connection_factory is pytds.connect:
            # pytds performs this check after the handshake and before it sends login credentials.
            _TLS_HOST_VALIDATION.is_sql_server_connection = self.config.encrypt == "mandatory"
            _TLS_HOST_VALIDATION.deadline = deadline
            try:
                connection = self.connection_factory(**connection_settings)
            finally:
                _TLS_HOST_VALIDATION.is_sql_server_connection = False
                _TLS_HOST_VALIDATION.deadline = None
        else:
            connection = self.connection_factory(**connection_settings)
        try:
            if deadline is not None:
                deadline.attach_connection(connection)
                deadline.check()
            cursor = connection.cursor()
            lock_timeout = int(self.config.statement_timeout_seconds * 1000 + 0.999)
            settings = (*_SESSION_SETTINGS, f"SET LOCK_TIMEOUT {lock_timeout}")
            if self.connection_factory is pytds.connect:
                from .operations import _InvalidResponse

                identity_statement = (
                    "SELECT @@SPID, CONVERT(nvarchar(128), SERVERPROPERTY('ProductVersion'))"
                )
                cursor.execute("; ".join((*settings, identity_statement)))
                identity = cursor.fetchone()
                if (
                    identity is None
                    or len(identity) != 2
                    or type(identity[0]) is not int
                    or identity[0] < 0
                    or not isinstance(identity[1], str)
                ):
                    raise _InvalidResponse("SQL Server did not return a valid session identity")
                while cursor.nextset():
                    raise _InvalidResponse("SQL Server returned multiple session identity result sets")
                connection._dex_receipt_metadata = {}
                process_id, server_version = str(identity[0]), str(identity[1])
                if process_id.isdecimal() and len(process_id) <= 20:
                    connection._dex_receipt_metadata["processId"] = process_id
                if 0 < len(server_version) <= 128 and all(32 <= ord(char) <= 126 for char in server_version):
                    connection._dex_receipt_metadata["serverVersion"] = server_version
            else:
                for setting in settings:
                    cursor.execute(setting)
            if deadline is not None:
                deadline.check()
            cursor.close()
        except Exception:
            connection.close()
            raise
        return connection


def _is_loopback_address(host: str) -> bool:
    try:
        return ip_address(host).is_loopback
    except ValueError:
        return False


__all__ = ["Client", "Config", "Credentials", "CONNECTOR_ID", "EXECUTE_DEFINITION", "QUERY_DEFINITION"]
