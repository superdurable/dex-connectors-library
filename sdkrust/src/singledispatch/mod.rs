// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Single-dispatch markers and the Dex heartbeat Fence.
//!
//! A [`Fence`] guards one non-idempotent provider send per Call and attempt. The checkpoint
//! is a Dex heartbeat value shared by every Fence of one Step attempt.

mod fence;
pub(crate) mod markers;

#[cfg(test)]
mod conformance_tests;

pub use fence::{DEFAULT_MAX_SENDS, Fence, FenceError};
pub use markers::{
    Decision, DecisionKind, MAXIMUM_MAX_SENDS, MINIMUM_MAX_SENDS, Marker, State, decide_dispatch,
    validate_max_sends,
};
