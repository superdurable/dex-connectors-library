// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Secret-safe provider failure values and retry classification.

use std::fmt;

use serde::{Deserialize, Serialize};

/// Provider or local fact; a kind alone does not choose a Step route.
///
/// The wire form is the upper snake case name, such as `NOT_FOUND`, matching sdkgo.
#[derive(Clone, Copy, Debug, Deserialize, Eq, Hash, PartialEq, Serialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum FailureKind {
    /// The application or provider rejected the input.
    Validation,
    /// The provider rejected the credential.
    Authentication,
    /// The credential lacks permission for the operation.
    Authorization,
    /// The provider resource does not exist.
    NotFound,
    /// The provider reported a conflicting state.
    Conflict,
    /// The provider throttled the request.
    RateLimit,
    /// A provider quota is exhausted.
    QuotaExhausted,
    /// The provider is temporarily unavailable.
    Availability,
    /// The provider refused the request for a business reason.
    ProviderRejection,
    /// The connection failed before, during, or after dispatch.
    Transport,
    /// The provider response exceeded the configured limit.
    ResponseTooLarge,
    /// The provider response violated its protocol.
    Protocol,
    /// The connector or application has a local defect.
    LocalDefect,
}

impl FailureKind {
    /// Every kind in sdkgo declaration order.
    pub const ALL: [FailureKind; 13] = [
        FailureKind::Validation,
        FailureKind::Authentication,
        FailureKind::Authorization,
        FailureKind::NotFound,
        FailureKind::Conflict,
        FailureKind::RateLimit,
        FailureKind::QuotaExhausted,
        FailureKind::Availability,
        FailureKind::ProviderRejection,
        FailureKind::Transport,
        FailureKind::ResponseTooLarge,
        FailureKind::Protocol,
        FailureKind::LocalDefect,
    ];

    /// Returns the wire name, such as `RATE_LIMIT`.
    pub fn as_wire_str(self) -> &'static str {
        match self {
            FailureKind::Validation => "VALIDATION",
            FailureKind::Authentication => "AUTHENTICATION",
            FailureKind::Authorization => "AUTHORIZATION",
            FailureKind::NotFound => "NOT_FOUND",
            FailureKind::Conflict => "CONFLICT",
            FailureKind::RateLimit => "RATE_LIMIT",
            FailureKind::QuotaExhausted => "QUOTA_EXHAUSTED",
            FailureKind::Availability => "AVAILABILITY",
            FailureKind::ProviderRejection => "PROVIDER_REJECTION",
            FailureKind::Transport => "TRANSPORT",
            FailureKind::ResponseTooLarge => "RESPONSE_TOO_LARGE",
            FailureKind::Protocol => "PROTOCOL",
            FailureKind::LocalDefect => "LOCAL_DEFECT",
        }
    }
}

impl fmt::Display for FailureKind {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(self.as_wire_str())
    }
}

/// Secret-safe failure that may be persisted in a Flow result.
///
/// Every field is required and nonempty. The message must not contain credentials or
/// provider payloads that may hold them.
#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(deny_unknown_fields)]
pub struct Failure {
    /// The fact the provider or connector observed.
    pub kind: FailureKind,
    /// The connector ID that produced the failure.
    pub provider: String,
    /// The operation ID that produced the failure.
    pub operation: String,
    /// A bounded, secret-safe explanation.
    pub message: String,
}

impl Failure {
    /// Creates a failure from its four fields without validating them.
    pub fn new(
        kind: FailureKind,
        provider: impl Into<String>,
        operation: impl Into<String>,
        message: impl Into<String>,
    ) -> Self {
        Self {
            kind,
            provider: provider.into(),
            operation: operation.into(),
            message: message.into(),
        }
    }

    /// Rejects empty identity or message fields.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] when the provider, operation, or message is empty.
    pub fn validate(&self) -> Result<(), ValidationError> {
        if self.provider.is_empty() || self.operation.is_empty() || self.message.is_empty() {
            return Err(ValidationError::new(
                "failure provider, operation, and message are required",
            ));
        }
        Ok(())
    }
}

/// Invalid connector value detected before it could enter Flow state.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ValidationError {
    message: String,
}

impl ValidationError {
    /// Creates a validation error with a secret-safe message.
    pub fn new(message: impl Into<String>) -> Self {
        Self {
            message: message.into(),
        }
    }

    /// Returns the secret-safe message.
    pub fn message(&self) -> &str {
        &self.message
    }
}

impl fmt::Display for ValidationError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(&self.message)
    }
}

impl std::error::Error for ValidationError {}

/// Retryable connector failure; Step factories map it to a Dex retry.
///
/// Retry exhaustion is handled by Dex and fails the Flow. A retry is never serialized as a
/// terminal result.
#[derive(Clone, Debug, PartialEq)]
pub struct RetryError {
    failure: Failure,
    retry_after_seconds: f64,
}

impl RetryError {
    /// Creates a retry with an optional nonnegative delay hint in seconds.
    ///
    /// Zero means no explicit hint.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid failure or a negative or non-finite delay.
    pub fn new(failure: Failure, retry_after_seconds: f64) -> Result<Self, ValidationError> {
        failure.validate()?;
        if !retry_after_seconds.is_finite() || retry_after_seconds < 0.0 {
            return Err(ValidationError::new(
                "retry delay must be finite and nonnegative",
            ));
        }
        Ok(Self {
            failure,
            retry_after_seconds,
        })
    }

    /// Returns the safe failure that caused the retry.
    pub fn failure(&self) -> &Failure {
        &self.failure
    }

    /// Returns the requested delay in seconds, possibly fractional.
    pub fn retry_after_seconds(&self) -> f64 {
        self.retry_after_seconds
    }

    /// Returns the whole-second delay that Dex can serialize, truncating fractions.
    ///
    /// Delays beyond `i32::MAX` seconds saturate.
    #[expect(
        clippy::cast_possible_truncation,
        reason = "truncation toward zero is the documented whole-second contract"
    )]
    pub fn retry_after_whole_seconds(&self) -> i32 {
        let truncated = self.retry_after_seconds.trunc();
        if truncated >= f64::from(i32::MAX) {
            i32::MAX
        } else {
            truncated as i32
        }
    }
}

impl fmt::Display for RetryError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            formatter,
            "connector {} {} retry ({}): {}",
            self.failure.provider, self.failure.operation, self.failure.kind, self.failure.message
        )
    }
}

impl std::error::Error for RetryError {}
