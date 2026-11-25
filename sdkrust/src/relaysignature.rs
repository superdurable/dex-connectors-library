// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Sign and verify requests a Dex host relays to an application with its scope token.
//!
//! The scheme matches the Go, Python, and TypeScript Connector SDKs: `Dex-Relay-Timestamp`
//! carries the Unix second of signing and `Dex-Relay-Signature` carries `v1=` and the
//! hexadecimal HMAC-SHA256 of `v1:<timestamp>:<raw body>` keyed with the scope token. A request
//! is valid only when the signature matches in constant time and the timestamp is within five
//! minutes of receipt. These functions never log, store, or return the token. Applications
//! with a loaded project use
//! [`LoadedProject::verify_relayed_request`](crate::projectconfig::LoadedProject::verify_relayed_request),
//! which reads the scope token and rereads it after a mismatch.
//!
//! ```
//! use std::time::{Duration, UNIX_EPOCH};
//! use dex_connectors_sdk::relaysignature::{
//!     RELAY_SIGNATURE_HEADER, RELAY_TIMESTAMP_HEADER, sign_relayed_request, verify_relay_signature,
//! };
//!
//! let signed_at = UNIX_EPOCH + Duration::from_secs(1_800_000_000);
//! let signature = sign_relayed_request("INERT_TOKEN", signed_at, b"{}");
//! let headers = vec![
//!     (RELAY_TIMESTAMP_HEADER.to_string(), "1800000000".to_string()),
//!     (RELAY_SIGNATURE_HEADER.to_string(), signature),
//! ];
//! assert!(verify_relay_signature("INERT_TOKEN", &headers, b"{}", signed_at).is_valid());
//! ```

use std::time::{Duration, SystemTime, UNIX_EPOCH};

use hmac::{Hmac, Mac};
use sha2::Sha256;

/// Header carrying the Unix second at which the host signed a relayed request.
pub const RELAY_TIMESTAMP_HEADER: &str = "Dex-Relay-Timestamp";
/// Header carrying `v1=` and the hexadecimal HMAC-SHA256 of `v1:<timestamp>:<body>`.
pub const RELAY_SIGNATURE_HEADER: &str = "Dex-Relay-Signature";
/// How far a timestamp may be from the receiver's clock; the boundary is valid.
pub const RELAY_TIMESTAMP_TOLERANCE: Duration = Duration::from_secs(5 * 60);

/// Request headers as name and value pairs; names match case-insensitively and the first
/// value of a name wins.
pub type RelayHeaders = [(String, String)];

const VERSION: &str = "v1";

/// Why a relayed request is refused.
#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq)]
pub enum RelaySignatureRefusal {
    /// A relay header is absent or empty.
    Missing,
    /// A relay header does not have the v1 syntax.
    Malformed,
    /// The timestamp is more than five minutes from receipt.
    Stale,
    /// The signature does not match the token over the raw body.
    Mismatch,
}

impl RelaySignatureRefusal {
    /// Returns the shared lowercase name, such as `stale`.
    pub fn as_str(self) -> &'static str {
        match self {
            RelaySignatureRefusal::Missing => "missing",
            RelaySignatureRefusal::Malformed => "malformed",
            RelaySignatureRefusal::Stale => "stale",
            RelaySignatureRefusal::Mismatch => "mismatch",
        }
    }
}

/// The signed parts of one relayed request, parsed before the token is needed.
#[derive(Clone, Eq, PartialEq)]
pub struct ParsedRelaySignature {
    /// The timestamp header text, used verbatim in the signed message.
    pub timestamp: String,
    /// When the host signed the request.
    pub signed_at: SystemTime,
    signature: Vec<u8>,
}

impl std::fmt::Debug for ParsedRelaySignature {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        formatter
            .debug_struct("ParsedRelaySignature")
            .field("timestamp", &self.timestamp)
            .field("signed_at", &self.signed_at)
            .finish_non_exhaustive()
    }
}

/// The outcome of verifying one relayed request.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RelaySignatureVerification {
    /// When the host signed a valid request; `None` for a refusal.
    pub signed_at: Option<SystemTime>,
    /// Why the request is refused; `None` for a valid request.
    pub refusal: Option<RelaySignatureRefusal>,
}

impl RelaySignatureVerification {
    /// Reports whether the request is validly signed.
    pub fn is_valid(&self) -> bool {
        self.refusal.is_none()
    }
}

fn unix_seconds_floor(value: SystemTime) -> i64 {
    match value.duration_since(UNIX_EPOCH) {
        Ok(after) => i64::try_from(after.as_secs()).unwrap_or(i64::MAX),
        Err(before) => {
            let duration = before.duration();
            let seconds = i64::try_from(duration.as_secs()).unwrap_or(i64::MAX);
            if duration.subsec_nanos() > 0 {
                -seconds - 1
            } else {
                -seconds
            }
        }
    }
}

fn unix_microseconds(value: SystemTime) -> i128 {
    match value.duration_since(UNIX_EPOCH) {
        Ok(after) => i128::try_from(after.as_micros()).unwrap_or(i128::MAX),
        Err(before) => -i128::try_from(before.duration().as_micros()).unwrap_or(i128::MAX),
    }
}

fn relay_header<'headers>(headers: &'headers RelayHeaders, name: &str) -> &'headers str {
    headers
        .iter()
        .find(|(key, _)| key.eq_ignore_ascii_case(name))
        .map_or("", |(_, value)| value.as_str())
}

fn relay_signature(token: &str, timestamp: &str, body: &[u8]) -> Vec<u8> {
    // HMAC accepts keys of any length, so construction cannot fail.
    let Ok(mut mac) = Hmac::<Sha256>::new_from_slice(token.as_bytes()) else {
        return Vec::new();
    };
    mac.update(format!("{VERSION}:{timestamp}:").as_bytes());
    mac.update(body);
    mac.finalize().into_bytes().to_vec()
}

fn to_lower_hex(bytes: &[u8]) -> String {
    bytes.iter().map(|byte| format!("{byte:02x}")).collect()
}

fn decode_hex(value: &str) -> Option<Vec<u8>> {
    if value.len() != 64 || !value.bytes().all(|byte| byte.is_ascii_hexdigit()) {
        return None;
    }
    (0..value.len())
        .step_by(2)
        .map(|index| u8::from_str_radix(&value[index..index + 2], 16).ok())
        .collect()
}

/// Returns the `Dex-Relay-Signature` value for `body` signed at the Unix second of
/// `signed_at`; send the same second in `Dex-Relay-Timestamp`.
pub fn sign_relayed_request(token: &str, signed_at: SystemTime, body: &[u8]) -> String {
    let seconds = unix_seconds_floor(signed_at);
    format!(
        "{VERSION}={}",
        to_lower_hex(&relay_signature(token, &seconds.to_string(), body))
    )
}

/// Parses the relay headers and checks the timestamp window without the token.
///
/// # Errors
///
/// Returns the refusal for an unsigned, malformed, or stale request.
pub fn parse_relay_signature(
    headers: &RelayHeaders,
    received_at: SystemTime,
) -> Result<ParsedRelaySignature, RelaySignatureRefusal> {
    let timestamp = relay_header(headers, RELAY_TIMESTAMP_HEADER);
    let signature_header = relay_header(headers, RELAY_SIGNATURE_HEADER);
    if timestamp.is_empty() || signature_header.is_empty() {
        return Err(RelaySignatureRefusal::Missing);
    }
    let digits = timestamp.strip_prefix('-').unwrap_or(timestamp);
    if digits.is_empty() || digits.len() > 15 || !digits.bytes().all(|byte| byte.is_ascii_digit()) {
        return Err(RelaySignatureRefusal::Malformed);
    }
    let Some(encoded) = signature_header.strip_prefix("v1=") else {
        return Err(RelaySignatureRefusal::Malformed);
    };
    let signature = decode_hex(encoded).ok_or(RelaySignatureRefusal::Malformed)?;
    let seconds: i64 = timestamp
        .parse()
        .map_err(|_| RelaySignatureRefusal::Malformed)?;
    let difference = unix_microseconds(received_at) - i128::from(seconds) * 1_000_000;
    if difference.abs() > i128::from(RELAY_TIMESTAMP_TOLERANCE.as_secs()) * 1_000_000 {
        return Err(RelaySignatureRefusal::Stale);
    }
    let signed_at = if seconds >= 0 {
        UNIX_EPOCH + Duration::from_secs(seconds.unsigned_abs())
    } else {
        UNIX_EPOCH - Duration::from_secs(seconds.unsigned_abs())
    };
    Ok(ParsedRelaySignature {
        timestamp: timestamp.to_string(),
        signed_at,
        signature,
    })
}

/// Reports whether a parsed signature matches `token` over the raw body, in constant time.
pub fn matches_relay_signature(parsed: &ParsedRelaySignature, token: &str, body: &[u8]) -> bool {
    let Ok(mut mac) = Hmac::<Sha256>::new_from_slice(token.as_bytes()) else {
        return false;
    };
    mac.update(format!("{VERSION}:{}:", parsed.timestamp).as_bytes());
    mac.update(body);
    mac.verify_slice(&parsed.signature).is_ok()
}

/// Verifies one relayed request's headers and raw body against `token` at `received_at`.
pub fn verify_relay_signature(
    token: &str,
    headers: &RelayHeaders,
    body: &[u8],
    received_at: SystemTime,
) -> RelaySignatureVerification {
    match parse_relay_signature(headers, received_at) {
        Err(refusal) => RelaySignatureVerification {
            signed_at: None,
            refusal: Some(refusal),
        },
        Ok(parsed) if !matches_relay_signature(&parsed, token, body) => {
            RelaySignatureVerification {
                signed_at: None,
                refusal: Some(RelaySignatureRefusal::Mismatch),
            }
        }
        Ok(parsed) => RelaySignatureVerification {
            signed_at: Some(parsed.signed_at),
            refusal: None,
        },
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn headers(timestamp: &str, signature: &str) -> Vec<(String, String)> {
        vec![
            ("dex-relay-timestamp".into(), timestamp.into()),
            ("DEX-RELAY-SIGNATURE".into(), signature.into()),
        ]
    }

    #[test]
    fn signs_and_classifies_relayed_requests() {
        let signed_at = UNIX_EPOCH + Duration::from_secs(1_800_000_000);
        let signature = sign_relayed_request("token", signed_at, b"body");
        assert!(signature.starts_with("v1=") && signature.len() == 67);
        let valid = verify_relay_signature(
            "token",
            &headers("1800000000", &signature),
            b"body",
            signed_at + RELAY_TIMESTAMP_TOLERANCE,
        );
        assert_eq!(valid.signed_at, Some(signed_at));
        let cases = [
            (headers("", &signature), RelaySignatureRefusal::Missing),
            (
                headers("18e8", &signature),
                RelaySignatureRefusal::Malformed,
            ),
            (
                headers("1800000000", "v2=00"),
                RelaySignatureRefusal::Malformed,
            ),
            (
                headers("1799999000", &signature),
                RelaySignatureRefusal::Stale,
            ),
        ];
        for (request_headers, refusal) in cases {
            let result = verify_relay_signature("token", &request_headers, b"body", signed_at);
            assert_eq!(result.refusal, Some(refusal));
        }
        let mismatch = verify_relay_signature(
            "other",
            &headers("1800000000", &signature),
            b"body",
            signed_at,
        );
        assert_eq!(mismatch.refusal, Some(RelaySignatureRefusal::Mismatch));
    }
}
