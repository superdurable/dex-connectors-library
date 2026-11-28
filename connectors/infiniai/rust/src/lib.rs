// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Read the InfiniAI model catalog from a Dex Query Step.

#[rustfmt::skip]
mod generated_definitions;
mod model_response;
#[rustfmt::skip]
mod generated_model_catalog;
pub use generated_model_catalog::*;
pub use model_response::ModelInfo;
