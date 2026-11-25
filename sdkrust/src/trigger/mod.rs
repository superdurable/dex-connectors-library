// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Trigger identities, runners, delivery, and the Dex Flow Trigger target.
//!
//! A Trigger turns provider events into Dex Flow starts. A [`TriggerSource`] owns the provider
//! transport, persists each event through [`prepare_trigger_delivery`] before acknowledging
//! it, and hands acknowledged events to a [`TriggerTarget`] with [`deliver_trigger`].
//! [`new_trigger`] runs a source against a target after replaying a durable target.
//!
//! Dex RPC Trigger targets are not provided: Dex Rust SDK 1.45.7 `RpcInvokeOptions` carries no
//! request ID, so an RPC could not be applied at most once per event.

mod delivery;
mod flow_target;

use std::sync::Arc;

use base64::Engine;
use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use sha2::{Digest, Sha256};

use crate::configuration_ui::ConnectorConfigurationUi;
use crate::errors::ValidationError;
use crate::types::{ConnectionRef, OperationRef};

pub(crate) use delivery::event_attributes;
pub use delivery::{
    TracingTriggerLogger, TriggerAttempt, TriggerCancellation, TriggerContext,
    TriggerDeliveryAbortedError, TriggerError, TriggerEvent, TriggerLogAttributes, TriggerLogLevel,
    TriggerLogValue, TriggerLogger, TriggerTarget, UNDELIVERABLE_TRIGGER_ERROR_TYPE,
    UNDELIVERABLE_TRIGGER_MESSAGE, UndeliverableTriggerError, bind_trigger_log_attributes,
    deliver_trigger, has_durable_trigger_inbox, is_trigger_delivery_aborted,
    is_trigger_undeliverable, log_trigger_record, mark_trigger_undeliverable,
    prepare_trigger_delivery, start_trigger_attempt, trigger_error_attributes, trigger_retry_delay,
    wait_for_trigger_delay,
};
pub use flow_target::{
    DexFlowTriggerTargetConfig, FlowIdResolver, FlowInputMapper, TriggerFilter,
    classify_dex_trigger_error, new_dex_flow_trigger_target,
};

/// Prefix of every request ID [`trigger_rpc_request_id`] derives.
pub const TRIGGER_RPC_REQUEST_ID_PREFIX: &str = "trigger-rpc-";

/// Stable manifest identity of one connector Trigger.
#[derive(Clone, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
pub struct TriggerRef {
    /// DNS-like manifest connector ID.
    pub connector_id: String,
    /// Lower camel case manifest Trigger name.
    pub trigger_name: String,
}

impl TriggerRef {
    /// Creates a Trigger reference.
    pub fn new(connector_id: impl Into<String>, trigger_name: impl Into<String>) -> Self {
        Self {
            connector_id: connector_id.into(),
            trigger_name: trigger_name.into(),
        }
    }

    /// Requires a DNS-like connector ID and a lower camel case Trigger name.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for either invalid identity.
    pub fn validate(&self) -> Result<(), ValidationError> {
        OperationRef::new(self.connector_id.clone(), self.trigger_name.clone())
            .validate()
            .map_err(|error| {
                if error.message().starts_with("operation ID") {
                    ValidationError::new("trigger name must be lower camel case")
                } else {
                    error
                }
            })
    }

    /// Returns the operation reference whose operation ID is the Trigger name.
    pub fn operation(&self) -> OperationRef {
        OperationRef::new(self.connector_id.clone(), self.trigger_name.clone())
    }
}

/// One provider event source declared by a connector manifest.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct TriggerDefinition {
    /// Trigger identity.
    pub trigger: TriggerRef,
    /// Application-facing description; must contain a non-blank character.
    pub description: String,
}

impl TriggerDefinition {
    /// Validates the identity and requires a description.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid identity or blank description.
    pub fn validate(&self) -> Result<(), ValidationError> {
        self.trigger.validate()?;
        if self.description.trim().is_empty() {
            return Err(ValidationError::new("trigger description is required"));
        }
        Ok(())
    }
}

/// One configured use of a connector Trigger: its connection, Trigger, and binding name.
#[derive(Clone, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
pub struct TriggerBindingRef {
    /// Provider connection the Trigger reads.
    pub connection: ConnectionRef,
    /// Trigger identity.
    pub trigger: TriggerRef,
    /// Stable application binding name.
    pub name: String,
}

impl TriggerBindingRef {
    /// Validates every identity part.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid connection, Trigger, or blank name.
    pub fn validate(&self) -> Result<(), ValidationError> {
        self.connection.validate()?;
        self.trigger.validate()?;
        if self.name.trim().is_empty() {
            return Err(ValidationError::new("trigger binding name is required"));
        }
        Ok(())
    }
}

/// One static application binding exposed to Flow Definition tooling and Dex Web.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct TriggerBindingDefinition {
    /// Immutable Trigger definition.
    pub definition: TriggerDefinition,
    /// Configured provider connection name.
    pub connection_name: String,
    /// Configured Trigger binding name.
    pub binding_name: String,
    /// Optional configuration units.
    pub configuration_ui: Option<ConnectorConfigurationUi>,
}

impl TriggerBindingDefinition {
    /// Validates the definition, names, and configuration units.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for any invalid part.
    pub fn validate(&self) -> Result<(), ValidationError> {
        self.definition.validate()?;
        if self.connection_name.trim().is_empty() {
            return Err(ValidationError::new(
                "trigger binding connection name is required",
            ));
        }
        if self.binding_name.trim().is_empty() {
            return Err(ValidationError::new("trigger binding name is required"));
        }
        if let Some(configuration_ui) = &self.configuration_ui {
            configuration_ui.validate()?;
        }
        Ok(())
    }
}

/// Validates static binding metadata and returns it.
///
/// # Panics
///
/// Panics on invalid static wiring, which is a startup defect.
#[expect(
    clippy::panic,
    reason = "static binding metadata is validated once at startup, like sdkgo's Must helpers"
)]
pub fn must_trigger_binding_definition(
    definition: TriggerBindingDefinition,
) -> TriggerBindingDefinition {
    if let Err(error) = definition.validate() {
        panic!("invalid Trigger binding definition: {error}");
    }
    definition
}

/// Owns the provider transport and emits acknowledged events.
///
/// `run` calls [`prepare_trigger_delivery`] before acknowledging an event, retries a
/// retryable error, and treats an undeliverable marker as consumed. It returns only when the
/// context is cancelled or it cannot recover.
pub trait TriggerSource<Payload>: Send + Sync {
    /// Processes provider events until the context is cancelled or an unrecoverable error.
    ///
    /// # Errors
    ///
    /// Returns [`TriggerDeliveryAbortedError`] after cancellation, or the unrecoverable error.
    fn run(
        &self,
        context: &TriggerContext,
        target: Arc<dyn TriggerTarget<Payload>>,
    ) -> Result<(), TriggerError>;
}

/// A configured long-running connector Trigger.
pub trait TriggerRunner: Send + Sync {
    /// Immutable connector Trigger definition.
    fn definition(&self) -> &TriggerDefinition;

    /// Configured Trigger binding identity.
    fn binding(&self) -> &TriggerBindingRef;

    /// Processes provider events until the context is cancelled or an unrecoverable error.
    ///
    /// # Errors
    ///
    /// Returns a replay failure prefixed `replay Trigger deliveries:`, an abort after
    /// cancellation, or the source's unrecoverable error.
    fn run(&self, context: &TriggerContext) -> Result<(), TriggerError>;
}

/// One provider source and application target.
pub struct TriggerConfig<Payload> {
    /// Immutable Trigger definition.
    pub definition: TriggerDefinition,
    /// Configured binding identity; its Trigger must equal the definition's.
    pub binding: TriggerBindingRef,
    /// Polls or subscribes to provider events.
    pub source: Arc<dyn TriggerSource<Payload>>,
    /// Routes accepted events into the application.
    pub target: Arc<dyn TriggerTarget<Payload>>,
    /// Receives the runner's records; `None` uses [`TracingTriggerLogger`].
    pub logger: Option<Arc<dyn TriggerLogger>>,
}

struct UndeliverableConsumingTarget<Payload> {
    target: Arc<dyn TriggerTarget<Payload>>,
    logger: Arc<dyn TriggerLogger>,
}

impl<Payload> TriggerTarget<Payload> for UndeliverableConsumingTarget<Payload> {
    fn handle_trigger(
        &self,
        context: &TriggerContext,
        event: &TriggerEvent<Payload>,
    ) -> Result<(), TriggerError> {
        match self.target.handle_trigger(context, event) {
            Err(error) if is_trigger_undeliverable(error.as_ref()) => {
                let mut attributes = event_attributes(&event.id);
                attributes.extend(trigger_error_attributes(error.as_ref()));
                log_trigger_record(
                    Some(self.logger.as_ref()),
                    context,
                    TriggerLogLevel::Warn,
                    "trigger event skipped: undeliverable",
                    attributes,
                );
                context.record_skip();
                Ok(())
            }
            other => other,
        }
    }

    fn prepares_trigger_delivery(&self) -> bool {
        self.target.prepares_trigger_delivery()
    }

    fn prepare_trigger(
        &self,
        context: &TriggerContext,
        event: &TriggerEvent<Payload>,
    ) -> Result<(), TriggerError> {
        self.target.prepare_trigger(context, event)
    }

    fn replays_trigger_deliveries(&self) -> bool {
        self.target.replays_trigger_deliveries()
    }

    fn replay_trigger_deliveries(&self, context: &TriggerContext) -> Result<(), TriggerError> {
        self.target.replay_trigger_deliveries(context)
    }
}

struct ConfiguredTriggerRunner<Payload> {
    definition: TriggerDefinition,
    binding: TriggerBindingRef,
    source: Arc<dyn TriggerSource<Payload>>,
    target: Arc<UndeliverableConsumingTarget<Payload>>,
}

/// Replay failure of a Trigger runner.
#[derive(Debug)]
pub struct TriggerReplayError {
    cause: TriggerError,
}

impl std::fmt::Display for TriggerReplayError {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(formatter, "replay Trigger deliveries: {}", self.cause)
    }
}

impl std::error::Error for TriggerReplayError {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        Some(self.cause.as_ref())
    }
}

impl<Payload: Send + Sync + 'static> TriggerRunner for ConfiguredTriggerRunner<Payload> {
    fn definition(&self) -> &TriggerDefinition {
        &self.definition
    }

    fn binding(&self) -> &TriggerBindingRef {
        &self.binding
    }

    fn run(&self, context: &TriggerContext) -> Result<(), TriggerError> {
        if self.target.replays_trigger_deliveries() {
            self.target
                .replay_trigger_deliveries(context)
                .map_err(|cause| Box::new(TriggerReplayError { cause }) as TriggerError)?;
        }
        let target: Arc<dyn TriggerTarget<Payload>> = self.target.clone();
        self.source.run(context, target)
    }
}

/// Validates and constructs one Trigger runner.
///
/// `run` replays a replaying target first, then runs the source with a target that consumes
/// undeliverable results, logging each with the binding identity, and forwards
/// `prepare_trigger`.
///
/// # Errors
///
/// Returns [`ValidationError`] when an identity is invalid or the binding does not match the
/// definition.
pub fn new_trigger<Payload: Send + Sync + 'static>(
    config: TriggerConfig<Payload>,
) -> Result<Arc<dyn TriggerRunner>, ValidationError> {
    config.definition.validate()?;
    config.binding.validate()?;
    if config.binding.trigger != config.definition.trigger {
        return Err(ValidationError::new(
            "trigger binding does not match its definition",
        ));
    }
    let binding = config.binding;
    let logger = bind_trigger_log_attributes(
        config.logger,
        TriggerLogAttributes::from([
            (
                "connector".to_string(),
                TriggerLogValue::from(binding.trigger.connector_id.as_str()),
            ),
            (
                "connection".to_string(),
                TriggerLogValue::from(binding.connection.name.as_str()),
            ),
            (
                "trigger".to_string(),
                TriggerLogValue::from(binding.trigger.trigger_name.as_str()),
            ),
            (
                "binding".to_string(),
                TriggerLogValue::from(binding.name.as_str()),
            ),
        ]),
    );
    Ok(Arc::new(ConfiguredTriggerRunner {
        definition: config.definition,
        binding,
        source: config.source,
        target: Arc::new(UndeliverableConsumingTarget {
            target: config.target,
            logger,
        }),
    }))
}

/// Derives the Dex request ID a Trigger RPC target sends on every attempt for one event.
///
/// The ID is [`TRIGGER_RPC_REQUEST_ID_PREFIX`] followed by the first 16 bytes, as unpadded
/// base64url, of the SHA-256 digest of the UTF-8 `rpc_name`, one `0x00` byte, and the UTF-8
/// `event_id`. It matches sdkgo `TriggerRPCRequestID`, sdkts `triggerRPCRequestId`, and Python
/// `trigger_rpc_request_id` byte for byte; for example `receiveReply` and `Ev1` yield
/// `trigger-rpc-kDFFz9I4ELDtzXkmvRTzlA`. Rust strings are always well-formed UTF-8.
///
/// This SDK provides no RPC Trigger target, because Dex Rust SDK 1.45.7 cannot send a request
/// ID with an RPC; the derivation is exported for cross-language tooling.
///
/// # Errors
///
/// Returns [`ValidationError`] when `rpc_name` is empty or contains a NUL character.
pub fn trigger_rpc_request_id(rpc_name: &str, event_id: &str) -> Result<String, ValidationError> {
    if rpc_name.is_empty() || rpc_name.contains('\0') {
        return Err(ValidationError::new(
            "RPC request ID requires an RPC name without NUL characters",
        ));
    }
    let mut digest = Sha256::new();
    digest.update(rpc_name.as_bytes());
    digest.update([0]);
    digest.update(event_id.as_bytes());
    let digest = digest.finalize();
    Ok(format!(
        "{TRIGGER_RPC_REQUEST_ID_PREFIX}{}",
        URL_SAFE_NO_PAD.encode(&digest[..16])
    ))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn rpc_request_id_matches_go_python_and_typescript() {
        assert_eq!(
            trigger_rpc_request_id("receiveReply", "Ev1").expect("valid name"),
            "trigger-rpc-kDFFz9I4ELDtzXkmvRTzlA"
        );
        assert!(trigger_rpc_request_id("", "Ev1").is_err());
        assert!(trigger_rpc_request_id("a\0b", "Ev1").is_err());
    }

    #[test]
    fn trigger_identities_validate() {
        assert!(
            TriggerRef::new("slack", "messageReceived")
                .validate()
                .is_ok()
        );
        assert_eq!(
            TriggerRef::new("slack", "Bad")
                .validate()
                .expect_err("invalid name")
                .message(),
            "trigger name must be lower camel case"
        );
        let definition = TriggerDefinition {
            trigger: TriggerRef::new("slack", "messageReceived"),
            description: " ".to_string(),
        };
        assert!(definition.validate().is_err());
    }
}
