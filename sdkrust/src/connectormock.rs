// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Scriptable provider outcomes and strict JSON fixtures for connector tests.
//!
//! [`Query`] and [`Mutation`] implement the real operation contracts from a connector's
//! actual definition, so a Flow test runs the generated Step factories against scripted
//! provider outcomes. Script precedence matches sdkgo: the longest matching Flow ID prefix
//! script first, then the operation script; rules before queued cases; defaults last unless
//! the mock is strict. A Mutation replays its first terminal outcome for an idempotency key.
//!
//! Each answered call is recorded and, when `DEX_CONNECTOR_MOCK_REPORT_DIR` is set, appended
//! to `connector-calls.json` in that directory as Go-compatible bytes.
//!
//! ```
//! use dex_connectors_sdk::connectormock::{Case, ScriptedOperation};
//! use dex_connectors_sdk::{ConnectionRef, OperationRef};
//!
//! let mock = ScriptedOperation::<String, String>::new(
//!     OperationRef::new("sample", "getItem"),
//!     &["found", "defect"],
//!     false,
//! )?;
//! mock.respond(vec![Case::branch("found", "item-1".to_string(), None)])?;
//! let answer = mock.answer(
//!     "flow-1",
//!     "call-1",
//!     &ConnectionRef::new("sample", "main"),
//!     &"input".to_string(),
//!     "",
//!     1,
//! );
//! assert_eq!(answer, Case::branch("found", "item-1".to_string(), None));
//! assert_eq!(mock.calls()[0].branch, "found");
//! # Ok::<(), dex_connectors_sdk::ValidationError>(())
//! ```

use std::collections::{BTreeMap, BTreeSet};
use std::fmt;
use std::fs::{self, OpenOptions};
use std::io::{self, Read as _, Seek as _, SeekFrom, Write as _};
use std::path::{Path, PathBuf};
use std::sync::{Mutex, MutexGuard, PoisonError};
use std::time::{Duration, Instant, SystemTime};

use serde::Serialize;
use serde::de::{Deserializer, MapAccess, Visitor};
use serde_json::{Map as JsonMap, Value as JsonValue};

use crate::errors::{Failure, FailureKind, ValidationError};
use crate::go_json::to_go_json_vec;
use crate::timestamps::Rfc3339NanoTimestamp;
use crate::types::{
    Call, CallId, ConnectionRef, DEFECT_BRANCH_ID, IdempotencyKey, MutationAttempt,
    MutationDefinition, OperationRef, QueryAttempt, QueryDefinition, Receipt, UNCERTAIN_BRANCH_ID,
};

/// Environment variable naming the directory of the connector call report.
pub const REPORT_DIRECTORY_ENVIRONMENT: &str = "DEX_CONNECTOR_MOCK_REPORT_DIR";
/// File name of the connector call report inside the report directory.
pub const REPORT_FILE_NAME: &str = "connector-calls.json";

const REPORT_LOCK_TIMEOUT: Duration = Duration::from_secs(30);
const STALE_REPORT_LOCK_AGE: Duration = Duration::from_secs(10);
static REPORT_PROCESS_LOCK: Mutex<()> = Mutex::new(());

/// One scripted branch, retry, uncertain outcome, or listing page.
#[derive(Clone, Debug, PartialEq)]
pub enum Case<Output> {
    /// A definite declared branch.
    Branch {
        /// Declared branch ID.
        branch: String,
        /// Operation value.
        value: Output,
        /// Optional safe failure context.
        failure: Option<Failure>,
    },
    /// Another Step attempt after an optional provider delay.
    Retry {
        /// Safe failure context.
        failure: Failure,
        /// Nonnegative delay hint in seconds.
        retry_after_seconds: f64,
    },
    /// A nonrepeatable, unconfirmable, possibly accepted Mutation.
    Uncertain {
        /// Operation value.
        value: Output,
        /// Safe failure context.
        failure: Failure,
    },
    /// Successive pages of a listing, selected by one-based page number.
    Paged {
        /// Declared branch ID of every page.
        branch: String,
        /// Pages in order; at least one.
        pages: Vec<Output>,
    },
}

impl<Output> Case<Output> {
    /// Answers a definite declared branch.
    pub fn branch(branch: impl Into<String>, value: Output, failure: Option<Failure>) -> Self {
        Case::Branch {
            branch: branch.into(),
            value,
            failure,
        }
    }

    /// Requests another Step attempt after an optional delay in seconds.
    pub fn retry(failure: Failure, retry_after_seconds: f64) -> Self {
        Case::Retry {
            failure,
            retry_after_seconds,
        }
    }

    /// Answers a nonrepeatable, unconfirmable, possibly accepted Mutation.
    pub fn uncertain(value: Output, failure: Failure) -> Self {
        Case::Uncertain { value, failure }
    }

    /// Answers successive pages of a listing.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] when `pages` is empty.
    pub fn paged(branch: impl Into<String>, pages: Vec<Output>) -> Result<Self, ValidationError> {
        if pages.is_empty() {
            return Err(ValidationError::new(
                "paged mock requires at least one page",
            ));
        }
        Ok(Case::Paged {
            branch: branch.into(),
            pages,
        })
    }

    fn is_retry(&self) -> bool {
        matches!(self, Case::Retry { .. })
    }

    fn branch_id(&self) -> &str {
        match self {
            Case::Branch { branch, .. } | Case::Paged { branch, .. } => branch,
            Case::Uncertain { .. } => UNCERTAIN_BRANCH_ID,
            Case::Retry { .. } => "",
        }
    }
}

/// One answered mock call, including replay and retry classification.
#[derive(Clone, Debug, PartialEq)]
pub struct RecordedCall<Input> {
    /// Connector ID.
    pub connector_id: String,
    /// Operation ID.
    pub operation_id: String,
    /// Logical connection name.
    pub connection_name: String,
    /// Dex Flow ID.
    pub flow_id: String,
    /// Connector Call ID.
    pub call_id: String,
    /// Operation input; never written to the report.
    pub input: Input,
    /// Selected branch; empty for a retry.
    pub branch: String,
    /// Whether the call requested a retry.
    pub is_retry: bool,
    /// Whether the call replayed an earlier terminal keyed outcome.
    pub is_replay: bool,
    /// Whether no script answered the call.
    pub is_unscripted: bool,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct RecordedCallWire<'call> {
    connector_id: &'call str,
    operation_id: &'call str,
    connection_name: &'call str,
    flow_id: &'call str,
    call_id: &'call str,
    #[serde(skip_serializing_if = "str::is_empty")]
    branch: &'call str,
    #[serde(skip_serializing_if = "is_false")]
    retry: bool,
    #[serde(skip_serializing_if = "is_false")]
    replay: bool,
    #[serde(skip_serializing_if = "is_false")]
    unscripted: bool,
}

/// Go connector report row; field order matches sdkgo's report struct.
#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct ReportRowWire<'call> {
    connection: &'call str,
    operation: &'call str,
    #[serde(skip_serializing_if = "str::is_empty")]
    branch: &'call str,
    connector_id: &'call str,
    #[serde(skip_serializing_if = "str::is_empty")]
    flow_id: &'call str,
    #[serde(skip_serializing_if = "str::is_empty")]
    call_id: &'call str,
    #[serde(skip_serializing_if = "is_false")]
    retry: bool,
    #[serde(skip_serializing_if = "is_false")]
    replay: bool,
    #[serde(skip_serializing_if = "is_false")]
    unscripted: bool,
}

fn is_false(value: &bool) -> bool {
    !*value
}

impl<Input> RecordedCall<Input> {
    /// Encodes the process-level Go `RecordedCall` JSON shape.
    pub fn to_wire(&self) -> JsonValue {
        serde_json::to_value(RecordedCallWire {
            connector_id: &self.connector_id,
            operation_id: &self.operation_id,
            connection_name: &self.connection_name,
            flow_id: &self.flow_id,
            call_id: &self.call_id,
            branch: &self.branch,
            retry: self.is_retry,
            replay: self.is_replay,
            unscripted: self.is_unscripted,
        })
        .unwrap_or(JsonValue::Null)
    }

    /// Encodes the Go connector report row without private input values.
    pub fn to_report_wire(&self) -> JsonValue {
        serde_json::to_value(self.report_row()).unwrap_or(JsonValue::Null)
    }

    fn report_row(&self) -> ReportRowWire<'_> {
        ReportRowWire {
            connection: &self.connection_name,
            operation: &self.operation_id,
            branch: &self.branch,
            connector_id: &self.connector_id,
            flow_id: &self.flow_id,
            call_id: &self.call_id,
            retry: self.is_retry,
            replay: self.is_replay,
            unscripted: self.is_unscripted,
        }
    }
}

/// Connector call report failure.
#[derive(Debug)]
pub enum ReportError {
    /// The report or its lock file could not be read or written.
    Io(io::Error),
    /// The existing report does not end with `]}`.
    MalformedReport,
}

impl fmt::Display for ReportError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            ReportError::Io(error) => write!(formatter, "connector call report: {error}"),
            ReportError::MalformedReport => {
                formatter.write_str("connector call report has no closing bracket")
            }
        }
    }
}

impl std::error::Error for ReportError {}

impl From<io::Error> for ReportError {
    fn from(error: io::Error) -> Self {
        ReportError::Io(error)
    }
}

fn append_to_report<Input>(call: &RecordedCall<Input>) -> Result<(), ReportError> {
    let Some(directory) = std::env::var_os(REPORT_DIRECTORY_ENVIRONMENT) else {
        return Ok(());
    };
    if directory.is_empty() {
        return Ok(());
    }
    append_to_report_in(Path::new(&directory), call)
}

/// Appends one row to `<directory>/connector-calls.json` under a cross-process lock file.
fn append_to_report_in<Input>(
    directory: &Path,
    call: &RecordedCall<Input>,
) -> Result<(), ReportError> {
    let report_path = directory.join(REPORT_FILE_NAME);
    let lock_path = PathBuf::from(format!("{}.lock", report_path.display()));
    let encoded = to_go_json_vec(&call.report_row())
        .map_err(|error| ReportError::Io(io::Error::other(error)))?;
    let _process_guard = REPORT_PROCESS_LOCK
        .lock()
        .unwrap_or_else(PoisonError::into_inner);
    acquire_report_lock(&lock_path)?;
    let written = write_report_row(&report_path, &encoded);
    if let Err(error) = fs::remove_file(&lock_path)
        && error.kind() != io::ErrorKind::NotFound
        && written.is_ok()
    {
        return Err(error.into());
    }
    written
}

fn acquire_report_lock(lock_path: &Path) -> Result<(), ReportError> {
    let deadline = Instant::now() + REPORT_LOCK_TIMEOUT;
    loop {
        match OpenOptions::new()
            .write(true)
            .create_new(true)
            .open(lock_path)
        {
            Ok(_) => return Ok(()),
            Err(error) if error.kind() == io::ErrorKind::AlreadyExists => {}
            Err(error) => return Err(error.into()),
        }
        let modified = fs::metadata(lock_path).and_then(|metadata| metadata.modified());
        match modified {
            Ok(modified)
                if SystemTime::now()
                    .duration_since(modified)
                    .is_ok_and(|age| age > STALE_REPORT_LOCK_AGE) =>
            {
                // A crashed writer left the lock; removal races are harmless retries.
                if let Err(error) = fs::remove_file(lock_path)
                    && error.kind() != io::ErrorKind::NotFound
                {
                    return Err(error.into());
                }
                continue;
            }
            Err(error) if error.kind() == io::ErrorKind::NotFound => continue,
            _ => {}
        }
        if Instant::now() >= deadline {
            return Err(ReportError::Io(io::Error::other(
                "connector call report lock stayed held",
            )));
        }
        std::thread::sleep(Duration::from_millis(5));
    }
}

fn write_report_row(report_path: &Path, encoded: &[u8]) -> Result<(), ReportError> {
    let mut file = OpenOptions::new()
        .read(true)
        .write(true)
        .create(true)
        .truncate(false)
        .open(report_path)?;
    let size = file.seek(SeekFrom::End(0))?;
    let mut entry = Vec::with_capacity(encoded.len() + 12);
    if size == 0 {
        entry.extend_from_slice(b"{\"calls\":[");
    } else {
        if size < 2 {
            return Err(ReportError::MalformedReport);
        }
        file.seek(SeekFrom::Start(size - 2))?;
        let mut closing = [0_u8; 2];
        file.read_exact(&mut closing)?;
        if &closing != b"]}" {
            return Err(ReportError::MalformedReport);
        }
        file.seek(SeekFrom::Start(size - 2))?;
        entry.push(b',');
    }
    entry.extend_from_slice(encoded);
    entry.extend_from_slice(b"]}");
    file.write_all(&entry)?;
    let end = file.stream_position()?;
    file.set_len(end)?;
    Ok(())
}

/// Input predicate of a reusable `when` rule.
pub type InputPredicate<Input> = Box<dyn Fn(&Input) -> bool + Send + Sync>;

struct Script<Input, Output> {
    rules: Vec<(InputPredicate<Input>, Case<Output>)>,
    queue: Vec<Case<Output>>,
    defaults: Vec<Case<Output>>,
    default_count: usize,
}

impl<Input, Output: Clone> Script<Input, Output> {
    fn new() -> Self {
        Self {
            rules: Vec::new(),
            queue: Vec::new(),
            defaults: Vec::new(),
            default_count: 0,
        }
    }

    fn answer(&mut self, input: &Input) -> Option<Case<Output>> {
        for (predicate, response) in &self.rules {
            if predicate(input) {
                return Some(response.clone());
            }
        }
        if self.queue.is_empty() {
            None
        } else {
            Some(self.queue.remove(0))
        }
    }

    fn default_case(&mut self) -> Option<Case<Output>> {
        let last = self.defaults.len().checked_sub(1)?;
        let result = self.defaults[self.default_count.min(last)].clone();
        self.default_count += 1;
        Some(result)
    }
}

struct ScriptedState<Input, Output> {
    script: Script<Input, Output>,
    flow_scripts: BTreeMap<String, Script<Input, Output>>,
    is_strict: bool,
    terminal_by_key: BTreeMap<String, Case<Output>>,
    calls: Vec<RecordedCall<Input>>,
    failures: Vec<String>,
}

/// Failure callback invoked for every unscripted call or report error.
pub type FailureReporter = Box<dyn Fn(&str) + Send + Sync>;

/// Thread-safe Query or Mutation test model without Dex Step registration.
pub struct ScriptedOperation<Input, Output> {
    operation: OperationRef,
    branches: BTreeSet<String>,
    is_mutation: bool,
    report_failure: Option<FailureReporter>,
    state: Mutex<ScriptedState<Input, Output>>,
}

impl<Input: Clone, Output: Clone + Default> ScriptedOperation<Input, Output> {
    /// Creates a model of `operation` with its declared branch IDs.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid operation or a branch list without `defect`.
    pub fn new(
        operation: OperationRef,
        branches: &[&str],
        is_mutation: bool,
    ) -> Result<Self, ValidationError> {
        Self::with_failure_reporter(operation, branches, is_mutation, None)
    }

    /// Creates a model that also reports failures to `report_failure`.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid operation or a branch list without `defect`.
    pub fn with_failure_reporter(
        operation: OperationRef,
        branches: &[&str],
        is_mutation: bool,
        report_failure: Option<FailureReporter>,
    ) -> Result<Self, ValidationError> {
        operation.validate()?;
        if !branches.contains(&DEFECT_BRANCH_ID) {
            return Err(ValidationError::new(
                "mock operation must declare defect branch",
            ));
        }
        Ok(Self {
            operation,
            branches: branches
                .iter()
                .map(|branch| (*branch).to_string())
                .collect(),
            is_mutation,
            report_failure,
            state: Mutex::new(ScriptedState {
                script: Script::new(),
                flow_scripts: BTreeMap::new(),
                is_strict: false,
                terminal_by_key: BTreeMap::new(),
                calls: Vec::new(),
                failures: Vec::new(),
            }),
        })
    }

    /// Queues cases for unscoped calls.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an undeclared branch or invalid retry or uncertainty.
    pub fn respond(&self, cases: Vec<Case<Output>>) -> Result<&Self, ValidationError> {
        self.validate_cases(&cases)?;
        self.lock_state().script.queue.extend(cases);
        Ok(self)
    }

    /// Answers every matching input with one reusable case.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid case.
    pub fn when(
        &self,
        predicate: impl Fn(&Input) -> bool + Send + Sync + 'static,
        response: Case<Output>,
    ) -> Result<&Self, ValidationError> {
        self.validate_cases(std::slice::from_ref(&response))?;
        self.lock_state()
            .script
            .rules
            .push((Box::new(predicate), response));
        Ok(self)
    }

    /// Sets fallback cases, repeating the last after exhaustion.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid case.
    pub fn default_cases(&self, cases: Vec<Case<Output>>) -> Result<&Self, ValidationError> {
        self.validate_cases(&cases)?;
        let mut state = self.lock_state();
        state.script.defaults = cases;
        state.script.default_count = 0;
        Ok(self)
    }

    /// Requires a `respond` or `when` answer for every call.
    pub fn strict(&self) -> &Self {
        self.lock_state().is_strict = true;
        self
    }

    /// Returns the script for a Flow ID or prefix, creating it once.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for a blank prefix.
    pub fn for_flow(
        &self,
        flow_id_or_prefix: &str,
    ) -> Result<FlowScript<'_, Input, Output>, ValidationError> {
        if flow_id_or_prefix.trim().is_empty() {
            return Err(ValidationError::new("Flow script prefix is required"));
        }
        self.lock_state()
            .flow_scripts
            .entry(flow_id_or_prefix.to_string())
            .or_insert_with(Script::new);
        Ok(FlowScript {
            operation: self,
            prefix: flow_id_or_prefix.to_string(),
        })
    }

    /// Resolves script precedence and replays terminal keyed Mutation results.
    ///
    /// An unscripted call, or a page outside a paged listing, answers `defect` and records a
    /// failure.
    pub fn answer(
        &self,
        flow_id: &str,
        call_id: &str,
        connection: &ConnectionRef,
        input: &Input,
        idempotency_key: &str,
        page_number: usize,
    ) -> Case<Output> {
        let mut state = self.lock_state();
        let replayed = if idempotency_key.is_empty() {
            None
        } else {
            state.terminal_by_key.get(idempotency_key).cloned()
        };
        let is_replay = replayed.is_some();
        let mut is_unscripted = false;
        let response = match replayed {
            Some(response) => response,
            None => {
                let mut response = self.scripted_answer(&mut state, flow_id, input);
                if response.is_none() {
                    is_unscripted = true;
                    self.record_failure(
                        &mut state,
                        &format!("unscripted call for Flow {flow_id:?}"),
                    );
                    response = Some(self.defect_case("unscripted connector mock call"));
                }
                let mut response =
                    response.unwrap_or_else(|| self.defect_case("unscripted connector mock call"));
                if let Case::Paged { branch, pages } = &response {
                    response = match page_number
                        .checked_sub(1)
                        .and_then(|index| pages.get(index))
                    {
                        Some(page) => Case::branch(branch.clone(), page.clone(), None),
                        None => {
                            let message = "requested page is outside the scripted listing";
                            self.record_failure(&mut state, message);
                            self.defect_case(message)
                        }
                    };
                }
                if !idempotency_key.is_empty() && !response.is_retry() {
                    state
                        .terminal_by_key
                        .insert(idempotency_key.to_string(), response.clone());
                }
                response
            }
        };
        let recorded = RecordedCall {
            connector_id: self.operation.connector_id.clone(),
            operation_id: self.operation.operation_id.clone(),
            connection_name: connection.name.clone(),
            flow_id: flow_id.to_string(),
            call_id: call_id.to_string(),
            input: input.clone(),
            branch: response.branch_id().to_string(),
            is_retry: response.is_retry(),
            is_replay,
            is_unscripted,
        };
        if let Err(error) = append_to_report(&recorded) {
            self.record_failure(&mut state, &error.to_string());
        }
        state.calls.push(recorded);
        response
    }

    /// Returns answered calls in answer order.
    pub fn calls(&self) -> Vec<RecordedCall<Input>> {
        self.lock_state().calls.clone()
    }

    /// Returns missing scripts and report failures for test assertions.
    pub fn failures(&self) -> Vec<String> {
        self.lock_state().failures.clone()
    }

    /// Fails the consuming test when calls were unscripted or invalid.
    ///
    /// # Panics
    ///
    /// Panics with every recorded failure joined by `; `.
    pub fn assert_no_failures(&self) {
        let failures = self.failures();
        assert!(failures.is_empty(), "{}", failures.join("; "));
    }

    fn scripted_answer(
        &self,
        state: &mut ScriptedState<Input, Output>,
        flow_id: &str,
        input: &Input,
    ) -> Option<Case<Output>> {
        let mut prefixes: Vec<String> = state
            .flow_scripts
            .keys()
            .filter(|prefix| flow_id.starts_with(prefix.as_str()))
            .cloned()
            .collect();
        prefixes.sort_by_key(|prefix| std::cmp::Reverse(prefix.len()));
        for prefix in &prefixes {
            if let Some(answer) = state
                .flow_scripts
                .get_mut(prefix)
                .and_then(|script| script.answer(input))
            {
                return Some(answer);
            }
        }
        if let Some(answer) = state.script.answer(input) {
            return Some(answer);
        }
        if state.is_strict {
            return None;
        }
        for prefix in &prefixes {
            if let Some(answer) = state
                .flow_scripts
                .get_mut(prefix)
                .and_then(Script::default_case)
            {
                return Some(answer);
            }
        }
        state.script.default_case()
    }

    fn defect_case(&self, message: &str) -> Case<Output> {
        Case::branch(
            DEFECT_BRANCH_ID,
            Output::default(),
            Some(Failure::new(
                FailureKind::LocalDefect,
                self.operation.connector_id.clone(),
                self.operation.operation_id.clone(),
                message,
            )),
        )
    }

    fn record_failure(&self, state: &mut ScriptedState<Input, Output>, message: &str) {
        state.failures.push(message.to_string());
        if let Some(report_failure) = &self.report_failure {
            report_failure(message);
        }
    }

    fn validate_cases(&self, cases: &[Case<Output>]) -> Result<(), ValidationError> {
        for response in cases {
            match response {
                Case::Branch {
                    branch, failure, ..
                } => {
                    if !self.branches.contains(branch) {
                        return Err(ValidationError::new("mock branch is not declared"));
                    }
                    if let Some(failure) = failure {
                        failure.validate()?;
                    }
                }
                Case::Paged { branch, .. } => {
                    if !self.branches.contains(branch) {
                        return Err(ValidationError::new("mock branch is not declared"));
                    }
                }
                Case::Uncertain { failure, .. } => {
                    if !self.is_mutation || !self.branches.contains(UNCERTAIN_BRANCH_ID) {
                        return Err(ValidationError::new(
                            "uncertainty requires a declared Mutation branch",
                        ));
                    }
                    failure.validate()?;
                }
                Case::Retry {
                    failure,
                    retry_after_seconds,
                } => {
                    if !retry_after_seconds.is_finite() || *retry_after_seconds < 0.0 {
                        return Err(ValidationError::new("invalid mock retry"));
                    }
                    failure.validate()?;
                }
            }
        }
        Ok(())
    }

    fn lock_state(&self) -> MutexGuard<'_, ScriptedState<Input, Output>> {
        // Test scripts hold plain data that stays consistent after a panicking predicate.
        self.state.lock().unwrap_or_else(PoisonError::into_inner)
    }
}

/// Cases scoped to one Flow ID prefix; they precede the operation's own script.
pub struct FlowScript<'operation, Input, Output> {
    operation: &'operation ScriptedOperation<Input, Output>,
    prefix: String,
}

impl<Input: Clone, Output: Clone + Default> FlowScript<'_, Input, Output> {
    /// Queues cases that answer the next matching calls.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid case.
    pub fn respond(&self, cases: Vec<Case<Output>>) -> Result<&Self, ValidationError> {
        self.operation.validate_cases(&cases)?;
        self.with_script(|script| script.queue.extend(cases));
        Ok(self)
    }

    /// Answers every matching input with one reusable case.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid case.
    pub fn when(
        &self,
        predicate: impl Fn(&Input) -> bool + Send + Sync + 'static,
        response: Case<Output>,
    ) -> Result<&Self, ValidationError> {
        self.operation
            .validate_cases(std::slice::from_ref(&response))?;
        self.with_script(|script| script.rules.push((Box::new(predicate), response)));
        Ok(self)
    }

    /// Sets fallback cases, repeating the last after exhaustion.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid case.
    pub fn default_cases(&self, cases: Vec<Case<Output>>) -> Result<&Self, ValidationError> {
        self.operation.validate_cases(&cases)?;
        self.with_script(|script| {
            script.defaults = cases;
            script.default_count = 0;
        });
        Ok(self)
    }

    fn with_script(&self, action: impl FnOnce(&mut Script<Input, Output>)) {
        let mut state = self.operation.lock_state();
        let script = state
            .flow_scripts
            .entry(self.prefix.clone())
            .or_insert_with(Script::new);
        action(script);
    }
}

/// Scripted Query implementing the real operation contract.
///
/// Construct it from the connector's actual definition. A live Dex Step supplies the Flow ID
/// for scoped scripts; a call outside a Step selects `defect`.
pub struct Query<Input, Output> {
    definition: QueryDefinition,
    scripted: ScriptedOperation<Input, Output>,
}

impl<Input: Clone, Output: Clone + Default> Query<Input, Output> {
    /// Creates a scripted Query.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid definition.
    pub fn new(
        definition: QueryDefinition,
        report_failure: Option<FailureReporter>,
    ) -> Result<Self, ValidationError> {
        definition.validate()?;
        let branches = branch_ids(definition.branches.as_deref());
        let scripted = ScriptedOperation::with_failure_reporter(
            definition.operation.clone(),
            &branches.iter().map(String::as_str).collect::<Vec<_>>(),
            false,
            report_failure,
        )?;
        Ok(Self {
            definition,
            scripted,
        })
    }

    /// Returns the script model for `respond`, `when`, `default_cases`, and assertions.
    pub fn scripted(&self) -> &ScriptedOperation<Input, Output> {
        &self.scripted
    }
}

impl<Input, Output> crate::Query for Query<Input, Output>
where
    Input: Clone + Send + Sync,
    Output: dex_sdk::Value + Clone + Default,
{
    type Input = Input;
    type Output = Output;

    fn definition(&self) -> QueryDefinition {
        self.definition.clone()
    }

    fn invoke(&self, call: &Call<'_>, input: &Input) -> QueryAttempt<Output> {
        let Some(flow_id) = call.flow_id() else {
            return QueryAttempt::branch(
                DEFECT_BRANCH_ID,
                Output::default(),
                mock_receipt(call),
                Some(missing_step_failure(&self.definition.operation)),
            );
        };
        match self
            .scripted
            .answer(flow_id, call.id.as_str(), &call.connection, input, "", 1)
        {
            Case::Retry {
                failure,
                retry_after_seconds,
            } => QueryAttempt::retry(failure, retry_after_seconds),
            Case::Branch {
                branch,
                value,
                failure,
            } => QueryAttempt::branch(branch, value, mock_receipt(call), failure),
            Case::Uncertain { value, failure } => QueryAttempt::branch(
                UNCERTAIN_BRANCH_ID,
                value,
                mock_receipt(call),
                Some(failure),
            ),
            Case::Paged { branch, pages } => QueryAttempt::branch(
                branch,
                pages.into_iter().next().unwrap_or_default(),
                mock_receipt(call),
                None,
            ),
        }
    }
}

/// Scripted Mutation with provider-like replay of terminal keyed outcomes.
///
/// The Call ID is the mock provider's idempotency key.
pub struct Mutation<Input, Output> {
    definition: MutationDefinition,
    scripted: ScriptedOperation<Input, Output>,
}

impl<Input: Clone, Output: Clone + Default> Mutation<Input, Output> {
    /// Creates a scripted Mutation.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid definition.
    pub fn new(
        definition: MutationDefinition,
        report_failure: Option<FailureReporter>,
    ) -> Result<Self, ValidationError> {
        definition.validate()?;
        let branches = branch_ids(definition.branches.as_deref());
        let scripted = ScriptedOperation::with_failure_reporter(
            definition.operation.clone(),
            &branches.iter().map(String::as_str).collect::<Vec<_>>(),
            true,
            report_failure,
        )?;
        Ok(Self {
            definition,
            scripted,
        })
    }

    /// Returns the script model for `respond`, `when`, `default_cases`, and assertions.
    pub fn scripted(&self) -> &ScriptedOperation<Input, Output> {
        &self.scripted
    }
}

impl<Input, Output> crate::Mutation for Mutation<Input, Output>
where
    Input: Clone + Send + Sync,
    Output: dex_sdk::Value + Clone + Default,
{
    type Input = Input;
    type Output = Output;

    fn definition(&self) -> MutationDefinition {
        self.definition.clone()
    }

    fn idempotency_key(&self, call_id: &CallId, _input: &Input) -> IdempotencyKey {
        IdempotencyKey::new(call_id.as_str())
    }

    fn invoke(&self, call: &Call<'_>, input: &Input) -> MutationAttempt<Output> {
        let Some(flow_id) = call.flow_id() else {
            return MutationAttempt::branch(
                DEFECT_BRANCH_ID,
                Output::default(),
                mock_receipt(call),
                Some(missing_step_failure(&self.definition.operation)),
            );
        };
        match self.scripted.answer(
            flow_id,
            call.id.as_str(),
            &call.connection,
            input,
            call.idempotency_key.as_str(),
            1,
        ) {
            Case::Retry {
                failure,
                retry_after_seconds,
            } => MutationAttempt::retry(failure, retry_after_seconds),
            Case::Uncertain { value, failure } => {
                MutationAttempt::uncertain(value, failure, mock_receipt(call))
            }
            Case::Branch {
                branch,
                value,
                failure,
            } => MutationAttempt::branch(branch, value, mock_receipt(call), failure),
            Case::Paged { branch, pages } => MutationAttempt::branch(
                branch,
                pages.into_iter().next().unwrap_or_default(),
                mock_receipt(call),
                None,
            ),
        }
    }
}

fn branch_ids(branches: Option<&[crate::types::BranchDefinition]>) -> Vec<String> {
    branches
        .unwrap_or_default()
        .iter()
        .map(|branch| branch.id.clone())
        .collect()
}

fn missing_step_failure(operation: &OperationRef) -> Failure {
    Failure::new(
        FailureKind::LocalDefect,
        operation.connector_id.clone(),
        operation.operation_id.clone(),
        "connector mock requires a live Dex Step context",
    )
}

fn mock_receipt(call: &Call<'_>) -> Receipt {
    Receipt {
        call_id: call.id.clone(),
        idempotency_key: call.idempotency_key.clone(),
        provider: call.operation.connector_id.clone(),
        observed_at: Rfc3339NanoTimestamp::now_utc(),
        ..Receipt::default()
    }
}

/// In-memory stand-in for one Dex Step attempt, for conformance replay and unit tests.
///
/// It supplies the Flow ID, attempt number, and the previous attempt's heartbeat value that a
/// real Step attempt reads from Dex, and records heartbeat writes in memory, so connector
/// operations and [`crate::singledispatch::Fence`] run outside a Worker. Create one per
/// replayed attempt; progress and text writes are no-ops.
pub struct ReplayStepAttempt {
    flow_id: String,
    attempt: u32,
    previous_heartbeat: Option<JsonValue>,
    written_heartbeat: Mutex<Option<JsonValue>>,
    checkpoint_markers: Mutex<Option<BTreeMap<String, crate::singledispatch::markers::Marker>>>,
    running_mutations: Mutex<BTreeSet<OperationRef>>,
}

impl ReplayStepAttempt {
    /// Creates attempt `attempt` of a Step in `flow_id` whose previous attempt persisted
    /// `previous_heartbeat`.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for a blank Flow ID or a zero attempt.
    pub fn new(
        flow_id: impl Into<String>,
        attempt: u32,
        previous_heartbeat: Option<JsonValue>,
    ) -> Result<Self, ValidationError> {
        let flow_id = flow_id.into();
        if flow_id.trim().is_empty() || attempt == 0 {
            return Err(ValidationError::new(
                "replay attempt requires a Flow ID and a positive attempt",
            ));
        }
        Ok(Self {
            flow_id,
            attempt,
            previous_heartbeat,
            written_heartbeat: Mutex::new(None),
            checkpoint_markers: Mutex::new(None),
            running_mutations: Mutex::new(BTreeSet::new()),
        })
    }

    /// Creates a Call bound to this attempt with a fixed Call ID and a send cap of 1.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid Call ID or reference.
    pub fn new_call(
        &self,
        call_id: CallId,
        connection: ConnectionRef,
        operation: OperationRef,
    ) -> Result<Call<'_>, ValidationError> {
        let mut call = Call::new(call_id, connection, operation)?;
        call.step_attempt = Some(self);
        Ok(call)
    }

    /// Returns the one-based attempt number.
    pub fn attempt(&self) -> u32 {
        self.attempt
    }

    /// Returns the last heartbeat value written during this attempt, or `None` when the
    /// attempt wrote none.
    pub fn written_heartbeat(&self) -> Option<JsonValue> {
        self.written_heartbeat
            .lock()
            .unwrap_or_else(PoisonError::into_inner)
            .clone()
    }
}

impl crate::step_attempt::StepAttemptAccess for ReplayStepAttempt {
    fn attempt_number(&self) -> u32 {
        self.attempt
    }

    fn attempt_flow_id(&self) -> &str {
        &self.flow_id
    }

    fn has_progress_stream(&self) -> bool {
        false
    }

    fn has_text_stream(&self) -> bool {
        false
    }

    fn report_progress(&self, _call_id: &CallId, _progress: crate::types::Progress) {}

    fn write_text(&self, _value: &str) {}

    fn begin_running_mutation(&self, operation: &OperationRef) -> bool {
        self.running_mutations
            .lock()
            .unwrap_or_else(PoisonError::into_inner)
            .insert(operation.clone())
    }

    fn end_running_mutation(&self, operation: &OperationRef) {
        self.running_mutations
            .lock()
            .unwrap_or_else(PoisonError::into_inner)
            .remove(operation);
    }

    fn checkpoint_markers(
        &self,
    ) -> &Mutex<Option<BTreeMap<String, crate::singledispatch::markers::Marker>>> {
        &self.checkpoint_markers
    }

    fn last_heartbeat_value(&self) -> Result<Option<JsonValue>, String> {
        Ok(self.previous_heartbeat.clone())
    }

    fn record_heartbeat_value(
        &self,
        value: crate::singledispatch::markers::CheckpointHeartbeat,
    ) -> Result<(), String> {
        let value = serde_json::to_value(value).map_err(|error| error.to_string())?;
        *self
            .written_heartbeat
            .lock()
            .unwrap_or_else(PoisonError::into_inner) = Some(value);
        Ok(())
    }
}

/// Fixture loading failure.
#[derive(Debug)]
pub enum FixtureError {
    /// The file could not be read.
    Io(io::Error),
    /// The JSON is invalid, has duplicate fields, or is not an object.
    Json(serde_json::Error),
    /// The top-level object has fields outside the allowed set.
    UnknownFields,
}

impl fmt::Display for FixtureError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            FixtureError::Io(error) => write!(formatter, "read fixture: {error}"),
            FixtureError::Json(error) => write!(formatter, "decode fixture: {error}"),
            FixtureError::UnknownFields => formatter.write_str("fixture has unknown fields"),
        }
    }
}

impl std::error::Error for FixtureError {}

/// Loads one JSON object, rejecting duplicate fields at any depth and unknown top-level
/// fields when `allowed_fields` is given.
///
/// # Errors
///
/// Returns [`FixtureError`] for unreadable, invalid, duplicate-field, non-object, or
/// unknown-field documents.
pub fn load_fixture(
    path: &Path,
    allowed_fields: Option<&[&str]>,
) -> Result<JsonMap<String, JsonValue>, FixtureError> {
    let bytes = fs::read(path).map_err(FixtureError::Io)?;
    let mut deserializer = serde_json::Deserializer::from_slice(&bytes);
    let document =
        StrictJsonValue::deserialize_strict(&mut deserializer).map_err(FixtureError::Json)?;
    deserializer.end().map_err(FixtureError::Json)?;
    let JsonValue::Object(document) = document else {
        return Err(FixtureError::Json(serde::de::Error::custom(
            "fixture must be a JSON object",
        )));
    };
    if let Some(allowed_fields) = allowed_fields
        && document
            .keys()
            .any(|key| !allowed_fields.contains(&key.as_str()))
    {
        return Err(FixtureError::UnknownFields);
    }
    Ok(document)
}

struct StrictJsonValue;

impl StrictJsonValue {
    fn deserialize_strict<'de, D: Deserializer<'de>>(
        deserializer: D,
    ) -> Result<JsonValue, D::Error> {
        deserializer.deserialize_any(StrictJsonVisitor)
    }
}

struct StrictJsonSeed;

impl<'de> serde::de::DeserializeSeed<'de> for StrictJsonSeed {
    type Value = JsonValue;

    fn deserialize<D: Deserializer<'de>>(self, deserializer: D) -> Result<JsonValue, D::Error> {
        StrictJsonValue::deserialize_strict(deserializer)
    }
}

struct StrictJsonVisitor;

impl<'de> Visitor<'de> for StrictJsonVisitor {
    type Value = JsonValue;

    fn expecting(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("a JSON value without duplicate object fields")
    }

    fn visit_bool<E>(self, value: bool) -> Result<JsonValue, E> {
        Ok(JsonValue::Bool(value))
    }

    fn visit_i64<E>(self, value: i64) -> Result<JsonValue, E> {
        Ok(JsonValue::from(value))
    }

    fn visit_u64<E>(self, value: u64) -> Result<JsonValue, E> {
        Ok(JsonValue::from(value))
    }

    fn visit_f64<E>(self, value: f64) -> Result<JsonValue, E> {
        Ok(JsonValue::from(value))
    }

    fn visit_str<E>(self, value: &str) -> Result<JsonValue, E> {
        Ok(JsonValue::String(value.to_string()))
    }

    fn visit_string<E>(self, value: String) -> Result<JsonValue, E> {
        Ok(JsonValue::String(value))
    }

    fn visit_unit<E>(self) -> Result<JsonValue, E> {
        Ok(JsonValue::Null)
    }

    fn visit_none<E>(self) -> Result<JsonValue, E> {
        Ok(JsonValue::Null)
    }

    fn visit_seq<A: serde::de::SeqAccess<'de>>(self, mut items: A) -> Result<JsonValue, A::Error> {
        let mut values = Vec::new();
        while let Some(value) = items.next_element_seed(StrictJsonSeed)? {
            values.push(value);
        }
        Ok(JsonValue::Array(values))
    }

    fn visit_map<A: MapAccess<'de>>(self, mut entries: A) -> Result<JsonValue, A::Error> {
        let mut object = JsonMap::new();
        while let Some(key) = entries.next_key::<String>()? {
            if object.contains_key(&key) {
                return Err(serde::de::Error::custom(
                    "fixture has duplicate JSON fields",
                ));
            }
            let value = entries.next_value_seed(StrictJsonSeed)?;
            object.insert(key, value);
        }
        Ok(JsonValue::Object(object))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn fixture() -> JsonValue {
        let path = Path::new(env!("CARGO_MANIFEST_DIR")).join("../conformance/connectormock.json");
        serde_json::from_slice(&fs::read(path).expect("fixture is readable"))
            .expect("fixture is JSON")
    }

    fn text(value: &JsonValue, key: &str) -> String {
        value
            .get(key)
            .and_then(JsonValue::as_str)
            .unwrap_or_default()
            .to_string()
    }

    fn flag(value: &JsonValue, key: &str) -> bool {
        value.get(key).and_then(JsonValue::as_bool).unwrap_or(false)
    }

    fn recorded(value: &JsonValue, operation_key: &str, connection_key: &str) -> RecordedCall<()> {
        RecordedCall {
            connector_id: text(value, "connectorId"),
            operation_id: text(value, operation_key),
            connection_name: text(value, connection_key),
            flow_id: text(value, "flowId"),
            call_id: text(value, "callId"),
            input: (),
            branch: text(value, "branch"),
            is_retry: flag(value, "retry"),
            is_replay: flag(value, "replay"),
            is_unscripted: flag(value, "unscripted"),
        }
    }

    #[test]
    fn definitions_round_trip_fixture() {
        let fixture = fixture();
        for key in ["queryDefinition", "emptyBranches", "nullBranches"] {
            let definition: QueryDefinition =
                serde_json::from_value(fixture[key].clone()).expect("definition decodes");
            assert_eq!(
                serde_json::to_value(&definition).expect("definition encodes"),
                fixture[key],
                "{key}"
            );
        }
        let mutation: MutationDefinition =
            serde_json::from_value(fixture["mutationDefinition"].clone()).expect("decodes");
        assert_eq!(
            serde_json::to_value(&mutation).expect("encodes"),
            fixture["mutationDefinition"]
        );
    }

    #[test]
    fn recorded_calls_match_go_projection() {
        let fixture = fixture();
        let mock = ScriptedOperation::<(), String>::new(
            OperationRef::new("sample", "createItem"),
            &["created", "defect"],
            false,
        )
        .expect("mock is valid");
        mock.respond(vec![Case::branch("created", "ok".to_string(), None)])
            .expect("case is declared");
        mock.answer(
            "flow-42",
            &text(&fixture["recordedCall"], "callId"),
            &ConnectionRef::new("sample", "main"),
            &(),
            "",
            1,
        );
        assert_eq!(mock.calls()[0].to_wire(), fixture["recordedCall"]);
        for key in [
            "retryRecordedCall",
            "replayedRecordedCall",
            "unscriptedRecordedCall",
        ] {
            let value = &fixture[key];
            assert_eq!(
                recorded(value, "operationId", "connectionName").to_wire(),
                *value,
                "{key}"
            );
        }
    }

    #[test]
    fn report_file_matches_go_bytes_after_two_appends() {
        let fixture = fixture();
        assert_eq!(
            fixture["reportDirectoryEnvironment"],
            REPORT_DIRECTORY_ENVIRONMENT
        );
        assert_eq!(fixture["reportFileName"], REPORT_FILE_NAME);
        let directory = tempfile::tempdir().expect("temporary directory");
        let rows = fixture["reportFile"]["calls"]
            .as_array()
            .expect("report rows")
            .clone();
        let mut last = None;
        for row in &rows {
            let call = recorded(row, "operation", "connection");
            assert_eq!(call.to_report_wire(), *row);
            append_to_report_in(directory.path(), &call).expect("row appends");
            last = Some(call);
        }
        let report_path = directory.path().join(REPORT_FILE_NAME);
        let bytes = fs::read(&report_path).expect("report is readable");
        assert!(
            bytes
                .windows(18)
                .any(|window| window == b"\\u003c\\u0026\\u003e")
        );
        let report: JsonValue = serde_json::from_slice(&bytes).expect("report is JSON");
        assert_eq!(report, fixture["reportFile"]);
        fs::write(&report_path, b"{\"calls\":[]}\n").expect("report rewrites");
        let last = last.expect("rows exist");
        assert!(matches!(
            append_to_report_in(directory.path(), &last),
            Err(ReportError::MalformedReport)
        ));
    }

    #[test]
    fn report_row_fixture_matches() {
        let fixture = fixture();
        let call = RecordedCall {
            connector_id: "sample".to_string(),
            operation_id: "createItem".to_string(),
            connection_name: "main".to_string(),
            flow_id: "flow-42".to_string(),
            call_id: text(&fixture["report"], "callId"),
            input: (),
            branch: "created".to_string(),
            is_retry: false,
            is_replay: false,
            is_unscripted: false,
        };
        assert_eq!(call.to_report_wire(), fixture["report"]);
    }

    #[test]
    fn flow_precedence_retry_and_terminal_key_replay() {
        let mock = ScriptedOperation::<(), String>::new(
            OperationRef::new("sample", "createItem"),
            &["created", "defect", "uncertain"],
            true,
        )
        .expect("mock is valid");
        let retry = Case::retry(
            Failure::new(FailureKind::RateLimit, "sample", "createItem", "throttled"),
            0.0,
        );
        mock.default_cases(vec![Case::branch("created", "default".to_string(), None)])
            .expect("default is valid");
        mock.for_flow("flow-special")
            .expect("prefix is valid")
            .respond(vec![
                retry,
                Case::branch("created", "special".to_string(), None),
            ])
            .expect("cases are valid");
        let connection = ConnectionRef::new("sample", "main");
        assert!(
            mock.answer("flow-special-1", "call", &connection, &(), "key", 1)
                .is_retry()
        );
        let special = Case::branch("created", "special".to_string(), None);
        assert_eq!(
            mock.answer("flow-special-1", "call", &connection, &(), "key", 1),
            special
        );
        assert_eq!(
            mock.answer("flow-special-1", "call", &connection, &(), "key", 1),
            special
        );
        assert!(mock.calls().last().is_some_and(|call| call.is_replay));
        assert_eq!(
            mock.answer("flow-other", "call-2", &connection, &(), "other", 1),
            Case::branch("created", "default".to_string(), None)
        );
    }

    #[test]
    fn strict_unscripted_paged_and_invalid_cases() {
        let mock = ScriptedOperation::<u32, String>::new(
            OperationRef::new("sample", "getItem"),
            &["found", "defect"],
            false,
        )
        .expect("mock is valid");
        let failure = Failure::new(FailureKind::Transport, "sample", "getItem", "unknown");
        assert!(
            mock.respond(vec![Case::uncertain(String::new(), failure)])
                .is_err()
        );
        assert!(
            mock.respond(vec![Case::branch("undeclared", String::new(), None)])
                .is_err()
        );
        mock.default_cases(vec![Case::branch("found", "fallback".to_string(), None)])
            .expect("default is valid")
            .strict();
        let connection = ConnectionRef::new("sample", "main");
        let unscripted = mock.answer("flow-1", "call-1", &connection, &1, "", 1);
        assert_eq!(unscripted.branch_id(), DEFECT_BRANCH_ID);
        assert!(mock.calls()[0].is_unscripted);
        mock.respond(vec![
            Case::paged("found", vec!["first".to_string(), "second".to_string()])
                .expect("pages exist"),
        ])
        .expect("paged is valid");
        let outside = mock.answer("flow-1", "call-2", &connection, &3, "", 3);
        assert_eq!(outside.branch_id(), DEFECT_BRANCH_ID);
        assert_eq!(mock.failures().len(), 2);
    }

    #[test]
    fn mutation_mock_replays_keyed_outcome_inside_replay_attempt() {
        let fixture = fixture();
        let definition: MutationDefinition =
            serde_json::from_value(fixture["mutationDefinition"].clone()).expect("decodes");
        let mock = Mutation::<String, String>::new(definition.clone(), None).expect("valid");
        mock.scripted()
            .respond(vec![Case::branch("created", "object-42".to_string(), None)])
            .expect("declared");
        let attempt = ReplayStepAttempt::new("flow-42", 1, None).expect("valid attempt");
        let call = attempt
            .new_call(
                CallId::new("5af8fafa-06ff-5b2d-bb13-04693809fabf"),
                ConnectionRef::new("sample", "main"),
                definition.operation.clone(),
            )
            .expect("valid call");
        for _ in 0..2 {
            let outcome = crate::run_mutation(&mock, &call, &"input".to_string());
            let result = outcome.into_result().expect("terminal result");
            assert_eq!(result.branch, "created");
            assert_eq!(result.value.as_deref(), Some("object-42"));
            assert_eq!(result.receipt.idempotency_key.as_str(), call.id.as_str());
        }
        let calls = mock.scripted().calls();
        assert_eq!(calls[0].flow_id, "flow-42");
        assert!(calls[1].is_replay);
        let outside = Call::new(
            CallId::new("5af8fafa-06ff-5b2d-bb13-04693809fabf"),
            ConnectionRef::new("sample", "main"),
            definition.operation.clone(),
        )
        .expect("valid call");
        let defect = crate::run_mutation(&mock, &outside, &"input".to_string())
            .into_result()
            .expect("terminal result");
        assert_eq!(defect.branch, DEFECT_BRANCH_ID);
        assert_eq!(attempt.written_heartbeat(), None);
    }

    #[test]
    fn strict_fixture_loader_rejects_duplicates_and_unknown_fields() {
        let directory = tempfile::tempdir().expect("temporary directory");
        let duplicate = directory.path().join("duplicate.json");
        fs::write(&duplicate, b"{\"a\":1,\"a\":2}").expect("write");
        assert!(load_fixture(&duplicate, None).is_err());
        let nested = directory.path().join("nested.json");
        fs::write(&nested, b"{\"a\":{\"b\":1,\"b\":2}}").expect("write");
        assert!(load_fixture(&nested, None).is_err());
        let valid = directory.path().join("valid.json");
        fs::write(&valid, b"{\"a\":[1,{\"b\":null}]}").expect("write");
        assert!(load_fixture(&valid, Some(&["a"])).is_ok());
        assert!(matches!(
            load_fixture(&valid, Some(&["b"])),
            Err(FixtureError::UnknownFields)
        ));
    }
}
