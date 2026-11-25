// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! At-most-once provider dispatch using Dex's public heartbeat values.

use std::collections::BTreeMap;
use std::fmt;
use std::sync::mpsc::{self, RecvTimeoutError};
use std::sync::{Mutex, PoisonError};
use std::time::{Duration, Instant};

use serde_json::Value as JsonValue;

use crate::errors::ValidationError;
use crate::step_attempt::StepAttemptAccess;
use crate::types::Call;

use super::markers::{
    Decision, DecisionKind, MINIMUM_MAX_SENDS, Marker, State, checkpoint_heartbeat,
    decide_dispatch, markers_from_heartbeat, validate_max_sends,
};

const DEFAULT_WRITE_WINDOW_FRACTION: f64 = 7.0 / 12.0;
const MAXIMUM_WRITE_WINDOW_FRACTION: f64 = 5.0 / 6.0;
const HEARTBEAT_INTERVAL: Duration = Duration::from_secs(5);

/// Single-dispatch failure; every variant except [`FenceError::Checkpoint`] proves nothing was
/// sent by this call of the Fence.
#[derive(Debug)]
pub enum FenceError {
    /// The attempt has no remaining permission to send.
    DispatchNotAllowed,
    /// The write window cannot fit the minimum request time; nothing was sent.
    InsufficientAttemptTime {
        /// The `notSent` heartbeat error, when recording it also failed.
        checkpoint_error: Option<String>,
    },
    /// The claim heartbeat failed before the provider request was sent.
    Checkpoint(String),
}

impl fmt::Display for FenceError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            FenceError::DispatchNotAllowed => {
                formatter.write_str("single dispatch is not allowed for this attempt")
            }
            FenceError::InsufficientAttemptTime {
                checkpoint_error: None,
            } => formatter.write_str("insufficient attempt time for single dispatch"),
            FenceError::InsufficientAttemptTime {
                checkpoint_error: Some(_),
            } => formatter.write_str("single dispatch has insufficient time and checkpoint failed"),
            FenceError::Checkpoint(_) => {
                formatter.write_str("single-dispatch checkpoint could not be recorded")
            }
        }
    }
}

impl std::error::Error for FenceError {}

struct FenceState {
    decision: Option<Decision>,
    has_dispatched: bool,
    has_send_finished: bool,
    has_pre_send_retry: bool,
    marker: Option<Marker>,
}

/// Fences one non-idempotent provider send in a synchronous Execute attempt.
///
/// Construct one Fence per logical Call and attempt. Call [`Fence::decide`] before resolving
/// credentials. [`Fence::dispatch`] records the claim before calling `send`, repeats it every
/// five seconds while the request runs, and never calls `send` twice.
///
/// `max_sends` caps the possibly effective sends of one Call ID across all attempts; pass
/// [`Call::max_sends`]. The value 1 never resends after a possible send. A value from 2 to 10
/// enables bounded resend: [`DecisionKind::ReadBackThenResendAllowed`] permits Dispatch after
/// the connector's read-back is not found or inconclusive. Every heartbeat holds one entry per
/// Call ID the Step attempt knows, so several Fences of one Step do not lose each other's
/// entries. The heartbeat format is internal to the connector SDKs; hosts must not parse it.
///
/// Unlike Python, a synchronous send cannot be cancelled at the write deadline: `send`
/// receives the deadline and must bound its own I/O by it, as [`crate::providerhttp`] does.
pub struct Fence<'call> {
    call: &'call Call<'call>,
    step_attempt: &'call (dyn StepAttemptAccess + 'call),
    attempt: u32,
    max_sends: u32,
    write_deadline: Instant,
    minimum_request_time: Duration,
    state: Mutex<FenceState>,
}

impl<'call> Fence<'call> {
    /// Creates a Fence with the default write window of 7/12 of the Execute timeout.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for a Call outside a Step, a zero timeout, or an invalid cap.
    pub fn new(
        call: &'call Call<'call>,
        execute_timeout: Duration,
        max_sends: u32,
    ) -> Result<Self, ValidationError> {
        Self::with_write_window_fraction(
            call,
            execute_timeout,
            max_sends,
            DEFAULT_WRITE_WINDOW_FRACTION,
        )
    }

    /// Creates a Fence whose write window is `write_window_fraction` of the Execute timeout.
    ///
    /// The fraction must be within (0, 5/6].
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for a Call outside a Step, a zero timeout, an invalid cap,
    /// or a fraction out of range.
    pub fn with_write_window_fraction(
        call: &'call Call<'call>,
        execute_timeout: Duration,
        max_sends: u32,
        write_window_fraction: f64,
    ) -> Result<Self, ValidationError> {
        let step_attempt = call.step_attempt.ok_or_else(|| {
            ValidationError::new(
                "single dispatch requires a call, positive attempt, and Execute timeout",
            )
        })?;
        let attempt = step_attempt.attempt_number();
        if call.id.is_empty() || attempt < 1 || execute_timeout.is_zero() {
            return Err(ValidationError::new(
                "single dispatch requires a call, positive attempt, and Execute timeout",
            ));
        }
        validate_max_sends(max_sends)?;
        if !write_window_fraction.is_finite()
            || write_window_fraction <= 0.0
            || write_window_fraction > MAXIMUM_WRITE_WINDOW_FRACTION
        {
            return Err(ValidationError::new(
                "single-dispatch write window must be within (0, 5/6]",
            ));
        }
        let started_at = Instant::now();
        Ok(Self {
            call,
            step_attempt,
            attempt,
            max_sends,
            write_deadline: started_at + execute_timeout.mul_f64(write_window_fraction),
            minimum_request_time: execute_timeout / 10,
            state: Mutex::new(FenceState {
                decision: None,
                has_dispatched: false,
                has_send_finished: false,
                has_pre_send_retry: false,
                marker: None,
            }),
        })
    }

    /// Returns the instant by which `send` must finish.
    pub fn write_deadline(&self) -> Instant {
        self.write_deadline
    }

    /// Reads this call's prior checkpoint entry before any credential or provider lookup.
    ///
    /// A heartbeat whose entries cannot be decoded counts as no marker; an error reading the
    /// heartbeat fails closed, as in sdkgo.
    pub fn decide(&self) -> Decision {
        let mut state = self.lock_state();
        if let Some(decision) = &state.decision {
            return decision.clone();
        }
        let decision = match self.previous_heartbeat() {
            Ok(previous) => decide_dispatch(
                &self.call.id,
                self.attempt,
                previous.as_ref(),
                self.max_sends,
            )
            .unwrap_or_else(|_| {
                Decision::fail_closed(None, "single-dispatch checkpoint could not be read")
            }),
            Err(_) => Decision::fail_closed(None, "single-dispatch checkpoint could not be read"),
        };
        state.decision = Some(decision.clone());
        decision
    }

    /// Checkpoints a claim, calls `send` once, and repeats the claim heartbeat meanwhile.
    ///
    /// Allowed once after [`DecisionKind::SendAllowed`], or after
    /// [`DecisionKind::ReadBackThenResendAllowed`] when read-back did not confirm the write.
    /// The claim records one more send than the decision's prior send upper bound. A too-short
    /// write window or failed claim records `notSent` only after `SendAllowed`.
    ///
    /// # Errors
    ///
    /// Returns [`FenceError`] when Dispatch is not allowed, the window is too short, or the
    /// claim heartbeat fails; `send` was not called in each case.
    pub fn dispatch<Output: Send>(
        &self,
        send: impl FnOnce(Instant) -> Output + Send,
    ) -> Result<Output, FenceError> {
        let claim = {
            let mut state = self.lock_state();
            let Some(decision) = state.decision.clone() else {
                return Err(FenceError::DispatchNotAllowed);
            };
            if !decision.kind.permits_dispatch() || state.has_dispatched || state.has_pre_send_retry
            {
                return Err(FenceError::DispatchNotAllowed);
            }
            let should_record_not_sent = decision.kind == DecisionKind::SendAllowed;
            let now = Instant::now();
            if self.minimum_request_time.is_zero()
                || self.write_deadline.saturating_duration_since(now) < self.minimum_request_time
            {
                state.has_pre_send_retry = true;
                let mut checkpoint_error = None;
                if should_record_not_sent {
                    let marker = self.marker(State::NotSent, "", decision.prior_send_upper_bound);
                    match self.record(&marker, false) {
                        Ok(()) => state.marker = Some(marker),
                        Err(error) => checkpoint_error = Some(error),
                    }
                }
                return Err(FenceError::InsufficientAttemptTime { checkpoint_error });
            }
            state.has_dispatched = true;
            let claim = self.marker(State::Claimed, "", decision.prior_send_upper_bound + 1);
            if let Err(error) = self.record(&claim, false) {
                state.has_pre_send_retry = true;
                if should_record_not_sent {
                    let marker = self.marker(State::NotSent, "", decision.prior_send_upper_bound);
                    if self.record(&marker, false).is_ok() {
                        state.marker = Some(marker);
                    }
                }
                return Err(FenceError::Checkpoint(error));
            }
            state.marker = Some(claim.clone());
            claim
        };
        let (stop_sender, stop_receiver) = mpsc::channel::<()>();
        let output = std::thread::scope(|scope| {
            let claim = &claim;
            scope.spawn(move || self.repeat_claim(claim, &stop_receiver));
            let output = send(self.write_deadline);
            self.lock_state().has_send_finished = true;
            // The repeater exits when the channel closes or receives the stop signal.
            drop(stop_sender);
            output
        });
        Ok(output)
    }

    /// Checkpoints a pre-send Retry and forbids Dispatch for this attempt.
    ///
    /// Allowed only after [`DecisionKind::SendAllowed`]. The marker carries the prior send
    /// upper bound.
    ///
    /// # Errors
    ///
    /// Returns [`FenceError::DispatchNotAllowed`] in any other state, or
    /// [`FenceError::Checkpoint`] when the heartbeat fails.
    pub fn record_not_sent(&self) -> Result<(), FenceError> {
        let mut state = self.lock_state();
        let Some(decision) = state.decision.clone() else {
            return Err(FenceError::DispatchNotAllowed);
        };
        if decision.kind != DecisionKind::SendAllowed
            || state.has_dispatched
            || state.has_pre_send_retry
        {
            return Err(FenceError::DispatchNotAllowed);
        }
        state.has_pre_send_retry = true;
        let marker = self.marker(State::NotSent, "", decision.prior_send_upper_bound);
        self.record(&marker, false)
            .map_err(FenceError::Checkpoint)?;
        state.marker = Some(marker);
        Ok(())
    }

    /// Checkpoints positive proof that the provider did not process the request.
    ///
    /// The marker carries the send count from before this attempt's claim.
    ///
    /// # Errors
    ///
    /// Returns [`FenceError::DispatchNotAllowed`] unless a send finished with a claim and no
    /// provider ID, or [`FenceError::Checkpoint`] when the heartbeat fails.
    pub fn record_can_resend(&self) -> Result<(), FenceError> {
        let mut state = self.lock_state();
        let is_allowed = state.has_send_finished
            && state.marker.as_ref().is_some_and(|marker| {
                marker.state == State::Claimed && marker.provider_id.is_empty()
            });
        if !is_allowed {
            return Err(FenceError::DispatchNotAllowed);
        }
        let prior = Self::prior_send_count(&state);
        let marker = self.marker(State::CanResend, "", prior);
        self.record(&marker, false)
            .map_err(FenceError::Checkpoint)?;
        state.marker = Some(marker);
        Ok(())
    }

    /// Checkpoints an accepted resource identity for later read-back.
    ///
    /// The marker carries the same send count as this attempt's claim. When the heartbeat
    /// fails the error is returned, but the provider ID stays in this attempt's checkpoint.
    ///
    /// # Errors
    ///
    /// Returns [`FenceError::DispatchNotAllowed`] unless a claimed send finished and
    /// `provider_id` is nonempty, or [`FenceError::Checkpoint`] when the heartbeat fails.
    pub fn record_provider_id(&self, provider_id: &str) -> Result<(), FenceError> {
        let mut state = self.lock_state();
        let is_allowed = state.has_send_finished
            && !provider_id.is_empty()
            && state
                .marker
                .as_ref()
                .is_some_and(|marker| marker.state == State::Claimed);
        if !is_allowed {
            return Err(FenceError::DispatchNotAllowed);
        }
        let prior = Self::prior_send_count(&state);
        let accepted = self.marker(State::Claimed, provider_id, prior + 1);
        let recorded = self.record(&accepted, true);
        state.marker = Some(accepted);
        recorded.map_err(FenceError::Checkpoint)
    }

    fn prior_send_count(state: &FenceState) -> u32 {
        state
            .decision
            .as_ref()
            .map_or(0, |decision| decision.prior_send_upper_bound)
    }

    fn marker(&self, state: State, provider_id: &str, send_count: u32) -> Marker {
        Marker {
            call_id: self.call.id.clone(),
            attempt: self.attempt,
            state,
            provider_id: provider_id.to_string(),
            send_count,
        }
    }

    fn previous_heartbeat(&self) -> Result<Option<JsonValue>, String> {
        self.step_attempt.last_heartbeat_value()
    }

    /// Merges `marker` into the Step checkpoint and heartbeats every known entry.
    fn record(&self, marker: &Marker, should_retain_on_failure: bool) -> Result<(), String> {
        let checkpoint = self.step_attempt.checkpoint_markers();
        // The lock spans the heartbeat so the last persisted value has every committed entry.
        let mut current = checkpoint.lock().unwrap_or_else(PoisonError::into_inner);
        let mut markers = match current.as_ref() {
            Some(markers) => markers.clone(),
            None => self.previous_attempt_markers(),
        };
        markers.insert(marker.call_id.as_str().to_string(), marker.clone());
        let result = self
            .step_attempt
            .record_heartbeat_value(checkpoint_heartbeat(&markers));
        if result.is_ok() || should_retain_on_failure {
            *current = Some(markers);
        } else if current.is_none() {
            *current = Some(self.previous_attempt_markers());
        }
        result
    }

    fn previous_attempt_markers(&self) -> BTreeMap<String, Marker> {
        // Seeding nothing loses entries, which only overcounts earlier sends.
        self.previous_heartbeat()
            .ok()
            .flatten()
            .map(|heartbeat| markers_from_heartbeat(&heartbeat))
            .unwrap_or_default()
    }

    fn repeat_claim(&self, claim: &Marker, stop: &mpsc::Receiver<()>) {
        loop {
            match stop.recv_timeout(HEARTBEAT_INTERVAL) {
                Ok(()) | Err(RecvTimeoutError::Disconnected) => return,
                Err(RecvTimeoutError::Timeout) => {}
            }
            let state = self.lock_state();
            if state.marker.as_ref() == Some(claim) && !state.has_send_finished {
                // A missed repeat only shortens the heartbeat; the claim is already durable.
                if let Err(error) = self.record(claim, false) {
                    tracing::warn!(error = %error, "single-dispatch claim heartbeat failed");
                }
            }
        }
    }

    fn lock_state(&self) -> std::sync::MutexGuard<'_, FenceState> {
        self.state.lock().unwrap_or_else(PoisonError::into_inner)
    }
}

/// Default send cap: one send, no bounded resend.
pub const DEFAULT_MAX_SENDS: u32 = MINIMUM_MAX_SENDS;
