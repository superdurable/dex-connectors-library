# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-declared OAuth and service-account token grants for connector credentials."""

import base64
import json
import re
from collections.abc import Callable, Mapping
from dataclasses import replace
from datetime import UTC, datetime, timedelta
from ipaddress import ip_address
from typing import Any, Generic, TypeVar, cast
from urllib.parse import quote, urlsplit

import httpx

from ..credentials import (
    CredentialRefreshResult,
    CredentialRefreshState,
    ReauthorizationRequiredError,
    SecretString,
)
from ..errors import Failure, FailureKind, RetryError
from ..providerhttp import new_provider_http_client
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
    ``storedMissingExpiry`` controls undated stored tokens for every grant and
    defaults to ``keep``. The separate ``missingExpiry`` controls responses
    without ``expires_in``; a declared lifetime does not refresh stored tokens.
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
        for method in auth.get("methods") or [auth]:
            for grant in self._grants(method):
                self._validate_stored_missing_expiry(grant)
                self._validate_new_declarations(grant)

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
            if any(grant["stored_missing_expiry"] == "refresh" for grant in grants)
            else MissingExpiryRule.KEEP_WHEN_EXPIRY_MISSING
        )
        if any(
            grant.get("refresh_when_credential_blank")
            and not self._credential_value(state.credentials, grant["refresh_when_credential_blank"])
            for grant in grants
        ):
            return True
        if any(
            grant["response_url_validation"].get("refreshWhenInvalid")
            and not _is_valid_response_url(
                self._credential_value(state.credentials, grant["response_url_validation"]["credential"]),
                grant["response_url_validation"],
            )
            for grant in grants
        ):
            return True
        if any(
            grant["stored_missing_expiry"] == "refreshIfRefreshTokenPresent"
            and self._credential_value(state.credentials, grant.get("refresh_field", ""))
            for grant in grants
        ):
            missing_expiry_rule = MissingExpiryRule.REFRESH_WHEN_EXPIRY_MISSING
        effective_expiry = state.expires_at
        jwt_grant = next((grant for grant in grants if grant["access_token_jwt_expiry_fallback"]), None)
        if jwt_grant and effective_expiry is None and access_fields:
            expiry_seconds = _read_jwt_expiry_seconds(
                self._credential_value(state.credentials, access_fields[0])
            )
            if expiry_seconds is not None:
                effective_expiry = datetime.fromtimestamp(expiry_seconds, UTC)
        return is_refresh_required(has_access_token, effective_expiry, state.now, missing_expiry_rule)

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
            try:
                token, copies = await self._exchange(grant, credentials)
            except httpx.InvalidURL:
                raise RetryError(
                    Failure(
                        FailureKind.PROTOCOL, self._provider_name,
                        "refreshCredentials", "token endpoint is invalid",
                    )
                ) from None
            except (OSError, ValueError) as error:
                raise RetryError(
                    Failure(FailureKind.PROTOCOL, self._provider_name, "refreshCredentials", str(error))
                ) from error
            required_scopes = self._scopes(grant["scopes"])
            if grant["kind"] == "refresh":
                required_scopes = [scope for scope in required_scopes if scope != "offline_access"]
            separators = r"[ ,]+" if grant["scope_separators"] == "whitespaceAndComma" else r"\s+"
            reported_scope = token.scope
            has_reported_scope = bool(reported_scope.strip())
            reported_scope_array: list[str] | None = None
            has_scope_array = True
            if grant["response_scope_field"] == "scopesArray":
                raw_scopes = token.retained_fields.get("scopes")
                has_scope_array = raw_scopes is not None
                has_reported_scope = False
                if raw_scopes is not None:
                    if not isinstance(raw_scopes, list) or any(
                        not isinstance(scope, str) for scope in raw_scopes
                    ):
                        raise ReauthorizationRequiredError()
                    reported_scope_array = raw_scopes
                    has_reported_scope = True

            def canonical(
                scope: str,
                *,
                ignore_case: bool = bool(grant["case_insensitive_scopes"]),
                prefix: str = str(grant["scope_resource_prefix"]),
                ignore_prefix_case: bool = bool(grant["scope_resource_prefix_case_insensitive"]),
            ) -> str:
                matching_scope = scope.lower() if ignore_prefix_case else scope
                matching_prefix = prefix.lower() if ignore_prefix_case else prefix
                stripped = (
                    scope[len(prefix) :] if prefix and matching_scope.startswith(matching_prefix) else scope
                )
                return stripped.lower() if ignore_case else stripped

            reported_names = reported_scope_array if reported_scope_array is not None else re.split(
                separators, reported_scope
            )
            granted_scopes = {canonical(scope) for scope in reported_names} - {""}
            accepted_scope_sets = grant.get("accepted_scope_sets") or [required_scopes]
            if grant["kind"] == "refresh":
                accepted_scope_sets = [
                    [scope for scope in scope_set if scope != "offline_access"]
                    for scope_set in accepted_scope_sets
                ]
            has_wrong_scopes = (has_reported_scope or grant["exact_scopes"]) and not any(
                {canonical(scope) for scope in scope_set} <= granted_scopes
                and (not grant["exact_scopes"] or granted_scopes == {canonical(scope) for scope in scope_set})
                for scope_set in accepted_scope_sets
            )
            if grant["response_scopes"] != "ignore" and has_scope_array and has_wrong_scopes:
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
            url_rule = grant["response_url_validation"]
            if url_rule:
                destination = url_rule["credential"]
                source = next(
                    mapping["source"] for mapping in grant["mappings"] if mapping["credential"] == destination
                )
                has_response = source in token.retained_fields
                returned = token.retained_fields.get(source)
                prior = self._credential_value(credentials, destination)
                if has_response and returned is not None:
                    value = returned
                elif returned is None and has_response:
                    value = prior
                elif url_rule.get("requireResponse"):
                    value = None
                else:
                    value = prior or url_rule.get("defaultURL", "")
                if not isinstance(value, str) or not _is_valid_response_url(value, url_rule):
                    raise RetryError(
                        Failure(
                            FailureKind.PROTOCOL,
                            self._provider_name,
                            "refreshCredentials",
                            "token response URL is invalid",
                        )
                    )
                updates[self._fields[destination]] = _validate_response_url(value, url_rule)
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
            if grant["expiry_cap"] and lifetime > timedelta(0):
                lifetime = min(lifetime, _parse_duration(grant["expiry_cap"]))
            if grant["access_token_jwt_expiry_fallback"] and token.expires_in <= timedelta(0):
                expiry_seconds = _read_jwt_expiry_seconds(token.access_token.reveal())
                now = self._now().astimezone(UTC)
                jwt_expiry = (
                    datetime.fromtimestamp(expiry_seconds, UTC) if expiry_seconds is not None else None
                )
                lifetime = (
                    jwt_expiry - now
                    if jwt_expiry and jwt_expiry > now
                    else _parse_duration(grant["access_token_jwt_expiry_fallback"])
                )
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
                    "token_endpoint_network_policy": oauth.get("tokenEndpointNetworkPolicy", ""),
                    "authentication": oauth.get("tokenEndpointAuthMethod", "client_secret_post"),
                    "encoding": oauth.get("tokenRequestEncoding", "form"),
                    "client_id": oauth["clientIDCredential"],
                    "trim_client_id": refresh.get("trimClientID", False),
                    "client_secret": oauth["clientSecretCredential"],
                    "allows_public_client": refresh.get("allowsPublicClient", False),
                    "refresh_field": grant["refreshTokenCredential"],
                    "mappings": grant["credentialMappings"],
                    "scopes": (
                        oauth.get("userScopes", [])
                        if grant.get("grantedScopes") == "userScopes"
                        else refresh.get("requiredScopes", oauth.get("scopes", []))
                    ),
                    "accepted_scope_sets": refresh.get("acceptedScopeSets"),
                    "response_url_validation": refresh.get("responseURLValidation") or {},
                    "response_scopes": refresh.get("responseScopes", "check"),
                    "response_scope_field": refresh.get("responseScopeField", "scope"),
                    "scope_resource_prefix": refresh.get("scopeResourcePrefix", ""),
                    "case_insensitive_scopes": refresh.get("caseInsensitiveScopes", False),
                    "scope_resource_prefix_case_insensitive": refresh.get(
                        "scopeResourcePrefixCaseInsensitive", False
                    ),
                    "refresh_when_credential_blank": refresh.get("refreshWhenCredentialBlank", ""),
                    "expiry_cap": refresh.get("expiryCap", ""),
                    "access_token_jwt_expiry_fallback": refresh.get("accessTokenJWTExpiryFallback", ""),
                    "declares_accepted_scope_sets": "acceptedScopeSets" in refresh,
                    "terminal_codes": refresh.get("terminalErrorCodes") or ["invalid_grant"],
                    "accepted_token_types": grant.get("acceptedTokenTypes")
                    or refresh.get("acceptedTokenTypes")
                    or ["Bearer"],
                    "exact_scopes": grant.get("exactScopes", refresh.get("exactScopes", False)),
                    "scope_separators": refresh.get("scopeSeparators", "whitespace"),
                    "accepts_missing_token_type": grant.get(
                        "acceptsMissingTokenType", refresh.get("acceptsMissingTokenType", False)
                    ),
                    "accepts_missing_expires_in": (
                        refresh.get("missingExpiry") is not None and refresh.get("missingExpiry") != "reject"
                    ),
                    "missing_expiry_policy": refresh.get("missingExpiry"),
                    "declares_missing_expiry": "missingExpiry" in refresh,
                    "stored_missing_expiry": refresh.get("storedMissingExpiry", "keep"),
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
                    "token_endpoint_network_policy": "",
                    "authentication": client.get("tokenEndpointAuthMethod", "client_secret_post"),
                    "encoding": client.get("tokenRequestEncoding", "form"),
                    "client_id": client["clientIDCredential"],
                    "trim_client_id": client.get("trimClientID", False),
                    "client_secret": client["clientSecretCredential"],
                    "mappings": client["credentialMappings"],
                    "scopes": client.get("scopes", []),
                    "request_scopes": client.get("scopes", []),
                    "accepted_scope_sets": client.get("acceptedScopeSets"),
                    "response_url_validation": {},
                    "response_scopes": client.get("responseScopes", "check"),
                    "response_scope_field": "scope",
                    "scope_resource_prefix": "",
                    "case_insensitive_scopes": False,
                    "scope_resource_prefix_case_insensitive": False,
                    "refresh_when_credential_blank": "",
                    "expiry_cap": "",
                    "access_token_jwt_expiry_fallback": "",
                    "declares_accepted_scope_sets": "acceptedScopeSets" in client,
                    "terminal_codes": client.get("terminalErrorCodes") or ["invalid_client"],
                    "accepted_token_types": client.get("acceptedTokenTypes") or ["Bearer"],
                    "exact_scopes": False,
                    "scope_separators": client.get("scopeSeparators", "whitespace"),
                    "accepts_missing_token_type": client.get("acceptsMissingTokenType", False),
                    "accepts_missing_expires_in": (
                        client.get("missingExpiry") is not None and client.get("missingExpiry") != "reject"
                    ),
                    "missing_expiry_policy": client.get("missingExpiry"),
                    "declares_missing_expiry": "missingExpiry" in client,
                    "stored_missing_expiry": client.get("storedMissingExpiry", "keep"),
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
                    "token_endpoint_network_policy": "",
                    "encoding": "form",
                    "key_credential": bearer["keyCredential"],
                    "assertion": bearer,
                    "mappings": bearer["credentialMappings"],
                    "copies": bearer.get("credentialCopies", []),
                    "scopes": bearer.get("scopes", []),
                    "accepted_scope_sets": bearer.get("acceptedScopeSets"),
                    "response_url_validation": bearer.get("responseURLValidation") or {},
                    "response_scopes": bearer.get("responseScopes", "check"),
                    "response_scope_field": "scope",
                    "scope_resource_prefix": "",
                    "case_insensitive_scopes": False,
                    "scope_resource_prefix_case_insensitive": False,
                    "refresh_when_credential_blank": bearer.get("refreshWhenCredentialBlank", ""),
                    "expiry_cap": "",
                    "access_token_jwt_expiry_fallback": "",
                    "declares_accepted_scope_sets": "acceptedScopeSets" in bearer,
                    "terminal_codes": bearer.get("terminalErrorCodes") or ["invalid_grant"],
                    "accepted_token_types": bearer.get("acceptedTokenTypes") or ["Bearer"],
                    "exact_scopes": False,
                    "scope_separators": bearer.get("scopeSeparators", "whitespace"),
                    "accepts_missing_token_type": bearer.get("acceptsMissingTokenType", False),
                    "accepts_missing_expires_in": (
                        bearer.get("missingExpiry") is not None and bearer.get("missingExpiry") != "reject"
                    ),
                    "missing_expiry_policy": bearer.get("missingExpiry"),
                    "declares_missing_expiry": "missingExpiry" in bearer,
                    "stored_missing_expiry": bearer.get("storedMissingExpiry", "keep"),
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
        if grant["token_endpoint_network_policy"] == "privateNetworkHTTP" and not isinstance(
            self._http_client._transport, httpx.MockTransport
        ):
            async with new_provider_http_client(30, address_policy="privateNetworkHTTP") as client:
                return await self._exchange_with_client(grant, credentials, client)
        return await self._exchange_with_client(grant, credentials, self._http_client)

    async def _exchange_with_client(
        self, grant: Mapping[str, Any], credentials: Credential, http_client: httpx.AsyncClient
    ) -> tuple[Any, list[dict[str, str]]]:
        key: dict[str, Any] = {}
        if grant["kind"] == "jwt_bearer":
            try:
                parsed = json.loads(self._credential_value(credentials, grant["key_credential"]))
                key_fields = {
                    source["fromKey"]
                    for source in (
                        grant["endpoint"],
                        grant["assertion"]["privateKey"],
                        grant["assertion"]["issuer"],
                        grant["assertion"]["audience"],
                        grant["assertion"].get("subject"),
                    )
                    if isinstance(source, dict) and "fromKey" in source
                }
                if not isinstance(parsed, dict) or any(
                    field in parsed and not isinstance(parsed[field], str) for field in key_fields
                ):
                    raise ValueError("invalid service account key")
                key = parsed
            except (ValueError, UnicodeDecodeError):
                raise ReauthorizationRequiredError() from None
        endpoint_url = self._resolve_source(grant["endpoint"], credentials, key)
        if grant["token_endpoint_network_policy"] == "privateNetworkHTTP" and (
            any(ord(character) <= 0x20 or ord(character) > 0x7E for character in endpoint_url)
            or "#" in endpoint_url
        ):
            raise ReauthorizationRequiredError()
        endpoint = TokenEndpoint(
            self._provider_name,
            endpoint_url,
            http_client,
            terminal_error_codes=tuple(grant["terminal_codes"]),
            allows_plain_http=grant["token_endpoint_network_policy"] == "privateNetworkHTTP",
            retained_response_fields=tuple(
                (["scopes"] if grant["response_scope_field"] == "scopesArray" else [])
                + [
                    mapping["source"]
                    for mapping in grant["mappings"]
                    if mapping["credential"] == grant["response_url_validation"].get("credential")
                ]
            ),
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
                subject = (
                    self._resolve_assertion_source(assertion["subject"], assertion, credentials, key)
                    if assertion.get("subject")
                    else ""
                )
                if assertion.get("subjectValidation") == "goMailAddress":
                    subject = _trim_go_space(subject)
                    if not _is_go_mail_address(subject):
                        raise ReauthorizationRequiredError()
                private_key = parse_rsa_private_key_pem(
                    self._resolve_source(assertion["privateKey"], credentials, key)
                )
                signed = sign_jwt_bearer_assertion(
                    JWTBearerAssertion(
                        issuer=self._resolve_assertion_source(
                            assertion["issuer"], assertion, credentials, key
                        ),
                        subject=subject,
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
        client_id = self._credential_value(credentials, grant["client_id"])
        client = ClientCredentials(
            _trim_go_space(client_id) if grant.get("trim_client_id") else client_id,
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

    def _resolve_assertion_source(
        self, source: Any, assertion: Mapping[str, Any], credentials: Credential, key: Mapping[str, Any]
    ) -> str:
        value = self._resolve_source(source, credentials, key)
        if isinstance(source, dict) and source.get("fromCredential") in assertion.get("trimSpaceSources", []):
            value = _trim_go_space(value)
            if not value:
                raise ReauthorizationRequiredError()
        return value

    def _credential_value(self, credentials: Credential, name: str) -> str:
        value = getattr(credentials, self._fields[name], "")
        return value.reveal() if isinstance(value, SecretString) else str(value or "")

    def _is_secret(self, method: Mapping[str, Any], name: str) -> bool:
        for field in method.get("fields") or []:
            if field.get("name") == name:
                return str(field.get("type")) == "secretString"
        return False

    def _validate_grant(self, grant: Mapping[str, Any]) -> None:
        self._validate_stored_missing_expiry(grant)
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

    def _validate_stored_missing_expiry(self, grant: Mapping[str, Any]) -> None:
        if grant["stored_missing_expiry"] not in ("refresh", "keep", "refreshIfRefreshTokenPresent"):
            raise ValueError("manifest storedMissingExpiry must be refresh or keep")

    def _validate_new_declarations(self, grant: Mapping[str, Any]) -> None:
        url_rule = grant["response_url_validation"]
        allowed_url_keys = {
            "credential", "allowedHosts", "allowedHostSuffix", "allowAnyHTTPS", "allowLoopback",
            "forbidPort", "requireResponse", "refreshWhenInvalid", "defaultURL",
        }
        if not isinstance(url_rule, dict) or set(url_rule) - allowed_url_keys:
            raise ValueError("manifest responseURLValidation field is invalid")
        if (
            grant["refresh_when_credential_blank"]
            and grant["refresh_when_credential_blank"] not in self._fields
        ):
            raise ValueError("manifest refreshWhenCredentialBlank field is not mapped")
        if url_rule and (
            not isinstance(url_rule.get("credential"), str)
            or url_rule["credential"] not in self._fields
            or not any(mapping.get("credential") == url_rule["credential"] for mapping in grant["mappings"])
        ):
            raise ValueError("manifest responseURLValidation credential is not mapped")
        if grant["token_endpoint_network_policy"] not in ("", "privateNetworkHTTP"):
            raise ValueError("manifest tokenEndpointNetworkPolicy is invalid")
        if grant["response_scope_field"] not in ("scope", "scopesArray"):
            raise ValueError("manifest responseScopeField is invalid")
        if grant["scope_resource_prefix"] not in ("", "https://graph.microsoft.com/"):
            raise ValueError("manifest scopeResourcePrefix is invalid")
        if grant["access_token_jwt_expiry_fallback"] and _parse_duration(
            grant["access_token_jwt_expiry_fallback"]
        ) <= timedelta(0):
            raise ValueError("manifest accessTokenJWTExpiryFallback must be positive")
        if grant["expiry_cap"] and _parse_duration(grant["expiry_cap"]) <= timedelta(0):
            raise ValueError("manifest expiryCap must be positive")
        if grant["response_scopes"] not in ("check", "ignore"):
            raise ValueError("manifest responseScopes must be check or ignore")
        policy = grant.get("missing_expiry_policy")
        if (
            grant["declares_missing_expiry"]
            and policy not in ("reject", "keep")
            and not (
                isinstance(policy, dict)
                and set(policy) == {"lifetime"}
                and isinstance(policy["lifetime"], str)
                and _parse_duration(policy["lifetime"]) > timedelta(0)
            )
        ):
            raise ValueError("manifest missingExpiry must be reject, keep, or a positive lifetime")
        if grant["scope_separators"] not in ("whitespace", "whitespaceAndComma"):
            raise ValueError("manifest scopeSeparators is invalid")
        scope_sets = grant.get("accepted_scope_sets")
        if grant["kind"] == "client_credentials" and grant["declares_accepted_scope_sets"]:
            raise ValueError("manifest clientCredentials does not support acceptedScopeSets")
        if grant["declares_accepted_scope_sets"] and (
            not isinstance(scope_sets, list)
            or not scope_sets
            or any(
                not isinstance(scope_set, list)
                or not scope_set
                or any(not isinstance(scope, str) or not scope for scope in scope_set)
                for scope_set in scope_sets
            )
        ):
            raise ValueError("manifest acceptedScopeSets is invalid")
        if grant["kind"] == "jwt_bearer":
            assertion = grant["assertion"]
            if "subjectValidation" in assertion and assertion["subjectValidation"] != "goMailAddress":
                raise ValueError("manifest subjectValidation is invalid")
            if "subjectValidation" in assertion and not assertion.get("subject"):
                raise ValueError("manifest subjectValidation requires a subject")

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
            rules = source.get("templateValueRules", {})
            for match in re.finditer(r"\{([^{}]+)\}", template):
                name = match.group(1)
                raw = (
                    self._credential_value(credentials, name)
                    if name in self._fields
                    else self._settings.get(name, "")
                )
                value = _apply_template_value_rule(raw, rules.get(name, {}))
                template = template.replace(match.group(0), quote(value, safe=":@&=+$"))
            return template
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
        "ns": 1,
        "us": 1000,
        "µs": 1000,
        "μs": 1000,
        "ms": 1_000_000,
        "s": 1_000_000_000,
        "m": 60_000_000_000,
        "h": 3_600_000_000_000,
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


def _apply_template_value_rule(raw: str, rule: Mapping[str, Any]) -> str:
    """Validate one path value before applying the manifest's canonical casing."""
    value = _trim_go_space(raw) if rule.get("trimSpace") is True else raw
    if rule.get("maxBytes") and len(value.encode("utf-8")) > rule["maxBytes"]:
        raise ReauthorizationRequiredError()
    if value.lower() in {rejected.lower() for rejected in rule.get("reject", [])}:
        raise ReauthorizationRequiredError()
    patterns = rule.get("patterns", [])
    if patterns and not any(re.fullmatch(pattern, value) for pattern in patterns):
        raise ReauthorizationRequiredError()
    return value.lower() if rule.get("lowercase") is True else value


def _trim_go_space(value: str) -> str:
    """Trim the Unicode White_Space points used by Go strings.TrimSpace."""

    def is_space(character: str) -> bool:
        code = ord(character)
        return (
            code == 0x20
            or 0x09 <= code <= 0x0D
            or code in (0x85, 0xA0, 0x1680)
            or 0x2000 <= code <= 0x200A
            or code in (0x2028, 0x2029, 0x202F, 0x205F, 0x3000)
        )

    start, end = 0, len(value)
    while start < end and is_space(value[start]):
        start += 1
    while end > start and is_space(value[end - 1]):
        end -= 1
    return value[start:end]


def _is_go_mail_address(value: str) -> bool:
    """Match Go 1.24 net/mail addr-spec cases whose parsed address equals its input."""
    if value.count("@") != 1:
        return False
    local, domain = value.split("@")

    def is_atom(text: str) -> bool:
        if not text or text[0] == "." or text[-1] == "." or ".." in text:
            return False
        for character in text:
            code = ord(character)
            if 0xD800 <= code <= 0xDFFF:
                return False
            if code >= 0x80:
                continue
            if code < 0x21 or code > 0x7E or character in '()<>[]:;@\\,"':
                return False
        return True

    if not is_atom(local):
        return False
    if domain.startswith("[") and domain.endswith("]"):
        if "%" in domain:
            return False
        try:
            ip_address(domain[1:-1])
        except ValueError:
            return False
        return True
    return is_atom(domain)


__all__ = ["ManifestCredentialRefreshDriver"]


def _read_jwt_expiry_seconds(token: str) -> int | None:
    """Read monday.com JWT exp solely to schedule refresh."""
    if not token or len(token.encode()) > 16 << 10:
        return None
    segments = token.split(".")
    if len(segments) != 3:
        return None
    if re.fullmatch(r"[A-Za-z0-9_\-\r\n]*=*", segments[1]) is None:
        return None
    try:
        unpadded = segments[1].rstrip("=").replace("\r", "").replace("\n", "")
        payload = base64.urlsafe_b64decode(unpadded + "=" * (-len(unpadded) % 4))
        if not _has_bounded_json_depth(payload):
            return None
        claims = json.loads(payload)
        exp = claims.get("exp") if isinstance(claims, dict) else None
        return (
            int(exp)
            if isinstance(exp, (int, float)) and not isinstance(exp, bool) and 0 < exp <= 0x7FFFFFFF * 4
            else None
        )
    except (ValueError, TypeError, UnicodeDecodeError, RecursionError):
        return None


def _has_bounded_json_depth(payload: bytes) -> bool:
    """Reject oversized or deeply nested JWT claims before recursive JSON parsing."""
    if len(payload) > 16 << 10:
        return False
    depth = 0
    in_string = False
    escaped = False
    for byte in payload:
        if in_string:
            if escaped:
                escaped = False
            elif byte == 0x5C:
                escaped = True
            elif byte == 0x22:
                in_string = False
        elif byte == 0x22:
            in_string = True
        elif byte in (0x7B, 0x5B):
            depth += 1
            if depth > 128:
                return False
        elif byte in (0x7D, 0x5D):
            depth -= 1
            if depth < 0:
                return False
    return depth == 0 and not in_string


def _is_valid_response_url(value: str, rule: Mapping[str, Any]) -> bool:
    try:
        _validate_response_url(value, rule)
        return True
    except ValueError:
        return False


def _validate_response_url(value: str, rule: Mapping[str, Any]) -> str:
    """Validate a mapped provider API origin without exposing its value in errors."""
    if not value.startswith(("https://", "http://")):
        raise ValueError("response URL is invalid")
    if (
        not value
        or not value.isascii()
        or any(ord(character) <= 32 or ord(character) > 126 for character in value)
        or "?" in value
        or "#" in value
    ):
        raise ValueError("response URL is invalid")
    try:
        parsed = urlsplit(value)
        host = parsed.hostname or ""
        port = parsed.port
    except ValueError:
        raise ValueError("response URL is invalid") from None
    try:
        is_loopback = ip_address(host).is_loopback
    except ValueError:
        is_loopback = host.lower() == "localhost"
    if (
        not host
        or parsed.username is not None
        or parsed.password is not None
        or parsed.path not in ("", "/")
        or parsed.query
        or parsed.fragment
        or (
            parsed.scheme != "https"
            and not (rule.get("allowLoopback") and is_loopback and parsed.scheme == "http")
        )
        or (rule.get("forbidPort") and port is not None)
    ):
        raise ValueError("response URL is invalid")
    suffix = rule.get("allowedHostSuffix", "")
    if not (
        (rule.get("allowAnyHTTPS") and parsed.scheme == "https")
        or (rule.get("allowLoopback") and is_loopback)
        or host in rule.get("allowedHosts", [])
        or (suffix and (host.lower() == suffix or host.lower().endswith("." + suffix)))
    ):
        raise ValueError("response URL is invalid")
    return value.rstrip("/")
