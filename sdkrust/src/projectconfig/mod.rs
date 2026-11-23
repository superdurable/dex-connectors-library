// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Project configuration loading, versioned storage, and project credentials.
//!
//! An application loads its connections once at startup, either from Dex Web's local
//! `connections.json` (named by `DEX_CONNECTOR_CONFIG_FILE`) or from the hosted
//! `DEX_PROJECT_*` storage contract, then resolves credentials on every provider call. The
//! stored documents, keys, digests, and ETags match sdkgo byte for byte.
//!
//! ```no_run
//! use std::sync::Arc;
//! use dex_connectors_sdk::projectconfig::{
//!     ConnectionKey, RefreshingProjectCredentialProvider, load_from_environment,
//! };
//!
//! # fn main() -> Result<(), Box<dyn std::error::Error>> {
//! let project = load_from_environment()?;
//! let provider = RefreshingProjectCredentialProvider::new(
//!     project.connections().clone(),
//!     ConnectionKey::new("sample", "main"),
//!     Arc::new(|members: &serde_json::Map<String, serde_json::Value>| {
//!         members.get("accessToken").and_then(|token| token.as_str()).map(str::to_string)
//!             .ok_or_else(|| "accessToken is required".to_string())
//!     }),
//!     Arc::new(|token: &String| {
//!         let mut members = serde_json::Map::new();
//!         members.insert("accessToken".into(), serde_json::Value::String(token.clone()));
//!         Ok(members)
//!     }),
//!     None,
//! )?;
//! # let _ = provider;
//! # Ok(())
//! # }
//! ```
//!
//! Hosted storage needs an [`S3Client`]; this crate ships no AWS credential chain, so pass an
//! adapter over the application's AWS client to [`load_from_environment_values`].

mod broker;
mod connections;
mod dexweb_store;
mod errors;
mod models;
pub mod poll_cursor;
mod provider;
mod storage;
pub mod trigger_inbox;

use std::collections::HashMap;
use std::net::IpAddr;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::SystemTime;

use serde_json::Value;

pub use broker::{
    BrokerCredential, BrokerHttpClient, BrokerHttpResponse, BrokerReadError,
    CredentialBrokerClient, MAXIMUM_BROKER_CREDENTIAL_BYTES, MAXIMUM_BROKER_RESPONSE_BYTES,
    ScopeTokenReader, ScopeTokenReferenceReader, SharedConnection, UreqBrokerHttpClient,
    decode_broker_response,
};
pub use connections::{
    CONNECTION_SCHEMA_VERSION, Connection, ConnectionKey, ConnectionStore, CredentialMaterial,
    wire,
};
pub use dexweb_store::DexWebObjectStore;
pub use errors::{ProjectConfigError, ProjectConfigResult};
pub use models::{
    ApplicationSecretRef, CONFIGURATION_SCHEMA_VERSION, CREDENTIAL_BROKER_TOKEN_SECRET_NAME,
    Configuration, ConnectionConfiguration, CredentialBrokerConfiguration, EnvironmentValue,
    JsonObject, LOCAL_CONNECTIONS_SCHEMA_VERSION, LOCAL_USE_CONFIGURATIONS_SCHEMA_VERSION,
    PREVIOUS_CONFIGURATION_SCHEMA_VERSION, Scope, SnapshotRef, digest_bytes,
    go_canonical_json_bytes, is_application_environment_name, validate_broker_endpoint,
    validate_environment_value, validate_secret_reference,
};
pub use provider::{
    CredentialDecoder, CredentialEncoder, ProjectCredentialProvider,
    RefreshingProjectCredentialProvider,
};
pub use storage::{
    ConfigurationStore, InMemoryObjectStore, MAXIMUM_OBJECT_BYTES, ObjectStore, ProjectObject,
    S3Client, S3Error, S3GetObjectOutput, S3ObjectStore, S3PutObjectInput, S3PutObjectOutput,
    validate_object_key,
};

use crate::relaysignature::RelayHeaders;
use dexweb_store::read_private_file;
use models::require_object;

/// Environment variable naming Dex Web's local `connections.json`.
pub const LOCAL_CONNECTIONS_FILE_ENVIRONMENT: &str = "DEX_CONNECTOR_CONFIG_FILE";

const HOSTED_SELECTORS: [&str; 4] = [
    "DEX_PROJECT_ID",
    "DEX_PROJECT_SCOPE",
    "DEX_PROJECT_CONFIG_KEY",
    "DEX_PROJECT_STORAGE_BUCKET",
];

/// Pinned ordinary snapshot with independently resolved current credentials.
pub struct LoadedProject {
    pub(crate) configuration: Configuration,
    pub(crate) snapshot: SnapshotRef,
    pub(crate) connections: ConnectionStore,
    pub(crate) configurations: ConfigurationStore,
    pub(crate) broker: Option<Arc<CredentialBrokerClient>>,
    pub(crate) shared_connections: HashMap<ConnectionKey, Arc<SharedConnection>>,
}

impl LoadedProject {
    /// Returns the accepted configuration.
    pub fn configuration(&self) -> &Configuration {
        &self.configuration
    }

    /// Returns the pinned snapshot reference.
    pub fn snapshot(&self) -> &SnapshotRef {
        &self.snapshot
    }

    /// Returns the connection store used by credential providers.
    pub fn connections(&self) -> &ConnectionStore {
        &self.connections
    }

    /// Returns the configuration store.
    pub fn configurations(&self) -> &ConfigurationStore {
        &self.configurations
    }

    /// Returns the connection entry whose settings the application decodes once.
    pub fn connection_configuration(&self, key: &ConnectionKey) -> Option<&ConnectionConfiguration> {
        self.configuration.connections.iter().find(|entry| {
            entry.connector_id == key.connector_id && entry.connection_name == key.connection_name
        })
    }

    /// Resolves private material at call time without copying it into a Flow.
    ///
    /// # Errors
    ///
    /// Returns the storage or broker failure; a broker reauthorization answer is
    /// [`BrokerReadError::ReauthorizationRequired`].
    pub fn resolve_credential(
        &self,
        key: &ConnectionKey,
        call_id: &str,
    ) -> Result<JsonObject, BrokerReadError> {
        if let Some(shared) = self.shared_connections.get(key) {
            return shared.resolve(call_id);
        }
        let (material, _) = self.connections.read_credential_material(key)?;
        Ok(material.credentials)
    }

    /// Asks the broker for a newer revision after a shared credential rejection.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for a connection without a broker, or the
    /// broker failure.
    pub fn resolve_after_rejection(
        &self,
        key: &ConnectionKey,
        call_id: &str,
    ) -> Result<JsonObject, BrokerReadError> {
        let shared = self.shared_connections.get(key).ok_or_else(|| {
            ProjectConfigError::invalid("connection does not use a shared credential broker")
        })?;
        shared.resolve_after_rejection(call_id)
    }

    /// Loads ordinary values and the pinned secret versions of the application environment.
    ///
    /// # Errors
    ///
    /// Returns a storage or integrity error.
    pub fn resolve_application_environment(&self) -> ProjectConfigResult<HashMap<String, String>> {
        self.configuration
            .environment
            .iter()
            .map(|(name, value)| {
                let resolved = match value {
                    EnvironmentValue::Value(value) => value.clone(),
                    EnvironmentValue::SecretRef(reference) => {
                        self.configurations.read_secret_value(reference, name)?
                    }
                };
                Ok((name.clone(), resolved))
            })
            .collect()
    }

    /// Returns the shared connection ID of a configured connection, or `None` when it is not
    /// shared.
    pub fn shared_connection_id(&self, key: &ConnectionKey) -> Option<&str> {
        self.connection_configuration(key)
            .map(|entry| entry.shared_connection_id.as_str())
            .filter(|shared| !shared.is_empty())
    }

    /// Asks the credential broker to relay a shared connection's provider events here.
    ///
    /// `registration_path` is an absolute path on the broker endpoint's origin; connectors
    /// register again on every start.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] without a broker or shared entry, and the
    /// broker's errors otherwise.
    pub fn register_relayed_trigger(
        &self,
        key: &ConnectionKey,
        registration_path: &str,
    ) -> ProjectConfigResult<()> {
        let broker = self.broker.as_ref().ok_or_else(|| {
            ProjectConfigError::invalid(
                "relayed Trigger registration requires a credential broker in the project configuration",
            )
        })?;
        let shared_connection_id = self.shared_connection_id(key).ok_or_else(|| {
            ProjectConfigError::invalid(format!(
                "relayed Trigger registration requires a shared connection entry for {}/{}",
                key.connector_id, key.connection_name
            ))
        })?;
        broker.register_relayed_trigger(shared_connection_id, &key.connection_name, registration_path)
    }

    /// Checks that the host signed a relayed request with this scope's token.
    ///
    /// # Errors
    ///
    /// See [`CredentialBrokerClient::verify_relayed_request`]; returns
    /// [`ProjectConfigError::Invalid`] without a broker.
    pub fn verify_relayed_request(
        &self,
        headers: &RelayHeaders,
        body: &[u8],
        received_at: SystemTime,
    ) -> ProjectConfigResult<()> {
        let broker = self.broker.as_ref().ok_or_else(|| {
            ProjectConfigError::invalid(
                "relayed Trigger verification requires a credential broker in the project configuration",
            )
        })?;
        broker.verify_relayed_request(headers, body, received_at)
    }
}

/// Reads a pinned snapshot from a trusted object store without fetching credentials.
///
/// `broker_http` defaults to [`UreqBrokerHttpClient`]. `connection_store` replaces the
/// ordinary connection store and cannot bind shared connections.
///
/// # Errors
///
/// Returns a storage, integrity, or validation error.
pub fn load_project_from_object_store(
    objects: Arc<dyn ObjectStore>,
    scope: Scope,
    reference: SnapshotRef,
    broker_http: Option<Arc<dyn BrokerHttpClient>>,
    connection_store: Option<ConnectionStore>,
) -> ProjectConfigResult<LoadedProject> {
    let configurations = ConfigurationStore::new(objects.clone(), scope.clone())?;
    let configuration = configurations.read_snapshot(&reference)?;
    let has_shared = configuration
        .connections
        .iter()
        .any(|entry| !entry.shared_connection_id.is_empty());
    if connection_store.is_some() && has_shared {
        return Err(ProjectConfigError::invalid(
            "an externally supplied connection store cannot bind shared connections",
        ));
    }
    let mut broker = None;
    let mut shared_connections = HashMap::new();
    if let Some(broker_configuration) = &configuration.credential_broker {
        let token_store = configurations.clone();
        let reference_store = configurations.clone();
        let client = Arc::new(CredentialBrokerClient::new(
            broker_configuration,
            Arc::new(move |token_reference: &ApplicationSecretRef| {
                token_store.read_secret_value(token_reference, CREDENTIAL_BROKER_TOKEN_SECRET_NAME)
            }),
            Arc::new(move || {
                let (current, _) = reference_store.read_configuration()?;
                current
                    .credential_broker
                    .map(|broker| broker.token_secret_ref)
                    .ok_or_else(|| {
                        ProjectConfigError::invalid("credential broker token reference is absent")
                    })
            }),
            broker_http.unwrap_or_else(|| Arc::new(UreqBrokerHttpClient::new())),
        )?);
        for entry in &configuration.connections {
            if !entry.shared_connection_id.is_empty() {
                shared_connections.insert(
                    ConnectionKey::new(entry.connector_id.clone(), entry.connection_name.clone()),
                    Arc::new(SharedConnection::new(&entry.shared_connection_id, client.clone())?),
                );
            }
        }
        broker = Some(client);
    }
    let connections = connection_store.unwrap_or_else(|| {
        ConnectionStore::with_shared_connections(objects, scope, shared_connections.clone())
    });
    Ok(LoadedProject {
        configuration,
        snapshot: reference,
        connections,
        configurations,
        broker,
        shared_connections,
    })
}

const LOCAL_CONNECTION_FIELDS: [&str; 7] = [
    "connectorId",
    "connectionName",
    "modulePath",
    "provider",
    "configuration",
    "authMethodId",
    "sharedConnectionId",
];

/// Loads Dex Web's `connections.json` and its `use-configurations.json` sidecar.
///
/// Both files must be owner-only (0600) regular files. Credentials Dex Web saves later are
/// adopted on the next read, and refreshed credentials are written back.
///
/// # Errors
///
/// Returns an error for a loose, missing, or invalid file.
pub fn load_dex_web_project(path: &Path) -> ProjectConfigResult<LoadedProject> {
    let connections_path = std::path::absolute(path)
        .map_err(|_| ProjectConfigError::Io("connections.json path is invalid".into()))?;
    let source = read_private_file(&connections_path)?;
    if source.get("schemaVersion") != Some(&Value::String(LOCAL_CONNECTIONS_SCHEMA_VERSION.into()))
    {
        return Err(ProjectConfigError::invalid(
            "unsupported connector configuration schema version",
        ));
    }
    let uses_path = connections_path.with_file_name("use-configurations.json");
    let uses = if uses_path.exists() {
        let uses = read_private_file(&uses_path)?;
        if uses.get("schemaVersion")
            != Some(&Value::String(LOCAL_USE_CONFIGURATIONS_SCHEMA_VERSION.into()))
        {
            return Err(ProjectConfigError::invalid(
                "unsupported connector use configuration schema version",
            ));
        }
        uses
    } else {
        JsonObject::new()
    };
    let raw_connections = source
        .get("connections")
        .and_then(Value::as_array)
        .ok_or_else(|| ProjectConfigError::invalid("connections.json requires connections"))?;
    let mut entries = Vec::with_capacity(raw_connections.len());
    for (index, raw) in raw_connections.iter().enumerate() {
        let record = require_object(Some(raw), &format!("connection {index}"))?;
        let selected: JsonObject = record
            .iter()
            .filter(|(key, _)| LOCAL_CONNECTION_FIELDS.contains(&key.as_str()))
            .map(|(key, value)| (key.clone(), value.clone()))
            .collect();
        let entry = ConnectionConfiguration::from_wire(&Value::Object(selected))?;
        if entry.connector_id.is_empty()
            || entry.connection_name.is_empty()
            || entry.module_path.is_empty()
            || entry.provider.is_empty()
        {
            return Err(ProjectConfigError::invalid(
                "local connection identity is incomplete",
            ));
        }
        entries.push(entry);
    }
    let objects_of = |document: &JsonObject, name: &str, label: &str| {
        document
            .get(name)
            .and_then(Value::as_array)
            .map_or_else(Vec::new, Clone::clone)
            .iter()
            .map(|item| require_object(Some(item), label).cloned())
            .collect::<ProjectConfigResult<Vec<_>>>()
    };
    let scope = Scope::new("local", "preview");
    let store: Arc<dyn ObjectStore> = Arc::new(InMemoryObjectStore::new());
    let configurations = ConfigurationStore::new(store.clone(), scope.clone())?;
    let mut document = Configuration::new(scope.clone(), 1, entries);
    document.trigger_bindings = objects_of(&source, "triggerBindings", "trigger binding")?;
    document.operation_configurations =
        objects_of(&uses, "operationConfigurations", "operation configuration")?;
    let (written, _) = configurations.write_configuration(0, &document)?;
    let reference = configurations.freeze_configuration(written.revision)?;
    let local_objects: Arc<dyn ObjectStore> =
        Arc::new(DexWebObjectStore::open(&connections_path, scope.clone())?);
    load_project_from_object_store(
        store,
        scope.clone(),
        reference,
        None,
        Some(ConnectionStore::new(local_objects, scope)),
    )
}

/// Loads the project selected by the process environment.
///
/// See [`load_from_environment_values`]; hosted storage fails here because no S3 client is
/// supplied.
///
/// # Errors
///
/// Returns the loader error.
pub fn load_from_environment() -> ProjectConfigResult<LoadedProject> {
    let values: HashMap<String, String> = std::env::vars().collect();
    load_from_environment_values(&values, None)
}

/// Loads either Dex Web local files or the trusted `DEX_PROJECT_*` hosted storage contract.
///
/// `DEX_CONNECTOR_CONFIG_FILE` selects Dex Web and must not be combined with hosted
/// selectors. Hosted mode reads `DEX_PROJECT_ID`, `DEX_PROJECT_SCOPE`,
/// `DEX_PROJECT_STORAGE_BUCKET`, optional `DEX_PROJECT_STORAGE_PREFIX` and
/// `DEX_PROJECT_STORAGE_KMS_KEY_ARN`, and the pinned `DEX_PROJECT_CONFIG_KEY`,
/// `DEX_PROJECT_CONFIG_VERSION`, and `DEX_PROJECT_CONFIG_DIGEST`; a custom
/// `DEX_PROJECT_STORAGE_ENDPOINT` requires `DEX_PROJECT_ALLOW_LOCAL_STORAGE=true` and a
/// loopback, private, or cluster-local host. The endpoint is the caller's to apply when it
/// builds `s3_client`.
///
/// # Errors
///
/// Returns [`ProjectConfigError::Invalid`] for conflicting or unsafe selectors, or for
/// hosted mode without `s3_client`, and the loader error otherwise.
pub fn load_from_environment_values(
    values: &HashMap<String, String>,
    s3_client: Option<Arc<dyn S3Client>>,
) -> ProjectConfigResult<LoadedProject> {
    let value = |name: &str| values.get(name).map_or("", String::as_str);
    let local_path = value(LOCAL_CONNECTIONS_FILE_ENVIRONMENT);
    if !local_path.is_empty() {
        if HOSTED_SELECTORS.iter().any(|name| !value(name).is_empty()) {
            return Err(ProjectConfigError::invalid(
                "local and hosted project configuration selectors conflict",
            ));
        }
        return load_dex_web_project(&PathBuf::from(local_path));
    }
    let scope = Scope::new(value("DEX_PROJECT_ID"), value("DEX_PROJECT_SCOPE"));
    scope.prefix()?;
    let allow_local = values
        .get("DEX_PROJECT_ALLOW_LOCAL_STORAGE")
        .map_or("false", String::as_str);
    if !matches!(allow_local, "true" | "false") {
        return Err(ProjectConfigError::invalid(
            "project local storage opt-in must be true or false",
        ));
    }
    let endpoint = value("DEX_PROJECT_STORAGE_ENDPOINT");
    if !endpoint.is_empty() && (allow_local != "true" || !is_local_storage_endpoint(endpoint)) {
        return Err(ProjectConfigError::invalid(
            "custom project storage endpoint requires explicit local opt-in",
        ));
    }
    if allow_local == "true" && endpoint.is_empty() {
        return Err(ProjectConfigError::invalid(
            "local project storage requires an endpoint",
        ));
    }
    let bucket = value("DEX_PROJECT_STORAGE_BUCKET");
    if bucket.is_empty() {
        return Err(ProjectConfigError::invalid("project storage bucket is required"));
    }
    let s3_client = s3_client.ok_or_else(|| {
        ProjectConfigError::invalid(
            "hosted project storage requires an S3 client; pass one to load_from_environment_values",
        )
    })?;
    let objects: Arc<dyn ObjectStore> = Arc::new(S3ObjectStore::create(
        s3_client,
        bucket,
        value("DEX_PROJECT_STORAGE_PREFIX"),
        value("DEX_PROJECT_STORAGE_KMS_KEY_ARN"),
        allow_local == "true",
    )?);
    let reference = SnapshotRef::new(
        value("DEX_PROJECT_CONFIG_KEY"),
        value("DEX_PROJECT_CONFIG_VERSION"),
        value("DEX_PROJECT_CONFIG_DIGEST"),
    );
    load_project_from_object_store(objects, scope, reference, None, None)
}

/// Allows only an explicitly isolated local or cluster fixture endpoint.
fn is_local_storage_endpoint(value: &str) -> bool {
    let Some(parsed) = models::parse_url(value) else {
        return false;
    };
    if !matches!(parsed.scheme, "http" | "https")
        || parsed.host.is_empty()
        || parsed.has_user_info
        || parsed.has_query_or_fragment
        || !matches!(parsed.path, "" | "/")
    {
        return false;
    }
    let host = parsed.host.as_str();
    if matches!(host, "localhost" | "host.docker.internal") || host.ends_with(".svc.cluster.local")
    {
        return true;
    }
    host.parse::<IpAddr>().is_ok_and(|address| match address {
        IpAddr::V4(v4) => v4.is_loopback() || v4.is_private(),
        IpAddr::V6(v6) => v6.is_loopback() || (v6.segments()[0] & 0xfe00) == 0xfc00,
    })
}
