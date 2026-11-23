// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Secret-safe credential lookup and refresh interfaces.

use std::collections::HashMap;
use std::error::Error;
use std::fmt;

use crate::timestamps::Rfc3339NanoTimestamp;
use crate::types::{Call, ConnectionRef};

/// Boxed error returned by credential providers and refresh drivers.
pub type CredentialError = Box<dyn Error + Send + Sync + 'static>;

/// Credential wrapper that reveals only at a provider request boundary.
///
/// `Display` and `Debug` render `[REDACTED]`, and the type implements neither `Serialize`
/// nor `Clone`-free copies into logs.
#[derive(Clone, Eq, PartialEq)]
pub struct SecretString(String);

impl SecretString {
    /// Wraps a secret.
    pub fn new(value: impl Into<String>) -> Self {
        Self(value.into())
    }

    /// Returns the secret for immediate provider request construction.
    pub fn reveal(&self) -> &str {
        &self.0
    }
}

impl fmt::Display for SecretString {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("[REDACTED]")
    }
}

impl fmt::Debug for SecretString {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("SecretString([REDACTED])")
    }
}

/// Terminal credential state requiring an interactive authorization flow.
///
/// Its message never includes provider details or credentials.
#[derive(Default)]
pub struct ReauthorizationRequiredError {
    cause: Option<CredentialError>,
}

impl fmt::Debug for ReauthorizationRequiredError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        // The cause may quote provider responses, so only its presence is shown.
        formatter
            .debug_struct("ReauthorizationRequiredError")
            .field("has_cause", &self.cause.is_some())
            .finish()
    }
}

impl ReauthorizationRequiredError {
    /// Creates the error with an optional safe cause kept for diagnostics.
    pub fn new(cause: Option<CredentialError>) -> Self {
        Self { cause }
    }
}

impl fmt::Display for ReauthorizationRequiredError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("connector reauthorization is required")
    }
}

impl Error for ReauthorizationRequiredError {
    fn source(&self) -> Option<&(dyn Error + 'static)> {
        self.cause
            .as_deref()
            .map(|cause| cause as &(dyn Error + 'static))
    }
}

/// Returns whether `error` or any source in its chain is a [`ReauthorizationRequiredError`].
pub fn is_reauthorization_required(error: &(dyn Error + 'static)) -> bool {
    let mut current = Some(error);
    let mut depth = 0;
    while let Some(candidate) = current {
        if candidate.is::<ReauthorizationRequiredError>() {
            return true;
        }
        depth += 1;
        if depth >= 32 {
            return false;
        }
        current = candidate.source();
    }
    false
}

/// Current credential material, optional expiry, and decision time.
pub struct CredentialRefreshState<'state, Credential> {
    /// Current material.
    pub credentials: &'state Credential,
    /// Expiry of the current material, when known.
    pub expires_at: Option<Rfc3339NanoTimestamp>,
    /// Decision time in UTC.
    pub now: Rfc3339NanoTimestamp,
}

/// Complete replacement credential material and its required expiry.
pub struct CredentialRefreshResult<Credential> {
    /// Replacement material; callers persist it atomically.
    pub credentials: Credential,
    /// Expiry of the replacement material.
    pub expires_at: Rfc3339NanoTimestamp,
}

impl<Credential> fmt::Debug for CredentialRefreshResult<Credential> {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("CredentialRefreshResult")
            .field("credentials", &"[REDACTED]")
            .field("expires_at", &self.expires_at)
            .finish()
    }
}

/// Connector-specific refresh decision and provider exchange without storage.
pub trait CredentialRefreshDriver<Credential>: Send + Sync {
    /// Decides deterministically whether this credential needs refresh.
    fn is_refresh_required(&self, state: &CredentialRefreshState<'_, Credential>) -> bool;

    /// Exchanges credentials; callers persist the complete result atomically.
    ///
    /// # Errors
    ///
    /// Returns [`ReauthorizationRequiredError`] when interactive authorization is required, or
    /// another error for a retryable exchange failure.
    fn refresh(
        &self,
        state: &CredentialRefreshState<'_, Credential>,
    ) -> Result<CredentialRefreshResult<Credential>, CredentialError>;
}

/// Resolves current credentials at call time, outside durable Flow state.
///
/// Implementations that support refresh override [`Self::resolve_with_refresh`] and
/// [`Self::resolve_after_rejection`]; refreshing providers serialize per connection, reload
/// after taking that lock, and persist complete replacement credentials atomically.
pub trait CredentialProvider<Credential>: Send + Sync {
    /// Returns connector-specific credentials for one provider call.
    ///
    /// # Errors
    ///
    /// Returns an error for a missing connection or unavailable store.
    fn resolve(&self, call: &Call<'_>) -> Result<Credential, CredentialError>;

    /// Returns a usable credential, refreshing it when required.
    ///
    /// The default performs an ordinary lookup.
    ///
    /// # Errors
    ///
    /// Returns the lookup or refresh error.
    fn resolve_with_refresh(
        &self,
        call: &Call<'_>,
        driver: &dyn CredentialRefreshDriver<Credential>,
    ) -> Result<Credential, CredentialError> {
        let _ = driver;
        self.resolve(call)
    }

    /// Reports whether [`Self::resolve_after_rejection`] forces a refresh.
    fn supports_refresh_after_rejection(&self) -> bool {
        false
    }

    /// Reloads and replaces rejected material under the connection lock.
    ///
    /// # Errors
    ///
    /// The default returns an error because refresh after rejection is unsupported.
    fn resolve_after_rejection(
        &self,
        call: &Call<'_>,
        driver: &dyn CredentialRefreshDriver<Credential>,
    ) -> Result<Credential, CredentialError> {
        let _ = (call, driver);
        Err("credential provider does not support refresh after rejection".into())
    }
}

/// Resolves current credentials through coordinated refresh when the provider supports it.
///
/// # Errors
///
/// Returns the provider's lookup or refresh error.
pub fn resolve_credential<Credential>(
    provider: &dyn CredentialProvider<Credential>,
    call: &Call<'_>,
    driver: &dyn CredentialRefreshDriver<Credential>,
) -> Result<Credential, CredentialError> {
    provider.resolve_with_refresh(call, driver)
}

/// Forces one coordinated refresh after a provider authentication rejection.
///
/// A connector calls this at most once per provider operation. Missing refresh support is an
/// error, so rejected credentials are never silently reused.
///
/// # Errors
///
/// Returns an error when the provider cannot refresh after rejection, or its refresh error.
pub fn resolve_credential_after_rejection<Credential>(
    provider: &dyn CredentialProvider<Credential>,
    call: &Call<'_>,
    driver: &dyn CredentialRefreshDriver<Credential>,
) -> Result<Credential, CredentialError> {
    if !provider.supports_refresh_after_rejection() {
        return Err("credential provider does not support refresh after rejection".into());
    }
    provider.resolve_after_rejection(call, driver)
}

/// Resolves in-memory credentials by logical connection without logging them.
pub struct StaticCredentialProvider<Credential> {
    credentials: HashMap<ConnectionRef, Credential>,
}

impl<Credential> StaticCredentialProvider<Credential> {
    /// Creates a provider over a connection map.
    pub fn new(credentials: HashMap<ConnectionRef, Credential>) -> Self {
        Self { credentials }
    }
}

impl<Credential: Clone + Send + Sync> CredentialProvider<Credential>
    for StaticCredentialProvider<Credential>
{
    fn resolve(&self, call: &Call<'_>) -> Result<Credential, CredentialError> {
        self.credentials
            .get(&call.connection)
            .cloned()
            .ok_or_else(|| "connection is not configured".into())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reauthorization_debug_hides_the_cause() {
        let error = ReauthorizationRequiredError::new(Some("token=INERT_SECRET rejected".into()));
        let rendered = format!("{error:?} {error}");
        assert!(!rendered.contains("INERT_SECRET"), "{rendered}");
        assert!(is_reauthorization_required(&error));
    }
}
