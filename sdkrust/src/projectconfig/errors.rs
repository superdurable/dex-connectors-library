// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Project configuration, storage, broker, and credential provider failures.

use std::fmt;

/// Failure while loading project configuration or resolving project credentials.
///
/// Messages never contain credential material or provider response bodies.
#[derive(Debug)]
#[non_exhaustive]
pub enum ProjectConfigError {
    /// A document, key, reference, or argument is invalid.
    Invalid(String),
    /// The requested project object or exact version does not exist.
    ObjectNotFound,
    /// A conditional create or compare-and-swap lost to another writer.
    ObjectConflict(String),
    /// A conditional write may have reached storage; reconcile before retrying.
    OutcomeUnknown(String),
    /// Local file or storage transport failed.
    Io(String),
    /// Transient credential broker transport or service failure; retry later.
    BrokerUnavailable(String),
    /// Terminal broker refusal or invalid broker answer.
    BrokerRejected(String),
    /// The broker no longer selects this shared connection for the scope.
    ConnectionNotSelected,
    /// A forced read returned the revision the provider rejected, or the rejection does not
    /// prove expiry.
    RejectedCredentialNotRefreshed(String),
    /// The broker does not relay this shared connection's provider events.
    RelayedTriggerNotRelayed,
    /// A relayed request's signature is missing, malformed, stale, or does not match.
    RelaySignatureInvalid,
    /// A relay signature mismatch arrived within 30 seconds of the last token reread; this is
    /// retryable, like [`ProjectConfigError::BrokerUnavailable`].
    RelayTokenRenewalDeferred,
    /// Refresh failed without proving the prior credential invalid; the prior one is kept.
    RefreshFailed(String),
    /// Another invocation owns provider dispatch for this credential generation.
    ExchangePending,
}

impl ProjectConfigError {
    /// Reports whether retrying later may succeed without operator action.
    pub fn is_retryable(&self) -> bool {
        matches!(
            self,
            ProjectConfigError::OutcomeUnknown(_)
                | ProjectConfigError::Io(_)
                | ProjectConfigError::BrokerUnavailable(_)
                | ProjectConfigError::RelayTokenRenewalDeferred
                | ProjectConfigError::RefreshFailed(_)
                | ProjectConfigError::ExchangePending
        )
    }

    pub(crate) fn invalid(message: impl Into<String>) -> Self {
        ProjectConfigError::Invalid(message.into())
    }
}

impl fmt::Display for ProjectConfigError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            ProjectConfigError::Invalid(message)
            | ProjectConfigError::ObjectConflict(message)
            | ProjectConfigError::OutcomeUnknown(message)
            | ProjectConfigError::Io(message)
            | ProjectConfigError::BrokerUnavailable(message)
            | ProjectConfigError::BrokerRejected(message)
            | ProjectConfigError::RejectedCredentialNotRefreshed(message)
            | ProjectConfigError::RefreshFailed(message) => formatter.write_str(message),
            ProjectConfigError::ObjectNotFound => formatter.write_str("project object not found"),
            ProjectConfigError::ConnectionNotSelected => {
                formatter.write_str("project connection is not selected by this scope")
            }
            ProjectConfigError::RelayedTriggerNotRelayed => formatter
                .write_str("the credential broker does not relay this connection's events"),
            ProjectConfigError::RelaySignatureInvalid => {
                formatter.write_str("relayed request signature is missing, invalid or stale")
            }
            ProjectConfigError::RelayTokenRenewalDeferred => formatter.write_str(
                "relayed request signature cannot be checked against a renewed scope token yet",
            ),
            ProjectConfigError::ExchangePending => {
                formatter.write_str("credential authorization exchange is pending")
            }
        }
    }
}

impl std::error::Error for ProjectConfigError {}

impl From<crate::errors::ValidationError> for ProjectConfigError {
    fn from(error: crate::errors::ValidationError) -> Self {
        ProjectConfigError::Invalid(error.message().to_string())
    }
}

/// Result of a project configuration operation.
pub type ProjectConfigResult<T> = Result<T, ProjectConfigError>;
