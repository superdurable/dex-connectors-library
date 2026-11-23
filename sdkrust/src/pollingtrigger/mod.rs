// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Polling Trigger helpers: the stable credential Call and poll schedules.
//!
//! A connector's own polling source, such as Gmail's, resolves credentials with
//! [`new_trigger_credential_call`] and spaces polls with [`PollSchedule`].

mod credential;
mod schedule;

pub use credential::new_trigger_credential_call;
pub use schedule::PollSchedule;
