// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Connector-owned Dex Web configuration presentation metadata.

use std::collections::BTreeSet;

use serde::{Deserialize, Serialize};

use crate::errors::ValidationError;
use crate::types::{OperationRef, is_lower_camel_identifier};

fn is_lower_camel_or_single(value: &str) -> bool {
    value.len() == 1 && value.as_bytes()[0].is_ascii_lowercase() || is_lower_camel_identifier(value)
}

/// Maps one released UI unit port to an application JSON Pointer.
#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ConnectorUiBinding {
    /// Lower-camel unit port.
    pub port: String,
    /// Nonempty RFC 6901 JSON Pointer into the application configuration.
    pub json_pointer: String,
}

impl ConnectorUiBinding {
    /// Requires a lower-camel port and a valid nonempty RFC 6901 pointer.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid port or pointer escape.
    pub fn validate(&self) -> Result<(), ValidationError> {
        if !is_lower_camel_or_single(&self.port) || !self.json_pointer.starts_with('/') {
            return Err(ValidationError::new("invalid Connector UI binding"));
        }
        let bytes = self.json_pointer.as_bytes();
        let mut index = 0;
        while index < bytes.len() {
            if bytes[index] == b'~' {
                if !matches!(bytes.get(index + 1), Some(b'0') | Some(b'1')) {
                    return Err(ValidationError::new(
                        "invalid Connector UI JSON Pointer escape",
                    ));
                }
                index += 1;
            }
            index += 1;
        }
        Ok(())
    }
}

/// One reusable catalog unit in application composition order.
#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ConnectorUiUnit {
    /// Lower-camel use ID, unique within the configuration.
    pub id: String,
    /// Lower-camel released unit ID.
    pub unit_id: String,
    /// Field label.
    pub label: String,
    /// Instructional description of the choice in this operation or Trigger.
    pub description: String,
    /// Whether the application must supply the value.
    pub required: bool,
    /// Port bindings; at least one.
    pub bindings: Vec<ConnectorUiBinding>,
}

impl ConnectorUiUnit {
    /// Checks static unit guidance and unique ports.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid identity, missing guidance, or duplicate port.
    pub fn validate(&self) -> Result<(), ValidationError> {
        if !is_lower_camel_or_single(&self.id) || !is_lower_camel_or_single(&self.unit_id) {
            return Err(ValidationError::new(
                "Connector UI unit IDs must be lower camel case",
            ));
        }
        if self.label.trim().is_empty()
            || self.description.trim().is_empty()
            || self.bindings.is_empty()
        {
            return Err(ValidationError::new(
                "Connector UI unit needs a label, description, and bindings",
            ));
        }
        let mut ports = BTreeSet::new();
        for binding in &self.bindings {
            if !ports.insert(binding.port.as_str()) {
                return Err(ValidationError::new(
                    "Connector UI unit ports must be unique",
                ));
            }
            binding.validate()?;
        }
        Ok(())
    }
}

/// Ordered connector units shown for a Step or Trigger binding.
#[derive(Clone, Debug, Default, Deserialize, Eq, PartialEq, Serialize)]
pub struct ConnectorConfigurationUi {
    /// Units in composition order.
    pub units: Vec<ConnectorUiUnit>,
}

impl ConnectorConfigurationUi {
    /// Checks all units and prevents duplicate use IDs.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid or duplicate unit.
    pub fn validate(&self) -> Result<(), ValidationError> {
        let mut ids = BTreeSet::new();
        for unit in &self.units {
            if !ids.insert(unit.id.as_str()) {
                return Err(ValidationError::new("Connector UI unit IDs must be unique"));
            }
            unit.validate()?;
        }
        Ok(())
    }
}

/// Stable Flow and Step use that owns an operation configuration.
#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ConnectorConfigurationRef {
    /// Connector ID.
    pub connector_id: String,
    /// Static connection name.
    pub connection_name: String,
    /// Operation ID.
    pub operation_id: String,
    /// Flow type that owns the Step.
    pub flow_type: String,
    /// Step type that calls the operation.
    pub step_type: String,
}

impl ConnectorConfigurationRef {
    /// Requires stable connector, operation, connection, Flow, and Step identities.
    ///
    /// # Errors
    ///
    /// Returns [`ValidationError`] for an invalid or blank identity.
    pub fn validate(&self) -> Result<(), ValidationError> {
        OperationRef::new(self.connector_id.clone(), self.operation_id.clone())
            .validate()
            .map_err(|_| ValidationError::new("invalid Connector configuration identity"))?;
        if self.connection_name.trim().is_empty()
            || self.flow_type.trim().is_empty()
            || self.step_type.trim().is_empty()
        {
            return Err(ValidationError::new(
                "Connector configuration connection, Flow, and Step are required",
            ));
        }
        Ok(())
    }
}
