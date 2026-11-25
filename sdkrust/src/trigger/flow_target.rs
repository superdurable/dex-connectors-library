// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! The Dex Flow-start Trigger target and Dex error classification.

use std::sync::Arc;

use dex_sdk::{Client, Flow, IdReusePolicy, SdkError, StartFlowOptions};

use super::delivery::{
    TriggerContext, TriggerError, TriggerEvent, TriggerLogAttributes, TriggerLogLevel,
    TriggerLogValue, TriggerLogger, TriggerTarget, UNDELIVERABLE_TRIGGER_ERROR_TYPE,
    UNDELIVERABLE_TRIGGER_MESSAGE, UndeliverableTriggerError, bind_trigger_log_attributes,
    event_attributes, log_trigger_record, mark_trigger_undeliverable,
};

/// Decides whether an application accepts an event; `false` consumes it without routing.
///
/// It must be deterministic and side-effect free, because durable delivery may evaluate it again
/// after a restart.
pub type TriggerFilter<Payload> = Box<dyn Fn(&TriggerEvent<Payload>) -> bool + Send + Sync>;

/// Maps one provider event to the stable application Flow ID that owns it.
pub type FlowIdResolver<Payload> = Box<dyn Fn(&TriggerEvent<Payload>) -> String + Send + Sync>;

/// Maps one provider event to a Flow's start input; an error is retried.
pub type FlowInputMapper<Payload, Input> =
    Box<dyn Fn(&TriggerEvent<Payload>) -> Result<Input, TriggerError> + Send + Sync>;

/// Configuration of a filtered target that starts one Dex Flow per event.
pub struct DexFlowTriggerTargetConfig<Payload, SomeFlow: Flow> {
    /// Dex Client whose Registry holds `flow`.
    pub client: Arc<Client>,
    /// A value of the registered Flow type; used only for its type and Flow type name.
    pub flow: SomeFlow,
    /// Selects accepted events.
    pub accepts_event: TriggerFilter<Payload>,
    /// Resolves the owning Flow ID.
    pub resolve_flow_id: FlowIdResolver<Payload>,
    /// Maps the event to the Flow start input.
    pub map_to_flow_input: FlowInputMapper<Payload, SomeFlow::StartInput>,
    /// Receives records; `None` uses the default tracing logger.
    pub logger: Option<Arc<dyn TriggerLogger>>,
}

struct DexFlowTriggerTarget<Payload, SomeFlow: Flow> {
    config: DexFlowTriggerTargetConfig<Payload, SomeFlow>,
    logger: Arc<dyn TriggerLogger>,
}

/// Creates a filtered target that starts a Dex Flow.
///
/// The event ID is the Flow-start request ID and Flow ID reuse is disallowed, so a duplicate
/// start is delivered even after the original Flow completes. A blank Flow ID or event ID, an
/// input that cannot be encoded, or a missing Flow returns an undeliverable marker. Mapper errors
/// and every other error propagate for a retry. It logs INFO "trigger event skipped: filtered"
/// and DEBUG "trigger event delivered" with `duplicate`.
pub fn new_dex_flow_trigger_target<Payload, SomeFlow>(
    config: DexFlowTriggerTargetConfig<Payload, SomeFlow>,
) -> Arc<dyn TriggerTarget<Payload>>
where
    Payload: Send + Sync + 'static,
    SomeFlow: Flow,
{
    let logger = bind_trigger_log_attributes(
        config.logger.clone(),
        TriggerLogAttributes::from([("target".to_string(), TriggerLogValue::from("flow_start"))]),
    );
    Arc::new(DexFlowTriggerTarget { config, logger })
}

impl<Payload, SomeFlow> TriggerTarget<Payload> for DexFlowTriggerTarget<Payload, SomeFlow>
where
    Payload: Send + Sync,
    SomeFlow: Flow,
{
    fn handle_trigger(
        &self,
        context: &TriggerContext,
        event: &TriggerEvent<Payload>,
    ) -> Result<(), TriggerError> {
        if !(self.config.accepts_event)(event) {
            log_trigger_record(
                Some(self.logger.as_ref()),
                context,
                TriggerLogLevel::Info,
                "trigger event skipped: filtered",
                event_attributes(&event.id),
            );
            return Ok(());
        }
        let flow_id = (self.config.resolve_flow_id)(event);
        if flow_id.trim().is_empty() || event.id.trim().is_empty() {
            return Err(Box::new(UndeliverableTriggerError::with_reason(
                "Flow trigger requires Flow ID and event ID",
            )));
        }
        let flow_input = (self.config.map_to_flow_input)(event)?;
        let options = StartFlowOptions::new()
            .id_reuse_policy(IdReusePolicy::Disallow)
            .request_id(event.id.clone());
        let is_duplicate = match self.config.client.start_flow_with_options(
            &self.config.flow,
            &flow_id,
            flow_input,
            options,
        ) {
            Ok(_) => false,
            Err(SdkError::FlowAlreadyStarted { .. }) => true,
            Err(error) => return Err(classify_dex_trigger_error(error)),
        };
        let mut attributes = event_attributes(&event.id);
        attributes.insert("flow_id".to_string(), TriggerLogValue::Text(flow_id));
        attributes.insert("duplicate".to_string(), TriggerLogValue::Flag(is_duplicate));
        log_trigger_record(
            Some(self.logger.as_ref()),
            context,
            TriggerLogLevel::Debug,
            "trigger event delivered",
            attributes,
        );
        Ok(())
    }
}

/// Marks the Dex errors no retry of the same event can fix; returns every other error unchanged.
///
/// A closed or never-started Flow, an RPC handler's own undeliverable marker (reported with type
/// [`UNDELIVERABLE_TRIGGER_ERROR_TYPE`] and the marker message), and a value that cannot be
/// mapped are marked. A Worker code alone never qualifies, so lookalike errors stay retryable.
/// The Flow-start target maps only an input, so a value mapping error is an encoding failure.
pub fn classify_dex_trigger_error(error: SdkError) -> TriggerError {
    let is_undeliverable = match &error {
        SdkError::FlowNotActiveOrNotFound { .. }
        | SdkError::FlowNotFound { .. }
        | SdkError::ValueMapping { .. } => true,
        SdkError::WorkerInvocation { worker, .. } => {
            worker.error_type() == UNDELIVERABLE_TRIGGER_ERROR_TYPE
                && (worker.detail() == UNDELIVERABLE_TRIGGER_MESSAGE
                    || worker
                        .detail()
                        .starts_with(&format!("{UNDELIVERABLE_TRIGGER_MESSAGE}: ")))
        }
        _ => false,
    };
    let error: TriggerError = Box::new(error);
    if is_undeliverable {
        mark_trigger_undeliverable(error)
    } else {
        error
    }
}
