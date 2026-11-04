# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-declared OAuth and service-account token grants for connector credentials."""

import json
import re
from collections.abc import Callable, Mapping
from dataclasses import replace
from datetime import UTC, datetime, timedelta
from typing import Any, Generic, TypeVar, cast
from urllib.parse import quote

import httpx

from ..credentials import (
    CredentialRefreshResult,
    CredentialRefreshState,
    ReauthorizationRequiredError,
    SecretString,
)
from . import (
    ClientAuthenticationMethod,
    ClientCredentials,
    JWTBearerAssertion,
    MissingExpiryRule,
    TokenEndpoint,
    is_refresh_required,
    parse_rsa_private_key_pem,
    sign_jwt_bearer_assertion,
)

Credential = TypeVar("Credential")
_DURATION_PART = re.compile(r"((?:\d+(?:\.\d*)?|\.\d+))(ns|us|µs|μs|ms|s|m|h)")


class ManifestCredentialRefreshDriver(Generic[Credential]):
    """Exchange grants described by a validated connector manifest without owning storage.

    ``auth`` is the manifest's ``spec.auth`` JSON object. ``credential_fields``
    maps its wire field names to attributes on a frozen credential dataclass.
    Credentials for a multi-method manifest carry ``auth_method_id``; an empty
    value selects ``defaultMethod``. The host
    owns the HTTP client, refresh admission, and complete replacement.
    """

    def __init__(
        self,
        provider_name: str,
        auth: Mapping[str, Any],
        credential_fields: Mapping[str, str],
        http_client: httpx.AsyncClient,
        *,
        settings: Mapping[str, str] | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        """Configure a driver with manifest auth, credential fields, and an owned HTTP client.

        ``settings`` supplies connection settings read by manifest value sources.
        ``now`` supplies the UTC issue time for expiry and JWT claims; it defaults
        to the current UTC time. Missing grant material and declared terminal
        provider errors raise ``ReauthorizationRequiredError``; transport and
        undeclared provider errors are retryable. The caller retains the client.
        """
        if not provider_name or not auth or not credential_fields or http_client is None:
            raise ValueError("manifest refresh configuration and HTTP client are required")
        self._provider_name = provider_name
        self._auth = auth
        self._fields = dict(credential_fields)
        self._http_client = http_client
        self._settings = dict(settings or {})
        self._now = now or (lambda: datetime.now(UTC))

    def refresh_required(self, state: CredentialRefreshState[Credential]) -> bool:
        """Check declared access fields and the host's single credential expiry."""
        method = self._selected_method(state.credentials)
        grants = self._grants(method)
        if not grants:
            return False
        access_fields = [
            str(mapping["credential"])
            for grant in grants
            for mapping in grant["mappings"]
            if mapping["source"] == "access_token"
        ]
        has_access_token = bool(access_fields) and all(
            self._credential_value(state.credentials, name) for name in access_fields
        )
        missing_expiry_rule = (
            MissingExpiryRule.REFRESH_WHEN_EXPIRY_MISSING
            if any(grant["missing_expiry"] for grant in grants)
            else MissingExpiryRule.KEEP_WHEN_EXPIRY_MISSING
        )
        return is_refresh_required(
            has_access_token, state.expires_at, state.now, missing_expiry_rule
        )

    async def refresh(self, state: CredentialRefreshState[Credential]) -> CredentialRefreshResult[Credential]:
        """Exchange each active grant and return one complete replacement credential."""
        credentials = state.credentials
        method = self._selected_method(credentials)
        grants = self._grants(method)
        if not grants:
            raise ReauthorizationRequiredError()
        for grant in grants:
            self._validate_grant(grant)
        expiries: list[datetime] = []
        exchanged = False
        for grant in grants:
            refresh_field = grant.get("refresh_field", "")
            if refresh_field and not self._credential_value(credentials, refresh_field):
                continue
            token, copies = await self._exchange(grant, credentials)
            required_scopes = self._scopes(grant["scopes"])
            if grant["kind"] == "refresh":
                required_scopes = [scope for scope in required_scopes if scope != "offline_access"]
            separators = r"[ ,]+" if grant["scope_separators"] == "whitespaceAndComma" else r"\s+"
            granted_scopes = set(re.split(separators, token.scope)) - {""}
            required_scope_set = set(required_scopes)
            has_wrong_scopes = (
                (bool(token.scope.strip()) or grant["exact_scopes"])
                and (not required_scope_set <= granted_scopes
                     or (grant["exact_scopes"] and granted_scopes != required_scope_set))
            )
            if has_wrong_scopes:
                raise ReauthorizationRequiredError()
            updates: dict[str, str | SecretString] = {}
            for mapping in grant["mappings"]:
                destination = str(mapping["credential"])
                value = token.mapped_response_string(str(mapping["source"]))
                if value is None or (value == "" and destination == refresh_field):
                    continue
                updates[self._fields[destination]] = (
                    SecretString(value) if self._is_secret(method, destination) else value
                )
            for copy in copies:
                destination = str(copy["credential"])
                source_value = self._credential_value(credentials, str(copy["fromCredential"]))
                updates[self._fields[destination]] = (
                    SecretString(source_value) if self._is_secret(method, destination) else source_value
                )
            credentials = cast(Credential, replace(cast(Any, credentials), **updates))
            lifetime = token.expires_in
            if lifetime <= timedelta(0):
                lifetime = _parse_duration(grant.get("missing_expiry", ""))
            expiries.append(
                self._now().astimezone(UTC) + lifetime if lifetime else datetime.min.replace(tzinfo=UTC)
            )
            exchanged = True
        if not exchanged:
            raise ReauthorizationRequiredError()
        return CredentialRefreshResult(credentials, min(expiries))

    def _selected_method(self, credentials: Credential) -> Mapping[str, Any]:
        methods = self._auth.get("methods") or []
        if not methods:
            return self._auth
        if not hasattr(credentials, "auth_method_id"):
            raise ValueError("multi-method credentials require auth_method_id")
        method_id = cast(Any, credentials).auth_method_id
        method_id = method_id or self._auth.get("defaultMethod", "")
        for method in methods:
            if method.get("id") == method_id:
                return cast(Mapping[str, Any], method)
        raise ReauthorizationRequiredError()

    def _grants(self, method: Mapping[str, Any]) -> list[dict[str, Any]]:
        oauth = method.get("oauth2")
        if oauth:
            refresh = oauth.get("refresh") or {}
            declared = refresh.get("grants") or []
            if not declared:
                mapped = next(
                    (
                        item["credential"]
                        for item in oauth.get("credentialMappings", [])
                        if item.get("source") == "refresh_token"
                    ),
                    "",
                )
                if not mapped:
                    return []
                declared = [
                    {
                        "refreshTokenCredential": mapped,
                        "credentialMappings": oauth.get("credentialMappings", []),
                    }
                ]
            return [
                {
                    "kind": "refresh",
                    "endpoint": oauth["tokenEndpoint"],
                    "authentication": oauth.get("tokenEndpointAuthMethod", "client_secret_post"),
                    "encoding": oauth.get("tokenRequestEncoding", "form"),
                    "client_id": oauth["clientIDCredential"],
                    "client_secret": oauth["clientSecretCredential"],
                    "allows_public_client": refresh.get("allowsPublicClient", False),
                    "refresh_field": grant["refreshTokenCredential"],
                    "mappings": grant["credentialMappings"],
                    "scopes": (oauth.get("userScopes", [])
                    if grant.get("grantedScopes") == "userScopes"
                    else refresh.get("requiredScopes", oauth.get("scopes", []))),
                    "terminal_codes": refresh.get("terminalErrorCodes") or ["invalid_grant"],
                    "accepted_token_types": grant.get("acceptedTokenTypes")
                    or refresh.get("acceptedTokenTypes") or ["Bearer"],
                    "exact_scopes": grant.get("exactScopes", refresh.get("exactScopes", False)),
                    "scope_separators": refresh.get("scopeSeparators", "whitespace"),
                    "accepts_missing_token_type": grant.get(
                        "acceptsMissingTokenType", refresh.get("acceptsMissingTokenType", False)
                    ),
                    "accepts_missing_expires_in": "missingExpiry" in refresh,
                    "missing_expiry": (refresh.get("missingExpiry") or {}).get("lifetime", "")
                    if isinstance(refresh.get("missingExpiry"), dict)
                    else "",
                    "additional": refresh.get("additionalParameters") or {},
                }
                for grant in declared
            ]
        client = method.get("clientCredentials")
        if client:
            return [
                {
                    "kind": "client_credentials",
                    "endpoint": client["tokenEndpoint"],
                    "authentication": client.get("tokenEndpointAuthMethod", "client_secret_post"),
                    "encoding": client.get("tokenRequestEncoding", "form"),
                    "client_id": client["clientIDCredential"],
                    "client_secret": client["clientSecretCredential"],
                    "mappings": client["credentialMappings"],
                    "scopes": client.get("scopes", []),
                    "request_scopes": client.get("scopes", []),
                    "terminal_codes": client.get("terminalErrorCodes") or ["invalid_client"],
                    "accepted_token_types": client.get("acceptedTokenTypes") or ["Bearer"],
                    "exact_scopes": False,
                    "scope_separators": "whitespace",
                    "accepts_missing_token_type": client.get("acceptsMissingTokenType", False),
                    "accepts_missing_expires_in": "missingExpiry" in client,
                    "missing_expiry": (client.get("missingExpiry") or {}).get("lifetime", "")
                    if isinstance(client.get("missingExpiry"), dict)
                    else "",
                    "additional": client.get("additionalParameters") or {},
                }
            ]
        bearer = (method.get("serviceAccount") or {}).get("jwtBearer")
        if bearer:
            return [
                {
                    "kind": "jwt_bearer",
                    "endpoint": bearer["tokenEndpoint"],
                    "encoding": "form",
                    "key_credential": bearer["keyCredential"],
                    "assertion": bearer,
                    "mappings": bearer["credentialMappings"],
                    "copies": bearer.get("credentialCopies", []),
                    "scopes": bearer.get("scopes", []),
                    "terminal_codes": bearer.get("terminalErrorCodes") or ["invalid_grant"],
                    "accepted_token_types": bearer.get("acceptedTokenTypes") or ["Bearer"],
                    "exact_scopes": False,
                    "scope_separators": "whitespace",
                    "accepts_missing_token_type": bearer.get("acceptsMissingTokenType", False),
                    "accepts_missing_expires_in": "missingExpiry" in bearer,
                    "missing_expiry": (bearer.get("missingExpiry") or {}).get("lifetime", "")
                    if isinstance(bearer.get("missingExpiry"), dict)
                    else "",
                    "additional": {},
                }
            ]
        return []

    async def _exchange(
        self, grant: Mapping[str, Any], credentials: Credential
    ) -> tuple[Any, list[dict[str, str]]]:
        key: dict[str, Any] = {}
        if grant["kind"] == "jwt_bearer":
            try:
                parsed = json.loads(self._credential_value(credentials, grant["key_credential"]))
                key_fields = {
                    source["fromKey"] for source in (
                        grant["endpoint"], grant["assertion"]["privateKey"],
                        grant["assertion"]["issuer"], grant["assertion"]["audience"],
                        grant["assertion"].get("subject"),
                    ) if isinstance(source, dict) and "fromKey" in source
                }
                if not isinstance(parsed, dict) or any(
                    field in parsed and not isinstance(parsed[field], str) for field in key_fields
                ):
                    raise ValueError("invalid service account key")
                key = parsed
            except (ValueError, UnicodeDecodeError):
                raise ReauthorizationRequiredError() from None
        endpoint = TokenEndpoint(
            self._provider_name,
            self._resolve_source(grant["endpoint"], credentials, key),
            self._http_client,
            terminal_error_codes=tuple(grant["terminal_codes"]),
            accepted_token_types=tuple(grant["accepted_token_types"]),
            accepts_missing_token_type=bool(grant["accepts_missing_token_type"]),
            accepts_missing_expires_in=bool(grant["accepts_missing_expires_in"]),
            uses_json_request_body=grant["encoding"] == "json",
            additional_request_parameters=dict(grant["additional"]),
        )
        if grant["kind"] == "jwt_bearer":
            assertion = grant["assertion"]
            lifetime = _parse_duration(assertion["lifetime"])
            try:
                private_key = parse_rsa_private_key_pem(
                    self._resolve_source(assertion["privateKey"], credentials, key)
                )
                signed = sign_jwt_bearer_assertion(
                    JWTBearerAssertion(
                        issuer=self._resolve_source(assertion["issuer"], credentials, key),
                        subject=self._resolve_source(assertion["subject"], credentials, key)
                        if assertion.get("subject")
                        else "",
                        audience=self._resolve_source(assertion["audience"], credentials, key),
                        scope=" ".join(self._scopes(assertion.get("scopes", []))),
                        issued_at=self._now(),
                        lifetime=lifetime,
                    ),
                    private_key,
                )
            except (KeyError, ValueError) as error:
                raise ReauthorizationRequiredError(error) from error
            return await endpoint.exchange_jwt_bearer_assertion(signed), list(grant.get("copies", []))
        secret = self._credential_value(credentials, grant["client_secret"])
        if not secret and grant["kind"] == "refresh" and not grant["allows_public_client"]:
            raise ReauthorizationRequiredError()
        method = self._resolve_source(grant["authentication"], credentials, key)
        if not secret and grant["kind"] == "refresh" and grant["allows_public_client"]:
            method = "none"
        client = ClientCredentials(
            self._credential_value(credentials, grant["client_id"]),
            SecretString(secret),
            ClientAuthenticationMethod(method),
        )
        if grant["kind"] == "refresh":
            return await endpoint.exchange_refresh_token(
                client, SecretString(self._credential_value(credentials, grant["refresh_field"]))
            ), []
        return await endpoint.exchange_client_credentials(
            client, tuple(self._scopes(grant["request_scopes"]))
        ), []

    def _credential_value(self, credentials: Credential, name: str) -> str:
        value = getattr(credentials, self._fields[name], "")
        return value.reveal() if isinstance(value, SecretString) else str(value or "")

    def _is_secret(self, method: Mapping[str, Any], name: str) -> bool:
        for field in method.get("fields") or []:
            if field.get("name") == name:
                return str(field.get("type")) == "secretString"
        return False

    def _validate_grant(self, grant: Mapping[str, Any]) -> None:
        for mapping in grant["mappings"]:
            if not mapping.get("source") or mapping.get("credential") not in self._fields:
                raise ValueError("manifest credential field is not mapped")
        for copy in grant.get("copies", []):
            if copy.get("credential") not in self._fields or copy.get("fromCredential") not in self._fields:
                raise ValueError("manifest credential field is not mapped")
        missing_expiry = grant.get("missing_expiry", "")
        if missing_expiry and _parse_duration(missing_expiry) <= timedelta(0):
            raise ValueError("manifest grant duration must be positive")
        if grant["kind"] == "jwt_bearer":
            if _parse_duration(grant["assertion"]["lifetime"]) < timedelta(milliseconds=1):
                raise ValueError("manifest JWT assertion lifetime must be at least one millisecond")

    def _scopes(self, source: Any) -> list[str]:
        if isinstance(source, list):
            return [str(value) for value in source]
        if isinstance(source, dict) and "fromSetting" in source:
            return self._settings.get(source["fromSetting"], "").split()
        return []

    def _resolve_source(self, source: Any, credentials: Credential, key: Mapping[str, Any]) -> str:
        if isinstance(source, str):
            return source
        if "value" in source:
            return str(source["value"])
        if "fromCredential" in source:
            value = self._credential_value(credentials, source["fromCredential"])
        elif "fromSetting" in source:
            value = self._settings.get(source["fromSetting"], "")
        elif "fromKey" in source:
            value = key.get(source["fromKey"], "")
        elif "template" in source:
            template = str(source["template"])
            return re.sub(
                r"\{([^{}]+)\}",
                lambda match: quote(
                    self._credential_value(credentials, match.group(1))
                    if match.group(1) in self._fields
                    else self._settings.get(match.group(1), ""),
                    safe=":@&=+$",
                ),
                template,
            )
        else:
            raise ReauthorizationRequiredError()
        if "values" in source:
            if value not in source["values"]:
                raise ReauthorizationRequiredError()
            return str(source["values"][value])
        if not value:
            raise ReauthorizationRequiredError()
        return str(value)


def _parse_duration(value: str) -> timedelta:
    """Validate Go duration syntax and nanosecond range for grant lifetimes."""
    if not value:
        return timedelta(0)
    if value in ("0", "+0", "-0"):
        return timedelta(0)
    sign = -1 if value.startswith("-") else 1
    if value.startswith(("+", "-")):
        value = value[1:]
    unit_nanoseconds = {
        "ns": 1, "us": 1000, "µs": 1000, "μs": 1000,
        "ms": 1_000_000, "s": 1_000_000_000,
        "m": 60_000_000_000, "h": 3_600_000_000_000,
    }
    total_nanoseconds = 0
    position = 0
    for part in _DURATION_PART.finditer(value):
        if part.start() != position:
            raise ValueError("manifest grant duration is invalid")
        whole, _, fraction = part.group(1).partition(".")
        unit = unit_nanoseconds[part.group(2)]
        total_nanoseconds += int(whole or "0") * unit
        if fraction:
            total_nanoseconds += int(fraction) * unit // 10 ** len(fraction)
        position = part.end()
    maximum = 2**63 if sign < 0 else 2**63 - 1
    if position != len(value) or total_nanoseconds > maximum:
        raise ValueError("manifest grant duration is invalid")
    return sign * timedelta(microseconds=total_nanoseconds // 1000)


__all__ = ["ManifestCredentialRefreshDriver"]
