// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Dex Execute retry defaults for connector operations.

use std::time::Duration;

use serde::{Deserialize, Serialize};

use crate::errors::ValidationError;

/// Execute retry schedule; zero attempts means unlimited within the total duration.
///
/// The serde form is Dex Go's `dex.RetryPolicy` JSON: capitalized field names and integer
/// nanosecond durations, as in `conformance/retry-policy.json`. Convert to the Dex Rust SDK
/// policy with [`RetryPolicy::to_dex_retry_policy`].
#[derive(Clone, Copy, Debug, Deserialize, PartialEq, Serialize)]
#[serde(deny_unknown_fields)]
pub struct RetryPolicy {
    /// Delay before the first retry.
    #[serde(rename = "InitialInterval", with = "duration_nanoseconds")]
    pub initial_interval: Duration,
    /// Multiplier applied to each later delay.
    #[serde(rename = "BackoffCoefficient", with = "go_float")]
    pub backoff_coefficient: f64,
    /// Upper bound of one retry delay.
    #[serde(rename = "MaximumInterval", with = "duration_nanoseconds")]
    pub maximum_interval: Duration,
    /// Maximum attempts; zero means no attempt count limit.
    #[serde(rename = "MaximumAttempts")]
    pub maximum_attempts: u32,
    /// Total retry budget measured from the first attempt.
    #[serde(rename = "TotalDuration", with = "duration_nanoseconds")]
    pub total_duration: Duration,
}

impl RetryPolicy {
    /// Rejects a negative or non-finite backoff coefficient.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid coefficient.
    pub fn validate(&self) -> Result<(), ValidationError> {
        if !self.backoff_coefficient.is_finite() || self.backoff_coefficient < 0.0 {
            return Err(ValidationError::new("invalid connector retry policy"));
        }
        Ok(())
    }

    /// Converts this schedule to the Dex Rust SDK policy used in Step options.
    pub fn to_dex_retry_policy(&self) -> dex_sdk::RetryPolicy {
        dex_sdk::RetryPolicy::new()
            .initial_interval(self.initial_interval)
            .backoff_coefficient(self.backoff_coefficient)
            .maximum_interval(self.maximum_interval)
            .maximum_attempts(self.maximum_attempts)
            .total_duration(self.total_duration)
    }
}

/// Returns a fresh one-hour retry policy with one-second exponential backoff.
///
/// It starts at one second, doubles to five minutes, has no attempt limit, and ends after one
/// hour. Exhaustion fails the Flow.
pub fn default_connector_step_execute_retry_policy() -> RetryPolicy {
    RetryPolicy {
        initial_interval: Duration::from_secs(1),
        backoff_coefficient: 2.0,
        maximum_interval: Duration::from_secs(5 * 60),
        maximum_attempts: 0,
        total_duration: Duration::from_secs(60 * 60),
    }
}

pub(crate) mod duration_nanoseconds {
    use std::time::Duration;

    use serde::{Deserialize, Deserializer, Serializer};

    pub(crate) fn serialize<S: Serializer>(
        value: &Duration,
        serializer: S,
    ) -> Result<S::Ok, S::Error> {
        let nanoseconds = i64::try_from(value.as_nanos())
            .map_err(|_| serde::ser::Error::custom("duration exceeds Go time.Duration"))?;
        serializer.serialize_i64(nanoseconds)
    }

    pub(crate) fn deserialize<'de, D: Deserializer<'de>>(
        deserializer: D,
    ) -> Result<Duration, D::Error> {
        let nanoseconds = i64::deserialize(deserializer)?;
        let nanoseconds = u64::try_from(nanoseconds)
            .map_err(|_| serde::de::Error::custom("duration cannot be negative"))?;
        Ok(Duration::from_nanos(nanoseconds))
    }
}

/// Encodes an integral float as a JSON integer, as Go's `encoding/json` does.
pub(crate) mod go_float {
    use serde::{Deserialize, Deserializer, Serializer};

    #[expect(
        clippy::float_cmp,
        clippy::cast_possible_truncation,
        reason = "an exact integral check selects Go's integer rendering"
    )]
    pub(crate) fn serialize<S: Serializer>(value: &f64, serializer: S) -> Result<S::Ok, S::Error> {
        if value.is_finite() && value.trunc() == *value && value.abs() < 1e15 {
            serializer.serialize_i64(*value as i64)
        } else {
            serializer.serialize_f64(*value)
        }
    }

    pub(crate) fn deserialize<'de, D: Deserializer<'de>>(deserializer: D) -> Result<f64, D::Error> {
        f64::deserialize(deserializer)
    }
}
