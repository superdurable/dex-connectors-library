// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Wire-compatible single-dispatch markers and conservative resend decisions.

use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};
use serde_json::Value as JsonValue;

use crate::errors::ValidationError;
use crate::types::CallId;

const CHECKPOINT_MARKERS_FIELD: &str = "singleDispatchMarkers";

/// Smallest send cap; one send disables bounded resend.
pub const MINIMUM_MAX_SENDS: u32 = 1;
/// Largest send cap accepted by the Fence and the Step override.
pub const MAXIMUM_MAX_SENDS: u32 = 10;

/// Last known provider dispatch state for one Execute attempt.
#[derive(Clone, Copy, Debug, Deserialize, Eq, Hash, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub enum State {
    /// The attempt proved it did not send.
    NotSent,
    /// The attempt may have sent.
    Claimed,
    /// The provider proved it did not process the send.
    CanResend,
}

/// Non-secret Dex heartbeat checkpoint for a logical call and attempt.
///
/// `send_count` is the number of possibly effective sends this Call ID made up to and
/// including `attempt`. The marker JSON omits an empty provider ID and a zero count; a
/// checkpoint entry always carries the exact count.
#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Marker {
    /// Stable call identity.
    pub call_id: CallId,
    /// One-based Dex attempt.
    pub attempt: u32,
    /// Dispatch state.
    pub state: State,
    /// Accepted provider resource identity; omitted when empty.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub provider_id: String,
    /// Possibly effective sends; omitted when zero.
    #[serde(default, skip_serializing_if = "is_zero")]
    pub send_count: u32,
}

fn is_zero(value: &u32) -> bool {
    *value == 0
}

impl Marker {
    /// Requires a UUID Call ID and a positive attempt.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid marker.
    pub fn validate(&self) -> Result<(), ValidationError> {
        self.call_id.validate()?;
        if self.attempt < 1 {
            return Err(ValidationError::new("invalid single-dispatch checkpoint"));
        }
        Ok(())
    }

    /// Decodes a marker strictly, failing closed on unknown or malformed fields.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for malformed data.
    pub fn from_json(value: &JsonValue) -> Result<Self, ValidationError> {
        let invalid = || ValidationError::new("invalid single-dispatch checkpoint");
        let object = value.as_object().ok_or_else(invalid)?;
        for key in object.keys() {
            if !matches!(
                key.as_str(),
                "callId" | "attempt" | "state" | "providerId" | "sendCount"
            ) {
                return Err(invalid());
            }
        }
        let integer = |field: &str, default: Option<u32>| -> Result<u32, ValidationError> {
            match object.get(field) {
                None => default.ok_or_else(invalid),
                Some(JsonValue::Number(number)) => number
                    .as_u64()
                    .and_then(|value| u32::try_from(value).ok())
                    .ok_or_else(invalid),
                Some(_) => Err(invalid()),
            }
        };
        let call_id = object
            .get("callId")
            .and_then(JsonValue::as_str)
            .ok_or_else(invalid)?;
        let state = match object.get("state").and_then(JsonValue::as_str) {
            Some("notSent") => State::NotSent,
            Some("claimed") => State::Claimed,
            Some("canResend") => State::CanResend,
            _ => return Err(invalid()),
        };
        let provider_id = match object.get("providerId") {
            None => String::new(),
            Some(JsonValue::String(provider_id)) => provider_id.clone(),
            Some(_) => return Err(invalid()),
        };
        let marker = Marker {
            call_id: CallId::new(call_id),
            attempt: integer("attempt", None)?,
            state,
            provider_id,
            send_count: integer("sendCount", Some(0))?,
        };
        marker.validate().map_err(|_| invalid())?;
        Ok(marker)
    }
}

/// Action permitted before credentials or provider lookups.
#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq)]
pub enum DecisionKind {
    /// Dispatch may send without reading back.
    SendAllowed,
    /// Dispatch is forbidden; read back, or select `uncertain` when unconfirmed.
    ReadBackRequired,
    /// Dispatch is forbidden because the checkpoint could not be trusted.
    FailClosed,
    /// Read back when possible; a definite not-found or inconclusive read-back permits one
    /// Dispatch. Returned only when bounded resend is enabled and the cap is not reached.
    ReadBackThenResendAllowed,
}

impl DecisionKind {
    /// Returns the conformance fixture name, such as `sendAllowed`.
    pub fn as_wire_str(self) -> &'static str {
        match self {
            DecisionKind::SendAllowed => "sendAllowed",
            DecisionKind::ReadBackRequired => "readBackRequired",
            DecisionKind::FailClosed => "failClosed",
            DecisionKind::ReadBackThenResendAllowed => "readBackThenResendAllowed",
        }
    }

    /// Returns whether [`crate::singledispatch::Fence::dispatch`] may send for this decision,
    /// after a read-back that did not confirm the write when bounded resend applies.
    pub fn permits_dispatch(self) -> bool {
        matches!(
            self,
            DecisionKind::SendAllowed | DecisionKind::ReadBackThenResendAllowed
        )
    }
}

/// Resend decision with the prior marker or a safe checkpoint error.
///
/// `prior_send_upper_bound` bounds the possibly effective sends made before this attempt; it
/// is zero for a fail-closed decision.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Decision {
    /// Permitted action.
    pub kind: DecisionKind,
    /// This call's prior checkpoint entry, when present.
    pub marker: Option<Marker>,
    /// Safe error text for a fail-closed decision.
    pub error: String,
    /// Upper bound of earlier possibly effective sends.
    pub prior_send_upper_bound: u32,
}

impl Decision {
    fn new(kind: DecisionKind, marker: Option<Marker>, prior_send_upper_bound: u32) -> Self {
        Self {
            kind,
            marker,
            error: String::new(),
            prior_send_upper_bound,
        }
    }

    pub(crate) fn fail_closed(marker: Option<Marker>, error: &str) -> Self {
        Self {
            kind: DecisionKind::FailClosed,
            marker,
            error: error.to_string(),
            prior_send_upper_bound: 0,
        }
    }
}

/// Rejects a send cap outside 1 to 10.
///
/// # Errors
///
/// Returns [`ValidationError`] for an out-of-range cap.
pub fn validate_max_sends(max_sends: u32) -> Result<(), ValidationError> {
    if !(MINIMUM_MAX_SENDS..=MAXIMUM_MAX_SENDS).contains(&max_sends) {
        return Err(ValidationError::new(
            "single-dispatch max sends must be an integer within 1..10",
        ));
    }
    Ok(())
}

/// Permits a send only when proven safe or while the send cap allows another.
///
/// `previous_heartbeat` is the previous attempt's whole heartbeat value, or `None` when it
/// recorded none. Only this call's entry in the checkpoint map counts; a legacy single marker
/// recorded as the whole heartbeat is this call's entry when its Call ID matches. No marker on
/// attempt one permits the first send, as does a `notSent` or `canResend` entry without a
/// provider ID from the immediately prior attempt. Any other case demands read-back, or
/// permits a resend after read-back while fewer than `max_sends` sends may have happened.
///
/// # Errors
///
/// Returns [`ValidationError`] for an invalid Call ID, a zero attempt, or a cap outside 1 to
/// 10.
pub fn decide_dispatch(
    call_id: &CallId,
    attempt: u32,
    previous_heartbeat: Option<&JsonValue>,
    max_sends: u32,
) -> Result<Decision, ValidationError> {
    call_id.validate()?;
    if attempt < 1 {
        return Err(ValidationError::new(
            "single dispatch requires a positive attempt",
        ));
    }
    validate_max_sends(max_sends)?;
    let marker = previous_heartbeat
        .map(markers_from_heartbeat)
        .and_then(|mut markers| markers.remove(call_id.as_str()));
    let Some(marker) = marker else {
        if attempt == 1 {
            return Ok(Decision::new(DecisionKind::SendAllowed, None, 0));
        }
        return Ok(bounded_read_back(None, attempt - 1, max_sends));
    };
    if !has_possible_send_count(&marker) {
        return Ok(Decision::fail_closed(
            Some(marker),
            "invalid single-dispatch checkpoint",
        ));
    }
    if marker.attempt >= attempt {
        let send_count = marker.send_count;
        return Ok(Decision::new(
            DecisionKind::ReadBackRequired,
            Some(marker),
            send_count,
        ));
    }
    let prior_send_upper_bound = marker.send_count + (attempt - 1 - marker.attempt);
    if marker.attempt == attempt - 1
        && matches!(marker.state, State::NotSent | State::CanResend)
        && marker.provider_id.is_empty()
    {
        return Ok(Decision::new(
            DecisionKind::SendAllowed,
            Some(marker),
            prior_send_upper_bound,
        ));
    }
    Ok(bounded_read_back(
        Some(marker),
        prior_send_upper_bound,
        max_sends,
    ))
}

/// Decodes checkpoint entries, dropping unreadable or mis-keyed ones.
pub(crate) fn markers_from_heartbeat(heartbeat: &JsonValue) -> BTreeMap<String, Marker> {
    let Some(object) = heartbeat.as_object() else {
        return BTreeMap::new();
    };
    let Some(entries) = object.get(CHECKPOINT_MARKERS_FIELD) else {
        return marker_from_entry(heartbeat)
            .map(|marker| BTreeMap::from([(marker.call_id.as_str().to_string(), marker)]))
            .unwrap_or_default();
    };
    let Some(entries) = entries.as_object() else {
        return BTreeMap::new();
    };
    entries
        .iter()
        .filter_map(|(call_id, entry)| {
            marker_from_entry(entry)
                .filter(|marker| marker.call_id.as_str() == call_id)
                .map(|marker| (call_id.clone(), marker))
        })
        .collect()
}

fn marker_from_entry(entry: &JsonValue) -> Option<Marker> {
    let mut marker = Marker::from_json(entry).ok()?;
    if entry.get("sendCount").is_none() {
        marker.send_count = conservative_send_count(&marker);
    }
    Some(marker)
}

/// One checkpoint entry with its exact send count, zero included, in Go's field order.
#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct CheckpointEntry {
    call_id: CallId,
    attempt: u32,
    state: State,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    provider_id: String,
    send_count: u32,
}

/// Whole-heartbeat checkpoint; map keys sort as Go encodes them.
#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub(crate) struct CheckpointHeartbeat {
    #[serde(rename = "singleDispatchMarkers")]
    single_dispatch_markers: BTreeMap<String, CheckpointEntry>,
}

pub(crate) fn checkpoint_heartbeat(markers: &BTreeMap<String, Marker>) -> CheckpointHeartbeat {
    CheckpointHeartbeat {
        single_dispatch_markers: markers
            .iter()
            .map(|(call_id, marker)| {
                (
                    call_id.clone(),
                    CheckpointEntry {
                        call_id: marker.call_id.clone(),
                        attempt: marker.attempt,
                        state: marker.state,
                        provider_id: marker.provider_id.clone(),
                        send_count: marker.send_count,
                    },
                )
            })
            .collect(),
    }
}

fn bounded_read_back(
    marker: Option<Marker>,
    prior_send_upper_bound: u32,
    max_sends: u32,
) -> Decision {
    let kind = if prior_send_upper_bound < max_sends && max_sends > MINIMUM_MAX_SENDS {
        DecisionKind::ReadBackThenResendAllowed
    } else {
        DecisionKind::ReadBackRequired
    };
    Decision::new(kind, marker, prior_send_upper_bound)
}

fn has_possible_send_count(marker: &Marker) -> bool {
    if marker.state == State::Claimed {
        return (1..=marker.attempt).contains(&marker.send_count);
    }
    marker.send_count < marker.attempt
}

fn conservative_send_count(marker: &Marker) -> u32 {
    if marker.state == State::Claimed {
        marker.attempt
    } else {
        marker.attempt - 1
    }
}
