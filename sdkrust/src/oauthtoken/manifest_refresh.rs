// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Manifest-declared OAuth and service-account token grants for connector credentials.

use std::collections::{BTreeMap, BTreeSet};
use std::marker::PhantomData;
use std::net::IpAddr;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use base64::Engine;
use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use serde_json::{Map as JsonMap, Value as JsonValue};

use super::{
    ClientAuthenticationMethod, ClientCredentials, JwtBearerAssertion, MissingExpiryRule,
    TokenEndpoint, TokenExchangeError, TokenResponse, is_refresh_required,
    parse_rsa_private_key_pem, sign_jwt_bearer_assertion,
};
use crate::credentials::{
    CredentialError, CredentialRefreshDriver, CredentialRefreshResult, CredentialRefreshState,
    ReauthorizationRequiredError, SecretString,
};
use crate::errors::{Failure, FailureKind, RetryError, ValidationError};
use crate::providerhttp::{AddressPolicy, ProviderHttpClient};
use crate::timestamps::Rfc3339NanoTimestamp;

const MAXIMUM_EXCHANGE_TIMEOUT_NANOSECONDS: i128 = 3_600_000_000_000;
const DEFAULT_PRIVATE_NETWORK_TIMEOUT: Duration = Duration::from_secs(30);
const MAXIMUM_JWT_BYTES: usize = 16 << 10;
const MAXIMUM_JWT_CLAIM_DEPTH: usize = 128;

/// Credential material a [`ManifestCredentialRefreshDriver`] reads and replaces by wire name.
///
/// Generated connector credential types implement it. Field names are the manifest
/// `spec.auth` field names, such as `access_token`.
pub trait ManifestCredential: Clone + Send + Sync {
    /// Returns every credential field name the type stores.
    fn credential_field_names() -> &'static [&'static str];

    /// Returns the field's revealed value, or an empty string when unset.
    fn credential_value(&self, name: &str) -> String;

    /// Returns a copy with the named field replaced; secret fields wrap `value` as secret.
    fn with_credential_value(&self, name: &str, value: String) -> Self;

    /// Returns the selected auth method ID; `None` when the type has no such field.
    ///
    /// An empty ID selects the manifest `defaultMethod`.
    fn auth_method_id(&self) -> Option<&str>;
}

type Clock = Box<dyn Fn() -> Rfc3339NanoTimestamp + Send + Sync>;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum GrantKind {
    Refresh,
    ClientCredentials,
    JwtBearer,
}

#[derive(Clone, Debug)]
struct CredentialMapping {
    credential: String,
    source: String,
}

#[derive(Clone, Debug)]
struct CredentialCopy {
    credential: String,
    from_credential: String,
}

#[derive(Clone, Debug)]
struct Grant {
    kind: GrantKind,
    endpoint: JsonValue,
    token_endpoint_network_policy: String,
    authentication: JsonValue,
    encoding: String,
    client_id: String,
    trim_client_id: bool,
    client_secret: String,
    allows_public_client: bool,
    refresh_field: String,
    key_credential: String,
    assertion: JsonMap<String, JsonValue>,
    mappings: Vec<CredentialMapping>,
    copies: Vec<CredentialCopy>,
    scopes: JsonValue,
    request_scopes: JsonValue,
    accepted_scope_sets: Option<JsonValue>,
    response_url_validation: JsonValue,
    response_scopes: String,
    response_scope_field: String,
    scope_resource_prefix: String,
    case_insensitive_scopes: bool,
    scope_resource_prefix_case_insensitive: bool,
    refresh_when_credential_blank: String,
    expiry_cap: String,
    token_exchange_timeout: Option<JsonValue>,
    access_token_jwt_expiry_fallback: String,
    declares_accepted_scope_sets: bool,
    terminal_codes: Vec<String>,
    accepted_token_types: Vec<String>,
    exact_scopes: bool,
    scope_separators: String,
    accepts_missing_token_type: bool,
    accepts_missing_expires_in: bool,
    missing_expiry_policy: Option<JsonValue>,
    declares_missing_expiry: bool,
    stored_missing_expiry: String,
    missing_expiry: String,
    additional: BTreeMap<String, String>,
}

#[derive(Clone, Debug)]
struct AuthMethod {
    id: String,
    grants: Vec<Grant>,
}

/// Exchanges grants described by a validated connector manifest without owning storage.
///
/// `auth` is the manifest's `spec.auth` JSON object. Credentials for a multi-method manifest
/// report [`ManifestCredential::auth_method_id`]; an empty value selects `defaultMethod`.
/// The host owns the HTTP client, refresh admission, and complete replacement.
/// `storedMissingExpiry` controls undated stored tokens for every grant and defaults to
/// `keep`; the separate `missingExpiry` controls responses without `expires_in`.
///
/// Missing grant material and declared terminal provider errors return
/// [`ReauthorizationRequiredError`]; transport and undeclared provider errors return a
/// [`RetryError`]; a token endpoint refused by its declared address policy returns
/// [`crate::providerhttp::AddressRefusal`]; an invalid declaration returns [`ValidationError`].
pub struct ManifestCredentialRefreshDriver<Credential> {
    provider_name: String,
    default_method: String,
    is_multi_method: bool,
    methods: Vec<AuthMethod>,
    http_client: ProviderHttpClient,
    settings: BTreeMap<String, JsonValue>,
    now: Clock,
    credential: PhantomData<fn() -> Credential>,
}

impl<Credential: ManifestCredential> ManifestCredentialRefreshDriver<Credential> {
    /// Configures a driver with manifest auth, an HTTP client, and connection settings.
    ///
    /// `settings` supplies connection settings read by manifest value sources; values are
    /// strings or integers. The issue time for expiry and JWT claims is the current UTC time.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an empty provider name or auth object, or any invalid
    /// grant declaration.
    pub fn new(
        provider_name: impl Into<String>,
        auth: &JsonValue,
        http_client: ProviderHttpClient,
        settings: BTreeMap<String, JsonValue>,
    ) -> Result<Self, ValidationError> {
        Self::new_with_clock(
            provider_name,
            auth,
            http_client,
            settings,
            Box::new(Rfc3339NanoTimestamp::now_utc),
        )
    }

    /// Configures a driver whose issue time comes from `now`, such as a fixed test clock.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] as for [`Self::new`].
    pub fn new_with_clock(
        provider_name: impl Into<String>,
        auth: &JsonValue,
        http_client: ProviderHttpClient,
        settings: BTreeMap<String, JsonValue>,
        now: Clock,
    ) -> Result<Self, ValidationError> {
        let provider_name = provider_name.into();
        let auth_object = auth.as_object().filter(|object| !object.is_empty());
        let Some(auth_object) = auth_object.filter(|_| !provider_name.is_empty()) else {
            return Err(ValidationError::new(
                "manifest refresh configuration and HTTP client are required",
            ));
        };
        let declared_methods = auth_object
            .get("methods")
            .and_then(JsonValue::as_array)
            .filter(|methods| !methods.is_empty());
        let is_multi_method = declared_methods.is_some();
        let method_values: Vec<&JsonValue> = match declared_methods {
            Some(methods) => methods.iter().collect(),
            None => vec![auth],
        };
        let mut methods = Vec::new();
        for method in method_values {
            let grants = parse_grants(method)?;
            methods.push(AuthMethod {
                id: string_at(method, "id"),
                grants,
            });
        }
        let driver = Self {
            provider_name,
            default_method: string_at(auth, "defaultMethod"),
            is_multi_method,
            methods,
            http_client,
            settings,
            now,
            credential: PhantomData,
        };
        for method in &driver.methods {
            for grant in &method.grants {
                validate_stored_missing_expiry(grant)?;
                driver.validate_new_declarations(grant)?;
            }
        }
        Ok(driver)
    }

    fn selected_method(&self, credentials: &Credential) -> Result<&AuthMethod, CredentialError> {
        if !self.is_multi_method {
            return self
                .methods
                .first()
                .ok_or_else(|| ReauthorizationRequiredError::default().into());
        }
        let Some(method_id) = credentials.auth_method_id() else {
            return Err(ValidationError::new("multi-method credentials require auth_method_id").into());
        };
        let method_id = if method_id.is_empty() {
            self.default_method.as_str()
        } else {
            method_id
        };
        self.methods
            .iter()
            .find(|method| method.id == method_id)
            .ok_or_else(|| ReauthorizationRequiredError::default().into())
    }

    fn is_known_field(name: &str) -> bool {
        Credential::credential_field_names().contains(&name)
    }

    fn credential_value(credentials: &Credential, name: &str) -> String {
        credentials.credential_value(name)
    }

    fn setting_text(&self, name: &str) -> String {
        match self.settings.get(name) {
            Some(JsonValue::String(text)) => text.clone(),
            Some(JsonValue::Null) | None => String::new(),
            Some(other) => other.to_string(),
        }
    }

    fn retry(&self, message: &str) -> CredentialError {
        match RetryError::new(
            Failure::new(
                FailureKind::Protocol,
                self.provider_name.clone(),
                "refreshCredentials",
                message,
            ),
            0.0,
        ) {
            Ok(retry) => retry.into(),
            Err(error) => error.into(),
        }
    }

    fn do_is_refresh_required(
        &self,
        state: &CredentialRefreshState<'_, Credential>,
    ) -> Result<bool, CredentialError> {
        let method = self.selected_method(state.credentials)?;
        let grants = &method.grants;
        if grants.is_empty() {
            return Ok(false);
        }
        let access_fields: Vec<&str> = grants
            .iter()
            .flat_map(|grant| grant.mappings.iter())
            .filter(|mapping| mapping.source == "access_token")
            .map(|mapping| mapping.credential.as_str())
            .collect();
        let has_access_token = !access_fields.is_empty()
            && access_fields
                .iter()
                .all(|name| !Self::credential_value(state.credentials, name).is_empty());
        let mut missing_expiry_rule = if grants
            .iter()
            .any(|grant| grant.stored_missing_expiry == "refresh")
        {
            MissingExpiryRule::RefreshWhenExpiryMissing
        } else {
            MissingExpiryRule::KeepWhenExpiryMissing
        };
        if grants.iter().any(|grant| {
            !grant.refresh_when_credential_blank.is_empty()
                && Self::credential_value(state.credentials, &grant.refresh_when_credential_blank)
                    .is_empty()
        }) {
            return Ok(true);
        }
        if grants.iter().any(|grant| {
            let rule = &grant.response_url_validation;
            bool_at(rule, "refreshWhenInvalid")
                && !is_valid_response_url(
                    &Self::credential_value(state.credentials, &string_at(rule, "credential")),
                    rule,
                )
        }) {
            return Ok(true);
        }
        if grants.iter().any(|grant| {
            grant.stored_missing_expiry == "refreshIfRefreshTokenPresent"
                && !grant.refresh_field.is_empty()
                && !Self::credential_value(state.credentials, &grant.refresh_field).is_empty()
        }) {
            missing_expiry_rule = MissingExpiryRule::RefreshWhenExpiryMissing;
        }
        let mut effective_expiry = state.expires_at.clone();
        let has_jwt_fallback = grants
            .iter()
            .any(|grant| !grant.access_token_jwt_expiry_fallback.is_empty());
        if has_jwt_fallback && effective_expiry.is_none() && !access_fields.is_empty() {
            effective_expiry = read_jwt_expiry_seconds(&Self::credential_value(
                state.credentials,
                access_fields[0],
            ))
            .map(|seconds| timestamp_from_unix_nanoseconds(i128::from(seconds) * 1_000_000_000));
        }
        Ok(is_refresh_required(
            has_access_token,
            effective_expiry.as_ref(),
            &state.now,
            missing_expiry_rule,
            None,
        ))
    }

    fn do_refresh(
        &self,
        state: &CredentialRefreshState<'_, Credential>,
    ) -> Result<CredentialRefreshResult<Credential>, CredentialError> {
        let mut credentials = state.credentials.clone();
        let method = self.selected_method(&credentials)?;
        if method.grants.is_empty() {
            return Err(ReauthorizationRequiredError::default().into());
        }
        for grant in &method.grants {
            self.validate_grant(grant)?;
        }
        let mut expiries: Vec<i128> = Vec::new();
        let mut has_exchanged = false;
        for grant in &method.grants {
            if !grant.refresh_field.is_empty()
                && Self::credential_value(&credentials, &grant.refresh_field).is_empty()
            {
                continue;
            }
            let (token, copies) = match self.exchange(grant, &credentials) {
                Ok(exchanged) => exchanged,
                Err(ExchangeFailure::Credential(error)) => return Err(error),
                Err(ExchangeFailure::Token(error)) => return Err(self.classify_token_error(error)),
            };
            self.check_scopes(grant, &token)?;
            let mut updates: Vec<(String, String)> = Vec::new();
            for mapping in &grant.mappings {
                let Some(value) = token.mapped_response_string(&mapping.source) else {
                    continue;
                };
                if value.is_empty() && mapping.credential == grant.refresh_field {
                    continue;
                }
                updates.push((mapping.credential.clone(), value.to_string()));
            }
            let url_rule = &grant.response_url_validation;
            if url_rule.as_object().is_some_and(|rule| !rule.is_empty()) {
                let destination = string_at(url_rule, "credential");
                let source = grant
                    .mappings
                    .iter()
                    .find(|mapping| mapping.credential == destination)
                    .map(|mapping| mapping.source.clone())
                    .unwrap_or_default();
                let returned = token.retained_fields.get(&source);
                let prior = Self::credential_value(&credentials, &destination);
                let value: Option<String> = match returned {
                    Some(JsonValue::Null) => Some(String::new()),
                    Some(JsonValue::String(text)) => Some(text.clone()),
                    Some(_) => None,
                    None if bool_at(url_rule, "requireResponse") => None,
                    None if prior.is_empty() => Some(string_at(url_rule, "defaultURL")),
                    None => Some(prior),
                };
                let validated = value
                    .as_deref()
                    .and_then(|value| validate_response_url(value, url_rule).ok())
                    .ok_or_else(|| self.retry("token response URL is invalid"))?;
                updates.push((destination, validated));
            }
            for copy in &copies {
                updates.push((
                    copy.credential.clone(),
                    Self::credential_value(&credentials, &copy.from_credential),
                ));
            }
            for (name, value) in updates {
                credentials = credentials.with_credential_value(&name, value);
            }
            let now = (self.now)().unix_nanoseconds();
            let mut lifetime = i128::try_from(token.expires_in.as_nanos()).unwrap_or(i128::MAX);
            if lifetime <= 0 {
                lifetime = parse_go_duration(&grant.missing_expiry)?;
            }
            if !grant.expiry_cap.is_empty() && lifetime > 0 {
                lifetime = lifetime.min(parse_go_duration(&grant.expiry_cap)?);
            }
            if !grant.access_token_jwt_expiry_fallback.is_empty() && token.expires_in.is_zero() {
                let jwt_expiry = read_jwt_expiry_seconds(token.access_token.reveal())
                    .map(|seconds| i128::from(seconds) * 1_000_000_000);
                lifetime = match jwt_expiry {
                    Some(expiry) if expiry > now => expiry - now,
                    _ => parse_go_duration(&grant.access_token_jwt_expiry_fallback)?,
                };
            }
            expiries.push(if lifetime != 0 {
                now + lifetime
            } else {
                Rfc3339NanoTimestamp::zero().unix_nanoseconds()
            });
            has_exchanged = true;
        }
        let Some(expires_at) = expiries.into_iter().min().filter(|_| has_exchanged) else {
            return Err(ReauthorizationRequiredError::default().into());
        };
        Ok(CredentialRefreshResult {
            credentials,
            expires_at: timestamp_from_unix_nanoseconds(expires_at),
        })
    }

    fn classify_token_error(&self, error: TokenExchangeError) -> CredentialError {
        match error {
            TokenExchangeError::ReauthorizationRequired(error) => error.into(),
            TokenExchangeError::AddressRefusal(refusal) => refusal.into(),
            TokenExchangeError::Configuration(error) => self.retry(error.message()),
            TokenExchangeError::Unavailable(message)
            | TokenExchangeError::InvalidResponse(message) => self.retry(&message),
        }
    }

    fn check_scopes(&self, grant: &Grant, token: &TokenResponse) -> Result<(), CredentialError> {
        let mut required_scopes = self.scopes(&grant.scopes);
        if grant.kind == GrantKind::Refresh {
            required_scopes.retain(|scope| scope != "offline_access");
        }
        let mut has_reported_scope = !token.scope.trim().is_empty();
        let mut reported_scope_array: Option<Vec<String>> = None;
        let mut has_scope_array = true;
        if grant.response_scope_field == "scopesArray" {
            let raw_scopes = token.retained_fields.get("scopes");
            has_scope_array = raw_scopes.is_some();
            has_reported_scope = false;
            if let Some(raw_scopes) = raw_scopes {
                let Some(items) = raw_scopes.as_array() else {
                    return Err(ReauthorizationRequiredError::default().into());
                };
                let mut scopes = Vec::with_capacity(items.len());
                for item in items {
                    let Some(scope) = item.as_str() else {
                        return Err(ReauthorizationRequiredError::default().into());
                    };
                    scopes.push(scope.to_string());
                }
                reported_scope_array = Some(scopes);
                has_reported_scope = true;
            }
        }
        let canonical = |scope: &str| -> String {
            let prefix = grant.scope_resource_prefix.as_str();
            let (matching_scope, matching_prefix) = if grant.scope_resource_prefix_case_insensitive
            {
                (scope.to_lowercase(), prefix.to_lowercase())
            } else {
                (scope.to_string(), prefix.to_string())
            };
            let stripped = if !prefix.is_empty() && matching_scope.starts_with(&matching_prefix) {
                scope.get(prefix.len()..).unwrap_or_default()
            } else {
                scope
            };
            if grant.case_insensitive_scopes {
                stripped.to_lowercase()
            } else {
                stripped.to_string()
            }
        };
        let reported_names: Vec<String> = match reported_scope_array {
            Some(scopes) => scopes,
            None if grant.scope_separators == "whitespaceAndComma" => token
                .scope
                .split([' ', ','])
                .map(str::to_string)
                .collect(),
            None => split_python_whitespace(&token.scope),
        };
        let granted: BTreeSet<String> = reported_names
            .iter()
            .map(|scope| canonical(scope))
            .filter(|scope| !scope.is_empty())
            .collect();
        let mut accepted_scope_sets: Vec<Vec<String>> = match &grant.accepted_scope_sets {
            Some(JsonValue::Array(sets)) if !sets.is_empty() => sets
                .iter()
                .map(|set| {
                    set.as_array()
                        .map(|scopes| {
                            scopes
                                .iter()
                                .filter_map(JsonValue::as_str)
                                .map(str::to_string)
                                .collect()
                        })
                        .unwrap_or_default()
                })
                .collect(),
            _ => vec![required_scopes],
        };
        if grant.kind == GrantKind::Refresh {
            for set in &mut accepted_scope_sets {
                set.retain(|scope| scope != "offline_access");
            }
        }
        let has_wrong_scopes = (has_reported_scope || grant.exact_scopes)
            && !accepted_scope_sets.iter().any(|set| {
                let wanted: BTreeSet<String> = set.iter().map(|scope| canonical(scope)).collect();
                wanted.is_subset(&granted) && (!grant.exact_scopes || granted == wanted)
            });
        if grant.response_scopes != "ignore" && has_scope_array && has_wrong_scopes {
            return Err(ReauthorizationRequiredError::default().into());
        }
        Ok(())
    }

    fn exchange(
        &self,
        grant: &Grant,
        credentials: &Credential,
    ) -> Result<(TokenResponse, Vec<CredentialCopy>), ExchangeFailure> {
        let timeout = self
            .token_exchange_timeout(grant.token_exchange_timeout.as_ref())
            .map_err(|error| ExchangeFailure::Credential(error.into()))?;
        if grant.token_endpoint_network_policy == "privateNetworkHTTP"
            && self.http_client.address_policy().is_some()
        {
            let client = ProviderHttpClient::new(
                timeout.unwrap_or(DEFAULT_PRIVATE_NETWORK_TIMEOUT),
                AddressPolicy::PrivateNetworkHttp,
            )
            .map_err(|error| ExchangeFailure::Credential(error.into()))?;
            return self.exchange_with_client(grant, credentials, client, timeout);
        }
        self.exchange_with_client(grant, credentials, self.http_client.clone(), timeout)
    }

    fn exchange_with_client(
        &self,
        grant: &Grant,
        credentials: &Credential,
        http_client: ProviderHttpClient,
        timeout: Option<Duration>,
    ) -> Result<(TokenResponse, Vec<CredentialCopy>), ExchangeFailure> {
        let reauthorization =
            || ExchangeFailure::Credential(ReauthorizationRequiredError::default().into());
        let mut key = JsonMap::new();
        if grant.kind == GrantKind::JwtBearer && !grant.key_credential.is_empty() {
            let raw = Self::credential_value(credentials, &grant.key_credential);
            let Ok(JsonValue::Object(parsed)) = serde_json::from_str::<JsonValue>(&raw) else {
                return Err(reauthorization());
            };
            let assertion = &grant.assertion;
            let sources = [
                Some(&grant.endpoint),
                assertion.get("privateKey"),
                assertion.get("issuer"),
                assertion.get("audience"),
                assertion.get("subject"),
            ];
            for source in sources.into_iter().flatten() {
                if let Some(field) = source.get("fromKey").and_then(JsonValue::as_str)
                    && parsed.get(field).is_some_and(|value| !value.is_string())
                {
                    return Err(reauthorization());
                }
            }
            key = parsed;
        }
        let endpoint_url = self
            .resolve_source(&grant.endpoint, credentials, &key)
            .map_err(ExchangeFailure::Credential)?;
        let is_private_network = grant.token_endpoint_network_policy == "privateNetworkHTTP";
        if is_private_network
            && (endpoint_url
                .chars()
                .any(|character| u32::from(character) <= 0x20 || u32::from(character) > 0x7e)
                || endpoint_url.contains('#'))
        {
            return Err(reauthorization());
        }
        let mut retained = Vec::new();
        if grant.response_scope_field == "scopesArray" {
            retained.push("scopes".to_string());
        }
        let url_credential = string_at(&grant.response_url_validation, "credential");
        for mapping in &grant.mappings {
            if !url_credential.is_empty() && mapping.credential == url_credential {
                retained.push(mapping.source.clone());
            }
        }
        let mut endpoint = TokenEndpoint::new(self.provider_name.clone(), endpoint_url, http_client);
        endpoint.terminal_error_codes = grant.terminal_codes.clone();
        endpoint.allows_plain_http = is_private_network;
        endpoint.retained_response_fields = retained;
        endpoint.accepted_token_types = grant.accepted_token_types.clone();
        endpoint.accepts_missing_token_type = grant.accepts_missing_token_type;
        endpoint.accepts_missing_expires_in = grant.accepts_missing_expires_in;
        endpoint.uses_json_request_body = grant.encoding == "json";
        endpoint.additional_request_parameters = grant.additional.clone();
        if grant.kind == GrantKind::JwtBearer {
            let signed = self
                .sign_assertion(grant, credentials, &key)
                .map_err(ExchangeFailure::Credential)?;
            return endpoint
                .exchange_jwt_bearer_assertion(&signed)
                .map(|token| (token, grant.copies.clone()))
                .map_err(ExchangeFailure::Token);
        }
        let secret = Self::credential_value(credentials, &grant.client_secret);
        if secret.is_empty() && grant.kind == GrantKind::Refresh && !grant.allows_public_client {
            return Err(reauthorization());
        }
        let mut method_name = self
            .resolve_source(&grant.authentication, credentials, &key)
            .map_err(ExchangeFailure::Credential)?;
        if secret.is_empty() && grant.kind == GrantKind::Refresh && grant.allows_public_client {
            method_name = "none".to_string();
        }
        let Some(authentication_method) = ClientAuthenticationMethod::from_wire_name(&method_name)
        else {
            return Err(ExchangeFailure::Token(TokenExchangeError::Configuration(
                ValidationError::new("OAuth client authentication method is not supported"),
            )));
        };
        let client_id = Self::credential_value(credentials, &grant.client_id);
        let client = ClientCredentials::new(
            if grant.trim_client_id {
                trim_go_space(&client_id).to_string()
            } else {
                client_id
            },
            SecretString::new(secret),
            authentication_method,
        );
        endpoint.exchange_timeout = timeout;
        let result = if grant.kind == GrantKind::Refresh {
            endpoint.exchange_refresh_token(
                &client,
                &SecretString::new(Self::credential_value(credentials, &grant.refresh_field)),
            )
        } else {
            endpoint.exchange_client_credentials(&client, &self.scopes(&grant.request_scopes))
        };
        result
            .map(|token| (token, Vec::new()))
            .map_err(ExchangeFailure::Token)
    }

    fn sign_assertion(
        &self,
        grant: &Grant,
        credentials: &Credential,
        key: &JsonMap<String, JsonValue>,
    ) -> Result<SecretString, CredentialError> {
        let assertion = &grant.assertion;
        let reauthorization = || -> CredentialError { ReauthorizationRequiredError::default().into() };
        let lifetime = parse_go_duration(&string_in(assertion, "lifetime"))?;
        let subject = match assertion.get("subject") {
            Some(source) if is_truthy(source) => {
                self.resolve_assertion_source(source, assertion, credentials, key)?
            }
            _ => String::new(),
        };
        let subject = if assertion.get("subjectValidation").and_then(JsonValue::as_str)
            == Some("goMailAddress")
        {
            let trimmed = trim_go_space(&subject).to_string();
            if !is_go_mail_address(&trimmed) {
                return Err(reauthorization());
            }
            trimmed
        } else {
            subject
        };
        let private_key_source = assertion.get("privateKey").ok_or_else(reauthorization)?;
        let private_key =
            parse_rsa_private_key_pem(&self.resolve_source(private_key_source, credentials, key)?)
                .map_err(|_| reauthorization())?;
        let issuer_source = assertion.get("issuer").ok_or_else(reauthorization)?;
        let audience_source = assertion.get("audience").ok_or_else(reauthorization)?;
        let claims = JwtBearerAssertion {
            issuer: self.resolve_assertion_source(issuer_source, assertion, credentials, key)?,
            subject,
            audience: self.resolve_source(audience_source, credentials, key)?,
            scope: self
                .scopes(assertion.get("scopes").unwrap_or(&JsonValue::Null))
                .join(" "),
            issued_at: (self.now)(),
            lifetime: duration_from_nanoseconds(lifetime),
        };
        sign_jwt_bearer_assertion(&claims, &private_key).map_err(|_| reauthorization())
    }

    fn resolve_assertion_source(
        &self,
        source: &JsonValue,
        assertion: &JsonMap<String, JsonValue>,
        credentials: &Credential,
        key: &JsonMap<String, JsonValue>,
    ) -> Result<String, CredentialError> {
        let value = self.resolve_source(source, credentials, key)?;
        let from_credential = source.get("fromCredential").and_then(JsonValue::as_str);
        let is_trimmed = from_credential.is_some_and(|name| {
            assertion
                .get("trimSpaceSources")
                .and_then(JsonValue::as_array)
                .is_some_and(|names| names.iter().any(|candidate| candidate.as_str() == Some(name)))
        });
        if is_trimmed {
            let trimmed = trim_go_space(&value).to_string();
            if trimmed.is_empty() {
                return Err(ReauthorizationRequiredError::default().into());
            }
            return Ok(trimmed);
        }
        Ok(value)
    }

    fn resolve_source(
        &self,
        source: &JsonValue,
        credentials: &Credential,
        key: &JsonMap<String, JsonValue>,
    ) -> Result<String, CredentialError> {
        let reauthorization = || -> CredentialError { ReauthorizationRequiredError::default().into() };
        let object = match source {
            JsonValue::String(text) => return Ok(text.clone()),
            JsonValue::Object(object) => object,
            _ => return Err(reauthorization()),
        };
        if let Some(value) = object.get("value") {
            return Ok(python_str(value));
        }
        let value = if let Some(name) = object.get("fromCredential") {
            Self::credential_value(credentials, &python_str(name))
        } else if let Some(name) = object.get("fromSetting") {
            self.setting_text(&python_str(name))
        } else if let Some(name) = object.get("fromKey") {
            key.get(&python_str(name))
                .map(python_str)
                .unwrap_or_default()
        } else if let Some(template) = object.get("template") {
            return self.resolve_template(&python_str(template), object, credentials);
        } else {
            return Err(reauthorization());
        };
        if let Some(values) = object.get("values") {
            return values
                .get(&value)
                .map(python_str)
                .ok_or_else(reauthorization);
        }
        if value.is_empty() {
            return Err(reauthorization());
        }
        Ok(value)
    }

    fn resolve_template(
        &self,
        template: &str,
        source: &JsonMap<String, JsonValue>,
        credentials: &Credential,
    ) -> Result<String, CredentialError> {
        let rules = source.get("templateValueRules");
        let mut resolved = template.to_string();
        for name in template_placeholders(template) {
            let raw = if Self::is_known_field(&name) {
                Self::credential_value(credentials, &name)
            } else {
                self.setting_text(&name)
            };
            let empty_rule = JsonValue::Object(JsonMap::new());
            let rule = rules.and_then(|rules| rules.get(&name)).unwrap_or(&empty_rule);
            let value = apply_template_value_rule(&raw, rule)?;
            resolved = resolved.replace(&format!("{{{name}}}"), &python_quote(&value));
        }
        Ok(resolved)
    }

    fn scopes(&self, source: &JsonValue) -> Vec<String> {
        match source {
            JsonValue::Array(values) => values.iter().map(python_str).collect(),
            JsonValue::Object(object) => match object.get("fromSetting") {
                Some(name) => split_python_whitespace(&self.setting_text(&python_str(name))),
                None => Vec::new(),
            },
            _ => Vec::new(),
        }
    }

    fn token_exchange_timeout(
        &self,
        declaration: Option<&JsonValue>,
    ) -> Result<Option<Duration>, ValidationError> {
        let invalid = || {
            ValidationError::new(
                "manifest tokenExchangeTimeout must resolve to a positive duration at most 1h",
            )
        };
        let mut declaration = match declaration {
            None | Some(JsonValue::Null) => return Ok(None),
            Some(JsonValue::String(text)) if text.is_empty() => return Ok(None),
            Some(value) => value.clone(),
        };
        if let Some(object) = declaration.as_object()
            && object.len() == 1
            && let Some(name) = object.get("fromSetting")
        {
            declaration = self
                .settings
                .get(&python_str(name))
                .cloned()
                .unwrap_or(JsonValue::Null);
        }
        let nanoseconds = match &declaration {
            JsonValue::Number(number) if number.is_i64() || number.is_u64() => {
                i128::from(number.as_i64().unwrap_or(i64::MAX))
            }
            JsonValue::String(text) => parse_go_duration(text).map_err(|_| invalid())?,
            _ => 0,
        };
        if nanoseconds <= 0 || nanoseconds > MAXIMUM_EXCHANGE_TIMEOUT_NANOSECONDS {
            return Err(invalid());
        }
        Ok(Some(duration_from_nanoseconds(nanoseconds)))
    }

    fn validate_grant(&self, grant: &Grant) -> Result<(), ValidationError> {
        validate_stored_missing_expiry(grant)?;
        let unmapped = || ValidationError::new("manifest credential field is not mapped");
        for mapping in &grant.mappings {
            if mapping.source.is_empty() || !Self::is_known_field(&mapping.credential) {
                return Err(unmapped());
            }
        }
        for copy in &grant.copies {
            if !Self::is_known_field(&copy.credential) || !Self::is_known_field(&copy.from_credential)
            {
                return Err(unmapped());
            }
        }
        if !grant.missing_expiry.is_empty() && parse_go_duration(&grant.missing_expiry)? <= 0 {
            return Err(ValidationError::new(
                "manifest grant duration must be positive",
            ));
        }
        if grant.kind == GrantKind::JwtBearer
            && parse_go_duration(&string_in(&grant.assertion, "lifetime"))? < 1_000_000
        {
            return Err(ValidationError::new(
                "manifest JWT assertion lifetime must be at least one millisecond",
            ));
        }
        Ok(())
    }

    fn validate_new_declarations(&self, grant: &Grant) -> Result<(), ValidationError> {
        if let Some(rules) = grant.endpoint.get("templateValueRules") {
            let Some(rules) = rules.as_object() else {
                return Err(ValidationError::new(
                    "manifest template value rules are invalid",
                ));
            };
            for rule in rules.values() {
                validate_template_value_rule(rule)?;
            }
        }
        let url_rule = &grant.response_url_validation;
        const ALLOWED_URL_KEYS: [&str; 9] = [
            "credential",
            "allowedHosts",
            "allowedHostSuffix",
            "allowAnyHTTPS",
            "allowLoopback",
            "forbidPort",
            "requireResponse",
            "refreshWhenInvalid",
            "defaultURL",
        ];
        let Some(url_object) = url_rule.as_object() else {
            return Err(ValidationError::new(
                "manifest responseURLValidation field is invalid",
            ));
        };
        if url_object
            .keys()
            .any(|name| !ALLOWED_URL_KEYS.contains(&name.as_str()))
        {
            return Err(ValidationError::new(
                "manifest responseURLValidation field is invalid",
            ));
        }
        if !grant.refresh_when_credential_blank.is_empty()
            && !Self::is_known_field(&grant.refresh_when_credential_blank)
        {
            return Err(ValidationError::new(
                "manifest refreshWhenCredentialBlank field is not mapped",
            ));
        }
        if !url_object.is_empty() {
            let credential = url_object.get("credential").and_then(JsonValue::as_str);
            let is_mapped = credential.is_some_and(|credential| {
                Self::is_known_field(credential)
                    && grant
                        .mappings
                        .iter()
                        .any(|mapping| mapping.credential == credential)
            });
            if !is_mapped {
                return Err(ValidationError::new(
                    "manifest responseURLValidation credential is not mapped",
                ));
            }
        }
        if !matches!(
            grant.token_endpoint_network_policy.as_str(),
            "" | "privateNetworkHTTP"
        ) {
            return Err(ValidationError::new(
                "manifest tokenEndpointNetworkPolicy is invalid",
            ));
        }
        if !matches!(grant.response_scope_field.as_str(), "scope" | "scopesArray") {
            return Err(ValidationError::new("manifest responseScopeField is invalid"));
        }
        if !matches!(
            grant.scope_resource_prefix.as_str(),
            "" | "https://graph.microsoft.com/"
        ) {
            return Err(ValidationError::new(
                "manifest scopeResourcePrefix is invalid",
            ));
        }
        if !grant.access_token_jwt_expiry_fallback.is_empty()
            && parse_go_duration(&grant.access_token_jwt_expiry_fallback)? <= 0
        {
            return Err(ValidationError::new(
                "manifest accessTokenJWTExpiryFallback must be positive",
            ));
        }
        if !grant.expiry_cap.is_empty() && parse_go_duration(&grant.expiry_cap)? <= 0 {
            return Err(ValidationError::new("manifest expiryCap must be positive"));
        }
        self.token_exchange_timeout(grant.token_exchange_timeout.as_ref())?;
        if !matches!(grant.response_scopes.as_str(), "check" | "ignore") {
            return Err(ValidationError::new(
                "manifest responseScopes must be check or ignore",
            ));
        }
        if grant.declares_missing_expiry {
            let is_valid = match &grant.missing_expiry_policy {
                Some(JsonValue::String(policy)) => policy == "reject" || policy == "keep",
                Some(JsonValue::Object(policy)) => {
                    policy.len() == 1
                        && policy
                            .get("lifetime")
                            .and_then(JsonValue::as_str)
                            .is_some_and(|lifetime| {
                                parse_go_duration(lifetime).is_ok_and(|value| value > 0)
                            })
                }
                _ => false,
            };
            if !is_valid {
                return Err(ValidationError::new(
                    "manifest missingExpiry must be reject, keep, or a positive lifetime",
                ));
            }
        }
        if !matches!(
            grant.scope_separators.as_str(),
            "whitespace" | "whitespaceAndComma"
        ) {
            return Err(ValidationError::new("manifest scopeSeparators is invalid"));
        }
        if grant.kind == GrantKind::ClientCredentials && grant.declares_accepted_scope_sets {
            return Err(ValidationError::new(
                "manifest clientCredentials does not support acceptedScopeSets",
            ));
        }
        if grant.declares_accepted_scope_sets {
            let is_valid = match &grant.accepted_scope_sets {
                Some(JsonValue::Array(sets)) => {
                    !sets.is_empty()
                        && sets.iter().all(|set| {
                            set.as_array().is_some_and(|scopes| {
                                !scopes.is_empty()
                                    && scopes
                                        .iter()
                                        .all(|scope| scope.as_str().is_some_and(|scope| !scope.is_empty()))
                            })
                        })
                }
                _ => false,
            };
            if !is_valid {
                return Err(ValidationError::new("manifest acceptedScopeSets is invalid"));
            }
        }
        if grant.kind == GrantKind::JwtBearer {
            let assertion = &grant.assertion;
            if let Some(validation) = assertion.get("subjectValidation") {
                if validation.as_str() != Some("goMailAddress") {
                    return Err(ValidationError::new("manifest subjectValidation is invalid"));
                }
                if !assertion.get("subject").is_some_and(is_truthy) {
                    return Err(ValidationError::new(
                        "manifest subjectValidation requires a subject",
                    ));
                }
            }
        }
        Ok(())
    }
}

enum ExchangeFailure {
    Credential(CredentialError),
    Token(TokenExchangeError),
}

impl<Credential: ManifestCredential> CredentialRefreshDriver<Credential>
    for ManifestCredentialRefreshDriver<Credential>
{
    fn is_refresh_required(&self, state: &CredentialRefreshState<'_, Credential>) -> bool {
        // A declaration or method error surfaces from refresh, so treat it as required here.
        self.do_is_refresh_required(state).unwrap_or(true)
    }

    fn refresh(
        &self,
        state: &CredentialRefreshState<'_, Credential>,
    ) -> Result<CredentialRefreshResult<Credential>, CredentialError> {
        self.do_refresh(state)
    }
}

fn validate_stored_missing_expiry(grant: &Grant) -> Result<(), ValidationError> {
    if !matches!(
        grant.stored_missing_expiry.as_str(),
        "refresh" | "keep" | "refreshIfRefreshTokenPresent"
    ) {
        return Err(ValidationError::new(
            "manifest storedMissingExpiry must be refresh or keep",
        ));
    }
    Ok(())
}

fn parse_grants(method: &JsonValue) -> Result<Vec<Grant>, ValidationError> {
    let missing = |field: &str| ValidationError::new(format!("manifest grant field {field} is required"));
    if let Some(oauth) = method.get("oauth2").filter(|value| is_truthy(value)) {
        let empty = JsonValue::Object(JsonMap::new());
        let refresh = oauth.get("refresh").filter(|value| is_truthy(value)).unwrap_or(&empty);
        let mut declared: Vec<JsonValue> = refresh
            .get("grants")
            .and_then(JsonValue::as_array)
            .cloned()
            .unwrap_or_default();
        if declared.is_empty() {
            let mapped = oauth
                .get("credentialMappings")
                .and_then(JsonValue::as_array)
                .and_then(|mappings| {
                    mappings
                        .iter()
                        .find(|mapping| mapping.get("source").and_then(JsonValue::as_str) == Some("refresh_token"))
                })
                .map(|mapping| string_at(mapping, "credential"))
                .unwrap_or_default();
            if mapped.is_empty() {
                return Ok(Vec::new());
            }
            let mut synthesized = JsonMap::new();
            synthesized.insert("refreshTokenCredential".to_string(), JsonValue::from(mapped));
            synthesized.insert(
                "credentialMappings".to_string(),
                oauth
                    .get("credentialMappings")
                    .cloned()
                    .unwrap_or(JsonValue::Array(Vec::new())),
            );
            declared = vec![JsonValue::Object(synthesized)];
        }
        let mut grants = Vec::new();
        for declared_grant in &declared {
            let missing_expiry = refresh.get("missingExpiry");
            grants.push(Grant {
                kind: GrantKind::Refresh,
                endpoint: oauth.get("tokenEndpoint").cloned().ok_or_else(|| missing("tokenEndpoint"))?,
                token_endpoint_network_policy: string_at(oauth, "tokenEndpointNetworkPolicy"),
                authentication: oauth
                    .get("tokenEndpointAuthMethod")
                    .cloned()
                    .unwrap_or_else(|| JsonValue::from("client_secret_post")),
                encoding: string_or(oauth, "tokenRequestEncoding", "form"),
                client_id: required_string(oauth, "clientIDCredential")?,
                trim_client_id: bool_at(refresh, "trimClientID"),
                client_secret: required_string(oauth, "clientSecretCredential")?,
                allows_public_client: bool_at(refresh, "allowsPublicClient"),
                refresh_field: required_string(declared_grant, "refreshTokenCredential")?,
                key_credential: String::new(),
                assertion: JsonMap::new(),
                mappings: parse_mappings(declared_grant.get("credentialMappings"))?,
                copies: Vec::new(),
                scopes: if declared_grant.get("grantedScopes").and_then(JsonValue::as_str)
                    == Some("userScopes")
                {
                    oauth.get("userScopes").cloned().unwrap_or(JsonValue::Array(Vec::new()))
                } else {
                    refresh
                        .get("requiredScopes")
                        .or_else(|| oauth.get("scopes"))
                        .cloned()
                        .unwrap_or(JsonValue::Array(Vec::new()))
                },
                request_scopes: JsonValue::Array(Vec::new()),
                accepted_scope_sets: refresh.get("acceptedScopeSets").cloned(),
                response_url_validation: truthy_or_empty(refresh.get("responseURLValidation")),
                response_scopes: string_or(refresh, "responseScopes", "check"),
                response_scope_field: string_or(refresh, "responseScopeField", "scope"),
                scope_resource_prefix: string_at(refresh, "scopeResourcePrefix"),
                case_insensitive_scopes: bool_at(refresh, "caseInsensitiveScopes"),
                scope_resource_prefix_case_insensitive: bool_at(
                    refresh,
                    "scopeResourcePrefixCaseInsensitive",
                ),
                refresh_when_credential_blank: string_at(refresh, "refreshWhenCredentialBlank"),
                expiry_cap: string_at(refresh, "expiryCap"),
                token_exchange_timeout: refresh.get("tokenExchangeTimeout").cloned(),
                access_token_jwt_expiry_fallback: string_at(
                    refresh,
                    "accessTokenJWTExpiryFallback",
                ),
                declares_accepted_scope_sets: refresh.get("acceptedScopeSets").is_some(),
                terminal_codes: string_list_or(refresh.get("terminalErrorCodes"), &["invalid_grant"]),
                accepted_token_types: declared_grant
                    .get("acceptedTokenTypes")
                    .filter(|value| is_truthy(value))
                    .or_else(|| refresh.get("acceptedTokenTypes"))
                    .map_or_else(|| vec!["Bearer".to_string()], |value| string_list_or(Some(value), &["Bearer"])),
                exact_scopes: declared_grant
                    .get("exactScopes")
                    .map_or_else(|| bool_at(refresh, "exactScopes"), is_truthy),
                scope_separators: string_or(refresh, "scopeSeparators", "whitespace"),
                accepts_missing_token_type: declared_grant
                    .get("acceptsMissingTokenType")
                    .map_or_else(|| bool_at(refresh, "acceptsMissingTokenType"), is_truthy),
                accepts_missing_expires_in: accepts_missing_expires_in(missing_expiry),
                missing_expiry_policy: missing_expiry.cloned(),
                declares_missing_expiry: missing_expiry.is_some(),
                stored_missing_expiry: string_or(refresh, "storedMissingExpiry", "keep"),
                missing_expiry: missing_expiry_lifetime(missing_expiry),
                additional: string_map(refresh.get("additionalParameters")),
            });
        }
        return Ok(grants);
    }
    if let Some(client) = method.get("clientCredentials").filter(|value| is_truthy(value)) {
        let missing_expiry = client.get("missingExpiry");
        let scopes = client.get("scopes").cloned().unwrap_or(JsonValue::Array(Vec::new()));
        return Ok(vec![Grant {
            kind: GrantKind::ClientCredentials,
            endpoint: client.get("tokenEndpoint").cloned().ok_or_else(|| missing("tokenEndpoint"))?,
            token_endpoint_network_policy: String::new(),
            authentication: client
                .get("tokenEndpointAuthMethod")
                .cloned()
                .unwrap_or_else(|| JsonValue::from("client_secret_post")),
            encoding: string_or(client, "tokenRequestEncoding", "form"),
            client_id: required_string(client, "clientIDCredential")?,
            trim_client_id: bool_at(client, "trimClientID"),
            client_secret: required_string(client, "clientSecretCredential")?,
            allows_public_client: false,
            refresh_field: String::new(),
            key_credential: String::new(),
            assertion: JsonMap::new(),
            mappings: parse_mappings(client.get("credentialMappings"))?,
            copies: Vec::new(),
            scopes: scopes.clone(),
            request_scopes: scopes,
            accepted_scope_sets: client.get("acceptedScopeSets").cloned(),
            response_url_validation: JsonValue::Object(JsonMap::new()),
            response_scopes: string_or(client, "responseScopes", "check"),
            response_scope_field: "scope".to_string(),
            scope_resource_prefix: String::new(),
            case_insensitive_scopes: false,
            scope_resource_prefix_case_insensitive: false,
            refresh_when_credential_blank: String::new(),
            expiry_cap: String::new(),
            token_exchange_timeout: client.get("tokenExchangeTimeout").cloned(),
            access_token_jwt_expiry_fallback: String::new(),
            declares_accepted_scope_sets: client.get("acceptedScopeSets").is_some(),
            terminal_codes: string_list_or(client.get("terminalErrorCodes"), &["invalid_client"]),
            accepted_token_types: string_list_or(client.get("acceptedTokenTypes"), &["Bearer"]),
            exact_scopes: false,
            scope_separators: string_or(client, "scopeSeparators", "whitespace"),
            accepts_missing_token_type: bool_at(client, "acceptsMissingTokenType"),
            accepts_missing_expires_in: accepts_missing_expires_in(missing_expiry),
            missing_expiry_policy: missing_expiry.cloned(),
            declares_missing_expiry: missing_expiry.is_some(),
            stored_missing_expiry: string_or(client, "storedMissingExpiry", "keep"),
            missing_expiry: missing_expiry_lifetime(missing_expiry),
            additional: string_map(client.get("additionalParameters")),
        }]);
    }
    let bearer = method
        .get("serviceAccount")
        .and_then(|account| account.get("jwtBearer"))
        .filter(|value| is_truthy(value));
    let Some(bearer) = bearer else {
        return Ok(Vec::new());
    };
    let missing_expiry = bearer.get("missingExpiry");
    Ok(vec![Grant {
        kind: GrantKind::JwtBearer,
        endpoint: bearer.get("tokenEndpoint").cloned().ok_or_else(|| missing("tokenEndpoint"))?,
        token_endpoint_network_policy: String::new(),
        authentication: JsonValue::Null,
        encoding: "form".to_string(),
        client_id: String::new(),
        trim_client_id: false,
        client_secret: String::new(),
        allows_public_client: false,
        refresh_field: String::new(),
        key_credential: string_at(bearer, "keyCredential"),
        assertion: bearer.as_object().cloned().unwrap_or_default(),
        mappings: parse_mappings(bearer.get("credentialMappings"))?,
        copies: parse_copies(bearer.get("credentialCopies"))?,
        scopes: bearer.get("scopes").cloned().unwrap_or(JsonValue::Array(Vec::new())),
        request_scopes: JsonValue::Array(Vec::new()),
        accepted_scope_sets: bearer.get("acceptedScopeSets").cloned(),
        response_url_validation: truthy_or_empty(bearer.get("responseURLValidation")),
        response_scopes: string_or(bearer, "responseScopes", "check"),
        response_scope_field: "scope".to_string(),
        scope_resource_prefix: String::new(),
        case_insensitive_scopes: false,
        scope_resource_prefix_case_insensitive: false,
        refresh_when_credential_blank: string_at(bearer, "refreshWhenCredentialBlank"),
        expiry_cap: String::new(),
        token_exchange_timeout: None,
        access_token_jwt_expiry_fallback: String::new(),
        declares_accepted_scope_sets: bearer.get("acceptedScopeSets").is_some(),
        terminal_codes: string_list_or(bearer.get("terminalErrorCodes"), &["invalid_grant"]),
        accepted_token_types: string_list_or(bearer.get("acceptedTokenTypes"), &["Bearer"]),
        exact_scopes: false,
        scope_separators: string_or(bearer, "scopeSeparators", "whitespace"),
        accepts_missing_token_type: bool_at(bearer, "acceptsMissingTokenType"),
        accepts_missing_expires_in: accepts_missing_expires_in(missing_expiry),
        missing_expiry_policy: missing_expiry.cloned(),
        declares_missing_expiry: missing_expiry.is_some(),
        stored_missing_expiry: string_or(bearer, "storedMissingExpiry", "keep"),
        missing_expiry: missing_expiry_lifetime(missing_expiry),
        additional: BTreeMap::new(),
    }])
}

fn parse_mappings(value: Option<&JsonValue>) -> Result<Vec<CredentialMapping>, ValidationError> {
    let Some(items) = value.and_then(JsonValue::as_array) else {
        return Err(ValidationError::new(
            "manifest grant field credentialMappings is required",
        ));
    };
    Ok(items
        .iter()
        .map(|item| CredentialMapping {
            credential: string_at(item, "credential"),
            source: string_at(item, "source"),
        })
        .collect())
}

fn parse_copies(value: Option<&JsonValue>) -> Result<Vec<CredentialCopy>, ValidationError> {
    Ok(value
        .and_then(JsonValue::as_array)
        .map(|items| {
            items
                .iter()
                .map(|item| CredentialCopy {
                    credential: string_at(item, "credential"),
                    from_credential: string_at(item, "fromCredential"),
                })
                .collect()
        })
        .unwrap_or_default())
}

fn accepts_missing_expires_in(missing_expiry: Option<&JsonValue>) -> bool {
    match missing_expiry {
        None | Some(JsonValue::Null) => false,
        Some(JsonValue::String(policy)) => policy != "reject",
        Some(_) => true,
    }
}

fn missing_expiry_lifetime(missing_expiry: Option<&JsonValue>) -> String {
    missing_expiry
        .and_then(JsonValue::as_object)
        .and_then(|policy| policy.get("lifetime"))
        .map(python_str)
        .unwrap_or_default()
}

fn is_truthy(value: &JsonValue) -> bool {
    match value {
        JsonValue::Null => false,
        JsonValue::Bool(flag) => *flag,
        JsonValue::Number(number) => number.as_f64().is_some_and(|value| value != 0.0),
        JsonValue::String(text) => !text.is_empty(),
        JsonValue::Array(items) => !items.is_empty(),
        JsonValue::Object(object) => !object.is_empty(),
    }
}

fn truthy_or_empty(value: Option<&JsonValue>) -> JsonValue {
    value
        .filter(|value| is_truthy(value))
        .cloned()
        .unwrap_or_else(|| JsonValue::Object(JsonMap::new()))
}

fn string_at(value: &JsonValue, name: &str) -> String {
    value.get(name).map(python_str).unwrap_or_default()
}

fn string_in(object: &JsonMap<String, JsonValue>, name: &str) -> String {
    object.get(name).map(python_str).unwrap_or_default()
}

fn string_or(value: &JsonValue, name: &str, default: &str) -> String {
    value
        .get(name)
        .map_or_else(|| default.to_string(), python_str)
}

fn required_string(value: &JsonValue, name: &str) -> Result<String, ValidationError> {
    value
        .get(name)
        .map(python_str)
        .ok_or_else(|| ValidationError::new(format!("manifest grant field {name} is required")))
}

fn bool_at(value: &JsonValue, name: &str) -> bool {
    value.get(name).is_some_and(is_truthy)
}

fn string_list_or(value: Option<&JsonValue>, default: &[&str]) -> Vec<String> {
    match value.and_then(JsonValue::as_array).filter(|items| !items.is_empty()) {
        Some(items) => items.iter().map(python_str).collect(),
        None => default.iter().map(|item| (*item).to_string()).collect(),
    }
}

fn string_map(value: Option<&JsonValue>) -> BTreeMap<String, String> {
    value
        .and_then(JsonValue::as_object)
        .map(|object| {
            object
                .iter()
                .map(|(name, value)| (name.clone(), python_str(value)))
                .collect()
        })
        .unwrap_or_default()
}

/// Renders a JSON scalar as Python `str()` would for manifest values.
fn python_str(value: &JsonValue) -> String {
    match value {
        JsonValue::String(text) => text.clone(),
        JsonValue::Bool(true) => "True".to_string(),
        JsonValue::Bool(false) => "False".to_string(),
        JsonValue::Null => "None".to_string(),
        other => other.to_string(),
    }
}

/// Splits like Python `str.split()`: runs of ASCII and Unicode whitespace.
fn split_python_whitespace(value: &str) -> Vec<String> {
    value
        .split(char::is_whitespace)
        .filter(|part| !part.is_empty())
        .map(str::to_string)
        .collect()
}

fn template_placeholders(template: &str) -> Vec<String> {
    let mut names = Vec::new();
    let mut rest = template;
    while let Some(start) = rest.find('{') {
        let after = &rest[start + 1..];
        match after.find(['{', '}']) {
            Some(end) if after.as_bytes()[end] == b'}' && end > 0 => {
                names.push(after[..end].to_string());
                rest = &after[end + 1..];
            }
            Some(end) => rest = &after[end..],
            None => break,
        }
    }
    names
}

/// Python `urllib.parse.quote(value, safe=":@&=+$")`.
fn python_quote(value: &str) -> String {
    let mut quoted = String::with_capacity(value.len());
    for byte in value.bytes() {
        match byte {
            b'A'..=b'Z'
            | b'a'..=b'z'
            | b'0'..=b'9'
            | b'_'
            | b'.'
            | b'-'
            | b'~'
            | b':'
            | b'@'
            | b'&'
            | b'='
            | b'+'
            | b'$' => quoted.push(char::from(byte)),
            _ => quoted.push_str(&format!("%{byte:02X}")),
        }
    }
    quoted
}

fn apply_template_value_rule(raw: &str, rule: &JsonValue) -> Result<String, CredentialError> {
    validate_template_value_rule(rule)?;
    let reauthorization = || -> CredentialError { ReauthorizationRequiredError::default().into() };
    let value = if rule.get("trimSpace") == Some(&JsonValue::Bool(true)) {
        trim_go_space(raw).to_string()
    } else {
        raw.to_string()
    };
    if let Some(maximum) = rule.get("maxBytes").and_then(JsonValue::as_u64)
        && u64::try_from(value.len()).unwrap_or(u64::MAX) > maximum
    {
        return Err(reauthorization());
    }
    let lowered = value.to_lowercase();
    if rule
        .get("reject")
        .and_then(JsonValue::as_array)
        .is_some_and(|rejected| {
            rejected
                .iter()
                .filter_map(JsonValue::as_str)
                .any(|candidate| candidate.to_lowercase() == lowered)
        })
    {
        return Err(reauthorization());
    }
    let patterns: Vec<&str> = rule
        .get("patterns")
        .and_then(JsonValue::as_array)
        .map(|patterns| patterns.iter().filter_map(JsonValue::as_str).collect())
        .unwrap_or_default();
    if !patterns.is_empty() {
        let mut is_matched = false;
        for pattern in patterns {
            if compile_ascii_full_match(pattern)?.is_match(&value) {
                is_matched = true;
                break;
            }
        }
        if !is_matched {
            return Err(reauthorization());
        }
    }
    Ok(if rule.get("lowercase") == Some(&JsonValue::Bool(true)) {
        lowered
    } else {
        value
    })
}

fn validate_template_value_rule(rule: &JsonValue) -> Result<(), ValidationError> {
    let invalid = || ValidationError::new("manifest template value rule is invalid");
    let Some(object) = rule.as_object() else {
        return Err(invalid());
    };
    const ALLOWED: [&str; 5] = ["trimSpace", "maxBytes", "patterns", "reject", "lowercase"];
    if object.keys().any(|name| !ALLOWED.contains(&name.as_str())) {
        return Err(invalid());
    }
    let is_string_list = |value: &JsonValue| {
        value
            .as_array()
            .is_some_and(|items| items.iter().all(JsonValue::is_string))
    };
    if object.get("trimSpace").is_some_and(|value| !value.is_boolean())
        || object.get("lowercase").is_some_and(|value| !value.is_boolean())
        || object
            .get("maxBytes")
            .is_some_and(|value| !(value.is_u64() || value.as_i64().is_some_and(|number| number >= 0)))
        || object.get("reject").is_some_and(|value| !is_string_list(value))
        || object.get("patterns").is_some_and(|value| !is_string_list(value))
    {
        return Err(invalid());
    }
    for pattern in object
        .get("patterns")
        .and_then(JsonValue::as_array)
        .into_iter()
        .flatten()
        .filter_map(JsonValue::as_str)
    {
        validate_portable_template_value_pattern(pattern)?;
    }
    Ok(())
}

fn validate_portable_template_value_pattern(pattern: &str) -> Result<(), ValidationError> {
    let invalid = || ValidationError::new("manifest template value pattern is invalid");
    let characters: Vec<char> = pattern.chars().collect();
    let mut is_character_class = false;
    let mut index = 0;
    while index < characters.len() {
        match characters[index] {
            '\\' => {
                index += 1;
                if characters
                    .get(index)
                    .is_some_and(|next| "123456789pPQEk".contains(*next))
                {
                    return Err(invalid());
                }
            }
            '[' => {
                if is_character_class && characters.get(index + 1) == Some(&':') {
                    return Err(invalid());
                }
                is_character_class = true;
            }
            ']' => is_character_class = false,
            '(' if !is_character_class
                && characters.get(index + 1) == Some(&'?')
                && characters.get(index + 2) != Some(&':') =>
            {
                return Err(invalid());
            }
            _ => {}
        }
        index += 1;
    }
    compile_ascii_full_match(pattern).map(|_| ())
}

/// Compiles `pattern` for a full match with Go and Python ASCII `\w`, `\d`, `\s`, and `\b`.
fn compile_ascii_full_match(pattern: &str) -> Result<regex::Regex, ValidationError> {
    let invalid = || ValidationError::new("manifest template value pattern is invalid");
    let mut translated = String::with_capacity(pattern.len() + 16);
    let mut characters = pattern.chars().peekable();
    let mut is_character_class = false;
    while let Some(character) = characters.next() {
        match character {
            '\\' => {
                let Some(escaped) = characters.next() else {
                    return Err(invalid());
                };
                let replacement = match (escaped, is_character_class) {
                    ('w', false) => Some("[0-9A-Za-z_]"),
                    ('W', false) => Some("[^0-9A-Za-z_]"),
                    ('d', false) => Some("[0-9]"),
                    ('D', false) => Some("[^0-9]"),
                    ('s', false) => Some("[\\t\\n\\x0B\\f\\r ]"),
                    ('S', false) => Some("[^\\t\\n\\x0B\\f\\r ]"),
                    ('b', false) => Some("(?-u:\\b)"),
                    ('B', false) => Some("(?-u:\\B)"),
                    ('w', true) => Some("0-9A-Za-z_"),
                    ('d', true) => Some("0-9"),
                    ('s', true) => Some("\\t\\n\\x0B\\f\\r "),
                    ('W' | 'D' | 'S', true) => return Err(invalid()),
                    _ => None,
                };
                match replacement {
                    Some(replacement) => translated.push_str(replacement),
                    None => {
                        translated.push('\\');
                        translated.push(escaped);
                    }
                }
            }
            '[' if !is_character_class => {
                is_character_class = true;
                translated.push('[');
                if characters.peek() == Some(&'^') {
                    translated.push('^');
                    characters.next();
                }
                if characters.peek() == Some(&']') {
                    translated.push_str("\\]");
                    characters.next();
                }
            }
            ']' if is_character_class => {
                is_character_class = false;
                translated.push(']');
            }
            _ => translated.push(character),
        }
    }
    regex::Regex::new(&format!("^(?:{translated})$")).map_err(|_| invalid())
}

/// Trims the Unicode `White_Space` points used by Go `strings.TrimSpace`.
fn trim_go_space(value: &str) -> &str {
    let is_space = |character: char| {
        let code = u32::from(character);
        code == 0x20
            || (0x09..=0x0d).contains(&code)
            || matches!(code, 0x85 | 0xa0 | 0x1680)
            || (0x2000..=0x200a).contains(&code)
            || matches!(code, 0x2028 | 0x2029 | 0x202f | 0x205f | 0x3000)
    };
    value.trim_matches(is_space)
}

/// Matches Go 1.24 `net/mail` addr-spec cases whose parsed address equals its input.
fn is_go_mail_address(value: &str) -> bool {
    let Some((local, domain)) = value.split_once('@') else {
        return false;
    };
    if domain.contains('@') {
        return false;
    }
    let is_atom = |text: &str| {
        !text.is_empty()
            && !text.starts_with('.')
            && !text.ends_with('.')
            && !text.contains("..")
            && text.chars().all(|character| {
                let code = u32::from(character);
                code >= 0x80 || ((0x21..=0x7e).contains(&code) && !"()<>[]:;@\\,\"".contains(character))
            })
    };
    if !is_atom(local) {
        return false;
    }
    if let Some(literal) = domain.strip_prefix('[').and_then(|rest| rest.strip_suffix(']')) {
        return !literal.contains('%') && literal.parse::<IpAddr>().is_ok();
    }
    is_atom(domain)
}

/// Reads a JWT `exp` claim solely to schedule refresh; invalid tokens yield `None`.
pub(crate) fn read_jwt_expiry_seconds(token: &str) -> Option<i64> {
    if token.is_empty() || token.len() > MAXIMUM_JWT_BYTES {
        return None;
    }
    let segments: Vec<&str> = token.split('.').collect();
    if segments.len() != 3 {
        return None;
    }
    let payload_segment = segments[1];
    let body = payload_segment.trim_end_matches('=');
    if !body
        .bytes()
        .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'_' | b'-' | b'\r' | b'\n'))
    {
        return None;
    }
    let cleaned: String = body.chars().filter(|character| !matches!(character, '\r' | '\n')).collect();
    let payload = URL_SAFE_NO_PAD.decode(cleaned).ok()?;
    if !has_bounded_json_depth(&payload) {
        return None;
    }
    let claims: JsonValue = serde_json::from_slice(&payload).ok()?;
    let expiry = claims.get("exp")?;
    let seconds = match expiry {
        JsonValue::Number(number) => number.as_f64()?,
        _ => return None,
    };
    if seconds > 0.0 && seconds <= f64::from(0x7fff_ffff_u32) * 4.0 {
        #[expect(
            clippy::cast_possible_truncation,
            reason = "the bound above keeps the truncated value within i64"
        )]
        let whole = seconds.trunc() as i64;
        Some(whole)
    } else {
        None
    }
}

fn has_bounded_json_depth(payload: &[u8]) -> bool {
    if payload.len() > MAXIMUM_JWT_BYTES {
        return false;
    }
    let mut depth: usize = 0;
    let mut is_in_string = false;
    let mut is_escaped = false;
    for byte in payload {
        if is_in_string {
            if is_escaped {
                is_escaped = false;
            } else if *byte == b'\\' {
                is_escaped = true;
            } else if *byte == b'"' {
                is_in_string = false;
            }
        } else if *byte == b'"' {
            is_in_string = true;
        } else if matches!(byte, b'{' | b'[') {
            depth += 1;
            if depth > MAXIMUM_JWT_CLAIM_DEPTH {
                return false;
            }
        } else if matches!(byte, b'}' | b']') {
            let Some(next) = depth.checked_sub(1) else {
                return false;
            };
            depth = next;
        }
    }
    depth == 0 && !is_in_string
}

fn is_valid_response_url(value: &str, rule: &JsonValue) -> bool {
    validate_response_url(value, rule).is_ok()
}

/// Validates a mapped provider API origin without exposing its value in errors.
fn validate_response_url(value: &str, rule: &JsonValue) -> Result<String, ValidationError> {
    let invalid = || ValidationError::new("response URL is invalid");
    let (scheme, rest) = if let Some(rest) = value.strip_prefix("https://") {
        ("https", rest)
    } else if let Some(rest) = value.strip_prefix("http://") {
        ("http", rest)
    } else {
        return Err(invalid());
    };
    if !value.bytes().all(|byte| (33..=126).contains(&byte)) || value.contains('?') || value.contains('#') {
        return Err(invalid());
    }
    let (netloc, path) = match rest.find('/') {
        Some(index) => (&rest[..index], &rest[index..]),
        None => (rest, ""),
    };
    if netloc.contains('@') || !matches!(path, "" | "/") {
        return Err(invalid());
    }
    let (host, port) = match netloc.strip_prefix('[') {
        Some(bracketed) => {
            let (host, after) = bracketed.split_once(']').ok_or_else(invalid)?;
            let port = match after {
                "" => None,
                _ => Some(after.strip_prefix(':').ok_or_else(invalid)?),
            };
            (host.to_string(), port)
        }
        None => match netloc.split_once(':') {
            Some((host, port)) => (host.to_ascii_lowercase(), Some(port)),
            None => (netloc.to_ascii_lowercase(), None),
        },
    };
    let port = match port.filter(|port| !port.is_empty()) {
        None => None,
        Some(port) => {
            let number = port
                .bytes()
                .all(|byte| byte.is_ascii_digit())
                .then(|| port.parse::<u32>().ok())
                .flatten()
                .filter(|number| *number <= 65535)
                .ok_or_else(invalid)?;
            Some(number)
        }
    };
    if host.is_empty() {
        return Err(invalid());
    }
    let is_loopback = match host.parse::<IpAddr>() {
        Ok(address) => address.is_loopback(),
        Err(_) => host == "localhost",
    };
    let allows_loopback = bool_at(rule, "allowLoopback");
    if scheme != "https" && !(allows_loopback && is_loopback && scheme == "http") {
        return Err(invalid());
    }
    if bool_at(rule, "forbidPort") && port.is_some() {
        return Err(invalid());
    }
    let suffix = string_at(rule, "allowedHostSuffix");
    let is_allowed_host = rule
        .get("allowedHosts")
        .and_then(JsonValue::as_array)
        .is_some_and(|hosts| hosts.iter().any(|candidate| candidate.as_str() == Some(netloc)));
    let is_allowed = (bool_at(rule, "allowAnyHTTPS") && scheme == "https")
        || (allows_loopback && is_loopback)
        || is_allowed_host
        || (!suffix.is_empty()
            && (netloc == suffix || netloc.ends_with(&format!(".{suffix}"))));
    if !is_allowed {
        return Err(invalid());
    }
    Ok(value.trim_end_matches('/').to_string())
}

/// Parses Go `time.ParseDuration` syntax into signed nanoseconds; empty is zero.
///
/// # Errors
///
/// Returns [`ValidationError`] for invalid syntax or an out-of-range value.
fn parse_go_duration(value: &str) -> Result<i128, ValidationError> {
    let invalid = || ValidationError::new("manifest grant duration is invalid");
    if value.is_empty() || matches!(value, "0" | "+0" | "-0") {
        return Ok(0);
    }
    let (sign, mut rest) = match value.strip_prefix('-') {
        Some(rest) => (-1, rest),
        None => (1, value.strip_prefix('+').unwrap_or(value)),
    };
    if rest.is_empty() {
        return Err(invalid());
    }
    let mut total: i128 = 0;
    while !rest.is_empty() {
        let number_end = rest
            .find(|character: char| !(character.is_ascii_digit() || character == '.'))
            .ok_or_else(invalid)?;
        let number = &rest[..number_end];
        let (whole, fraction) = number.split_once('.').unwrap_or((number, ""));
        if (whole.is_empty() && fraction.is_empty()) || fraction.contains('.') {
            return Err(invalid());
        }
        rest = &rest[number_end..];
        let units: [(&str, i128); 8] = [
            ("ns", 1),
            ("us", 1_000),
            ("µs", 1_000),
            ("μs", 1_000),
            ("ms", 1_000_000),
            ("s", 1_000_000_000),
            ("m", 60_000_000_000),
            ("h", 3_600_000_000_000),
        ];
        let (unit_text, unit) = units
            .iter()
            .find(|(name, _)| rest.starts_with(name))
            .copied()
            .ok_or_else(invalid)?;
        rest = &rest[unit_text.len()..];
        let whole_value: i128 = if whole.is_empty() {
            0
        } else {
            whole.parse().map_err(|_| invalid())?
        };
        total = total
            .checked_add(whole_value.checked_mul(unit).ok_or_else(invalid)?)
            .ok_or_else(invalid)?;
        if !fraction.is_empty() {
            let digits = fraction.len().min(30);
            let fraction_value: i128 = fraction[..digits].parse().map_err(|_| invalid())?;
            let scale = 10_i128.checked_pow(u32::try_from(digits).map_err(|_| invalid())?).ok_or_else(invalid)?;
            total += fraction_value.checked_mul(unit).ok_or_else(invalid)? / scale;
        }
        if total > i128::from(i64::MAX) + 1 {
            return Err(invalid());
        }
    }
    let maximum = if sign < 0 {
        i128::from(i64::MAX) + 1
    } else {
        i128::from(i64::MAX)
    };
    if total > maximum {
        return Err(invalid());
    }
    Ok(sign * total)
}

fn duration_from_nanoseconds(nanoseconds: i128) -> Duration {
    Duration::from_nanos(u64::try_from(nanoseconds.max(0)).unwrap_or(u64::MAX))
}

fn timestamp_from_unix_nanoseconds(nanoseconds: i128) -> Rfc3339NanoTimestamp {
    let magnitude = duration_from_nanoseconds(nanoseconds.abs());
    let instant = if nanoseconds >= 0 {
        UNIX_EPOCH.checked_add(magnitude)
    } else {
        UNIX_EPOCH.checked_sub(magnitude)
    };
    instant.map_or_else(Rfc3339NanoTimestamp::zero, |instant: SystemTime| {
        Rfc3339NanoTimestamp::from_system_time_utc(instant)
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_go_durations() {
        assert_eq!(parse_go_duration("1h").ok(), Some(3_600_000_000_000));
        assert_eq!(parse_go_duration("1.5s").ok(), Some(1_500_000_000));
        assert_eq!(parse_go_duration("10ms").ok(), Some(10_000_000));
        assert_eq!(parse_go_duration("-1s").ok(), Some(-1_000_000_000));
        assert_eq!(parse_go_duration("1h30m").ok(), Some(5_400_000_000_000));
        assert!(parse_go_duration("1").is_err());
        assert!(parse_go_duration("1d").is_err());
        assert!(parse_go_duration("3000000000h").is_err());
    }

    #[test]
    fn go_jwt_expiry_decoding() {
        let encode = |claims: &str| URL_SAFE_NO_PAD.encode(claims);
        let token = |payload: String| format!("e30.{payload}.sig");
        assert_eq!(read_jwt_expiry_seconds(&token(encode("{\"exp\":1700000000}"))), Some(1_700_000_000));
        assert_eq!(read_jwt_expiry_seconds(&token(encode("{\"exp\":1.9}"))), Some(1));
        assert_eq!(read_jwt_expiry_seconds(&token(encode("{\"exp\":true}"))), None);
        assert_eq!(read_jwt_expiry_seconds(&token(encode("{\"exp\":0}"))), None);
        assert_eq!(read_jwt_expiry_seconds("a.b"), None);
        let deep = format!("{}{}", "[".repeat(200), "]".repeat(200));
        assert_eq!(read_jwt_expiry_seconds(&token(encode(&deep))), None);
    }

    #[test]
    fn go_mail_addresses_and_trim() {
        assert!(is_go_mail_address("user@example.test"));
        assert!(is_go_mail_address("user@[127.0.0.1]"));
        assert!(!is_go_mail_address("user@[fe80::1%en0]"));
        assert!(!is_go_mail_address("a@b@c"));
        assert!(!is_go_mail_address("user.@example.test"));
        assert!(!is_go_mail_address("\"quoted\"@example.test"));
        assert_eq!(trim_go_space("\u{3000} value\u{85}"), "value");
    }

    #[test]
    fn template_patterns_match_ascii_and_full_values() {
        let rule = serde_json::json!({"patterns": ["\\w+"], "lowercase": true, "trimSpace": true});
        assert_eq!(apply_template_value_rule(" Tenant1 ", &rule).ok(), Some("tenant1".to_string()));
        assert!(apply_template_value_rule("tenänt", &rule).is_err());
        assert!(apply_template_value_rule("tenant/x", &rule).is_err());
        let rejected = serde_json::json!({"reject": ["common"]});
        assert!(apply_template_value_rule("COMMON", &rejected).is_err());
        for invalid in [r"(?i)x", r"\1", r"\pL", r"[[:alpha:]]", "("] {
            assert!(validate_template_value_rule(&serde_json::json!({"patterns": [invalid]})).is_err());
        }
        assert_eq!(python_quote("a b/c:d"), "a%20b%2Fc:d");
        assert_eq!(template_placeholders("https://{tenant}.example/{x}/{}"), vec!["tenant", "x"]);
    }

    #[test]
    fn response_urls_follow_rules() {
        let rule = serde_json::json!({"credential": "instance_url", "allowedHostSuffix": "example.test", "forbidPort": true});
        assert_eq!(
            validate_response_url("https://a.example.test/", &rule).ok(),
            Some("https://a.example.test".to_string())
        );
        assert!(validate_response_url("https://a.example.test:443", &rule).is_err());
        assert!(validate_response_url("https://evil.test", &rule).is_err());
        assert!(validate_response_url("https://a.example.test/path", &rule).is_err());
        let loopback = serde_json::json!({"credential": "x", "allowLoopback": true});
        assert!(validate_response_url("http://127.0.0.1:8080", &loopback).is_ok());
        let zoho = serde_json::json!({"credential": "x", "allowedHosts": ["www.zohoapis.com"]});
        assert!(validate_response_url("https://WWW.zohoapis.com", &zoho).is_err());
    }
}
