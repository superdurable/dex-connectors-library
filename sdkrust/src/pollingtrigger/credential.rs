// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! The stable credential Call identity of a polling Trigger.

use uuid::Uuid;

use crate::errors::ValidationError;
use crate::trigger::TriggerRef;
use crate::types::{Call, CallId, ConnectionRef};

const TRIGGER_CREDENTIAL_NAMESPACE: &str =
    "https://superdurable.dev/dex-connectors/trigger-credential/v1";

/// Returns the stable Call a poller uses to resolve and refresh connection credentials outside
/// a Step.
///
/// Its ID is the Go SDK's: a UUID v5 in the URL namespace over
/// `https://superdurable.dev/dex-connectors/trigger-credential/v1/<provider>/<connection>/<trigger>`,
/// so every poll of one Trigger on one connection shares an identity across restarts, replicas,
/// and languages. The Call has no Step attempt, so it cannot run a Connector Step.
///
/// # Errors
///
/// Returns [`ValidationError`] for an invalid connection or Trigger reference.
pub fn new_trigger_credential_call(
    connection: &ConnectionRef,
    trigger: &TriggerRef,
) -> Result<Call<'static>, ValidationError> {
    let identity = [
        TRIGGER_CREDENTIAL_NAMESPACE,
        connection.provider.as_str(),
        connection.name.as_str(),
        trigger.trigger_name.as_str(),
    ]
    .join("/");
    Call::new(
        CallId::new(Uuid::new_v5(&Uuid::NAMESPACE_URL, identity.as_bytes()).to_string()),
        connection.clone(),
        trigger.operation(),
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn credential_call_id_matches_go_and_python() {
        let call = new_trigger_credential_call(
            &ConnectionRef::new("google-gmail", "main"),
            &TriggerRef::new("google-gmail", "messageReceived"),
        )
        .expect("valid references");
        assert_eq!(call.id.as_str(), "eeb485f4-5d06-5a1e-b45d-90cdf525cd56");
        assert!(!call.is_in_step());
    }
}
