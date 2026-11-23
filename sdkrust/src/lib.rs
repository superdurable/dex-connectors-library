// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

#![doc = include_str!("../README.md")]

mod configuration_ui;
mod credentials;
mod errors;
mod operations;
mod retry_policy;
mod step_attempt;
mod steps;
mod timestamps;
mod types;

pub mod connectormock;
pub mod go_json;
pub mod oauthtoken;
pub mod pollingtrigger;
pub mod projectconfig;
pub mod providerhttp;
pub mod relaysignature;
pub mod singledispatch;
pub mod trigger;
pub mod webhooktrigger;

pub use configuration_ui::{
    ConnectorConfigurationRef, ConnectorConfigurationUi, ConnectorUiBinding, ConnectorUiUnit,
};
pub use credentials::{
    CredentialError, CredentialProvider, CredentialRefreshDriver, CredentialRefreshResult,
    CredentialRefreshState, ReauthorizationRequiredError, SecretString, StaticCredentialProvider,
    is_reauthorization_required, resolve_credential, resolve_credential_after_rejection,
};
pub use errors::{Failure, FailureKind, RetryError, ValidationError};
pub use operations::{Mutation, OperationOutcome, Query, run_mutation, run_query};
pub use retry_policy::{RetryPolicy, default_connector_step_execute_retry_policy};
pub use step_attempt::StepAttempt;
pub use steps::{
    BranchTarget, MutationStep, MutationStepConfig, OperationInputMapper, QueryStep,
    QueryStepConfig, StepAnnotations, StepOptionsOverride, dex_retry_error,
};
pub use timestamps::{Rfc3339NanoTimestamp, ZERO_TIMESTAMP_WIRE, format_rfc3339_nano};
pub use types::{
    BranchDefinition, Call, CallId, CallIdentity, ConnectionRef, DEFECT_BRANCH_ID,
    ExecuteDurability, IdempotencyKey, MutationAttempt, MutationDefinition, MutationResult,
    OperationRef, Progress, ProgressUpdate, QueryAttempt, QueryDefinition, QueryResult, Receipt,
    RepeatOnUnknown, StepDefaults, UNCERTAIN_BRANCH_ID, call_id_namespace, derive_call_id,
};

/// Re-export of the Dex Rust SDK this crate is built against.
pub use dex_sdk;
