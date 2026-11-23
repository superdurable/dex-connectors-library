// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Project Trigger inboxes and the durable Trigger target, byte-compatible with Go, Python, and
//! TypeScript.

use std::collections::HashMap;
use std::fmt;
use std::sync::{Arc, Mutex, PoisonError};

use base64::Engine;
use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use serde::de::{DeserializeOwned, IgnoredAny, MapAccess, Visitor};
use serde::{Deserialize, Deserializer, Serialize};
use serde_json::value::RawValue;

use super::errors::ProjectConfigError;
use super::models::{Configuration, Scope};
use super::storage::ObjectStore;
use crate::go_json::to_go_json_vec;
use crate::timestamps::Rfc3339NanoTimestamp;
use crate::trigger::{
    TriggerContext, TriggerError, TriggerEvent, TriggerLogAttributes, TriggerLogLevel,
    TriggerLogValue, TriggerLogger, TriggerTarget, bind_trigger_log_attributes, deliver_trigger,
    is_trigger_undeliverable, log_trigger_record, trigger_error_attributes,
};

/// Schema of one binding's inbox object, shared with the Go, Python, and TypeScript SDKs.
pub const TRIGGER_INBOX_SCHEMA_VERSION: &str = "connectors.dex.dev/project-trigger-inbox/v1alpha1";

/// One binding's pending backlog bound; a source stops acknowledging new events beyond it.
pub const MAXIMUM_PENDING_TRIGGER_EVENTS: usize = 1000;

/// One inbox object's size bound in bytes; S3 project storage still refuses objects over 4 MiB.
pub const MAXIMUM_TRIGGER_INBOX_BYTES: usize = 8 << 20;

const MAXIMUM_INBOX_WRITE_ATTEMPTS: usize = 8;
const MAXIMUM_INBOX_NAME_BYTES: usize = 256;

/// A binding's backlog reached its bound; the source retries once the application consumes
/// events.
#[derive(Debug, Default)]
pub struct TriggerInboxFullError;

impl fmt::Display for TriggerInboxFullError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("project trigger inbox is full")
    }
}

impl std::error::Error for TriggerInboxFullError {}

fn is_go_space(character: char) -> bool {
    // Go's unicode.IsSpace: ASCII whitespace, U+0085, U+00A0, and the Unicode Z categories.
    matches!(
        character,
        '\t' | '\n' | '\u{b}' | '\u{c}' | '\r' | ' ' | '\u{85}' | '\u{a0}'
    ) || matches!(
        character,
        '\u{1680}' | '\u{2000}'..='\u{200a}' | '\u{2028}' | '\u{2029}' | '\u{202f}' | '\u{205f}' | '\u{3000}'
    )
}

pub(crate) fn is_valid_inbox_name(name: &str) -> bool {
    let Some(first) = name.chars().next() else {
        return false;
    };
    let last = name.chars().last().unwrap_or(first);
    name.len() <= MAXIMUM_INBOX_NAME_BYTES
        && !name.contains(['\0', '\r', '\n'])
        && !is_go_space(first)
        && !is_go_space(last)
}

fn is_inbox_identifier(value: &str, is_connector: bool) -> bool {
    let bytes = value.as_bytes();
    (2..=63).contains(&bytes.len())
        && bytes[0].is_ascii_lowercase()
        && bytes[1..].iter().all(|byte| {
            if is_connector {
                byte.is_ascii_lowercase() || byte.is_ascii_digit() || *byte == b'-'
            } else {
                byte.is_ascii_alphanumeric()
            }
        })
}

pub(crate) fn base64_url(value: &str) -> String {
    URL_SAFE_NO_PAD.encode(value.as_bytes())
}

/// One application binding of a connector Trigger.
#[derive(Clone, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
pub struct TriggerInboxKey {
    /// Manifest connector ID.
    pub connector_id: String,
    /// Application-declared connection the Trigger reads.
    pub connection_name: String,
    /// Manifest Trigger name.
    pub trigger_name: String,
    /// Application binding of that Trigger.
    pub binding_name: String,
}

impl TriggerInboxKey {
    /// Creates a key.
    pub fn new(
        connector_id: impl Into<String>,
        connection_name: impl Into<String>,
        trigger_name: impl Into<String>,
        binding_name: impl Into<String>,
    ) -> Self {
        Self {
            connector_id: connector_id.into(),
            connection_name: connection_name.into(),
            trigger_name: trigger_name.into(),
            binding_name: binding_name.into(),
        }
    }

    /// Requires Go-valid identities: names of at most 256 UTF-8 bytes without edge spaces or
    /// line breaks.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for an invalid identity.
    pub fn validate(&self) -> Result<(), ProjectConfigError> {
        if !is_inbox_identifier(&self.connector_id, true)
            || !is_inbox_identifier(&self.trigger_name, false)
        {
            return Err(ProjectConfigError::Invalid(
                "trigger inbox connector or trigger identity is invalid".to_string(),
            ));
        }
        if !is_valid_inbox_name(&self.connection_name) || !is_valid_inbox_name(&self.binding_name)
        {
            return Err(ProjectConfigError::Invalid(
                "trigger inbox connection or binding name is invalid".to_string(),
            ));
        }
        Ok(())
    }

    /// Returns the inbox head object key in `scope`, as Go and TypeScript write it.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for an invalid identity or scope.
    pub fn path(&self, scope: &Scope) -> Result<String, ProjectConfigError> {
        self.validate()?;
        Ok(format!(
            "{}/trigger-inboxes/{}/{}/{}/{}/head",
            scope.prefix()?,
            self.connector_id,
            base64_url(&self.connection_name),
            self.trigger_name,
            base64_url(&self.binding_name)
        ))
    }
}

/// One acknowledged Trigger event that the application has not yet consumed.
#[derive(Clone, Eq, PartialEq)]
pub struct PendingTriggerEvent {
    /// The source's stable event identity.
    pub id: String,
    /// The complete encoded event as raw JSON, kept exactly as stored.
    pub event: Vec<u8>,
}

impl fmt::Debug for PendingTriggerEvent {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("PendingTriggerEvent")
            .field("id", &self.id)
            .field("event_bytes", &self.event.len())
            .finish()
    }
}

/// Stores acknowledged Trigger events in project storage until the application consumes them.
///
/// A restarted or replaced replica delivers them in order. Writes use compare-and-swap against
/// concurrent replicas. The object key and document match the Go, Python, and TypeScript inboxes
/// byte for byte.
pub struct TriggerInbox {
    objects: Arc<dyn ObjectStore>,
    identity: TriggerInboxKey,
    key: String,
    has_head: Mutex<bool>,
}

impl fmt::Debug for TriggerInbox {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("TriggerInbox")
            .field("key", &self.key)
            .finish()
    }
}

impl TriggerInbox {
    /// Binds one binding's inbox in `scope` without I/O.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for an invalid identity or scope.
    pub fn new(
        objects: Arc<dyn ObjectStore>,
        scope: &Scope,
        key: TriggerInboxKey,
    ) -> Result<Self, ProjectConfigError> {
        let path = key.path(scope)?;
        Ok(Self {
            objects,
            identity: key,
            key: path,
            has_head: Mutex::new(false),
        })
    }

    /// The inbox head object key, for diagnostics and fixtures.
    pub fn key(&self) -> &str {
        &self.key
    }

    /// The four-part binding identity this inbox belongs to.
    pub fn identity(&self) -> &TriggerInboxKey {
        &self.identity
    }

    /// Returns the pending events in acknowledgement order.
    ///
    /// # Errors
    ///
    /// Returns a storage error or an invalid document error.
    pub fn pending(&self) -> Result<Vec<PendingTriggerEvent>, TriggerError> {
        Ok(self.read()?.0)
    }

    /// Appends an event once; an ID already pending is unchanged.
    ///
    /// # Errors
    ///
    /// Returns [`ProjectConfigError::Invalid`] for a blank ID or invalid JSON,
    /// [`TriggerInboxFullError`] beyond 1000 events or 8 MiB, or a storage error.
    pub fn add(&self, event: PendingTriggerEvent) -> Result<(), TriggerError> {
        if event.id.trim().is_empty() || compact_go_raw_json(&event.event).is_err() {
            return Err(Box::new(ProjectConfigError::Invalid(
                "trigger inbox event identity or contents are invalid".to_string(),
            )));
        }
        self.update(|events| {
            if events.iter().any(|pending| pending.id == event.id) {
                return Ok(events);
            }
            if events.len() >= MAXIMUM_PENDING_TRIGGER_EVENTS {
                return Err(Box::new(TriggerInboxFullError));
            }
            let mut changed = events;
            changed.push(event.clone());
            Ok(changed)
        })
    }

    /// Deletes a consumed event; removing an absent event succeeds.
    ///
    /// # Errors
    ///
    /// Returns a storage error.
    pub fn remove(&self, event_id: &str) -> Result<(), TriggerError> {
        self.update(|events| {
            Ok(events
                .into_iter()
                .filter(|pending| pending.id != event_id)
                .collect())
        })
    }

    fn update(
        &self,
        change: impl Fn(Vec<PendingTriggerEvent>) -> Result<Vec<PendingTriggerEvent>, TriggerError>,
    ) -> Result<(), TriggerError> {
        for _ in 0..MAXIMUM_INBOX_WRITE_ATTEMPTS {
            let (current, etag) = self.read()?;
            let current_ids: Vec<String> = current.iter().map(|event| event.id.clone()).collect();
            let changed = change(current)?;
            if changed.iter().map(|event| &event.id).eq(current_ids.iter()) {
                return Ok(());
            }
            let contents = encode_inbox_document(&changed)?;
            if contents.len() > MAXIMUM_TRIGGER_INBOX_BYTES {
                return Err(Box::new(TriggerInboxFullError));
            }
            let written = if etag.is_empty() {
                self.objects.create_object(&self.key, &contents)
            } else {
                self.objects
                    .compare_and_swap_object(&self.key, &etag, &contents)
            };
            match written {
                Ok(_) => return Ok(()),
                Err(ProjectConfigError::OutcomeUnknown(_)) => {
                    // An unreadable head is retried like a conflict.
                    if let Ok(observed) = self.objects.read_object(&self.key, "")
                        && observed.contents == contents
                    {
                        return Ok(());
                    }
                }
                Err(ProjectConfigError::ObjectConflict(_)) => {}
                Err(error) => return Err(Box::new(error)),
            }
        }
        Err(Box::new(ProjectConfigError::ObjectConflict(
            "project trigger inbox compare-and-swap conflict".to_string(),
        )))
    }

    fn read(&self) -> Result<(Vec<PendingTriggerEvent>, String), TriggerError> {
        self.create_head();
        let stored = match self.objects.read_object(&self.key, "") {
            Ok(stored) => stored,
            Err(ProjectConfigError::ObjectNotFound) => return Ok((Vec::new(), String::new())),
            Err(error) => return Err(Box::new(error)),
        };
        if stored.etag.is_empty() {
            return Err(invalid_document());
        }
        Ok((decode_inbox_document(&stored.contents)?, stored.etag))
    }

    fn create_head(&self) {
        // S3 refuses reading a missing key like a permission error, so the head is created first.
        let mut has_head = self.has_head.lock().unwrap_or_else(PoisonError::into_inner);
        if *has_head {
            return;
        }
        let Ok(empty) = encode_inbox_document(&[]) else {
            return;
        };
        match self.objects.create_object(&self.key, &empty) {
            Ok(_) | Err(ProjectConfigError::ObjectConflict(_)) => *has_head = true,
            // An unknown outcome or other failure is retried on the next read.
            Err(_) => {}
        }
    }
}

fn invalid_document() -> TriggerError {
    Box::new(ProjectConfigError::Invalid(
        "trigger inbox document is invalid".to_string(),
    ))
}

/// Compacts raw JSON as Go's `json.Marshal` writes a `json.RawMessage`: no insignificant
/// whitespace, member order kept, and `<`, `>`, `&`, U+2028, and U+2029 escaped inside strings.
///
/// # Errors
///
/// Returns [`ProjectConfigError::Invalid`] when `raw` is not one valid JSON value.
pub fn compact_go_raw_json(raw: &[u8]) -> Result<Vec<u8>, ProjectConfigError> {
    serde_json::from_slice::<IgnoredAny>(raw)
        .map_err(|_| ProjectConfigError::Invalid("invalid raw JSON".to_string()))?;
    let mut output = Vec::with_capacity(raw.len());
    let mut is_in_string = false;
    let mut is_escaped = false;
    let mut index = 0;
    while index < raw.len() {
        let byte = raw[index];
        if is_in_string {
            if is_escaped {
                is_escaped = false;
                output.push(byte);
            } else if byte == b'\\' {
                is_escaped = true;
                output.push(byte);
            } else if byte == b'"' {
                is_in_string = false;
                output.push(byte);
            } else if byte == b'<' {
                output.extend_from_slice(b"\\u003c");
            } else if byte == b'>' {
                output.extend_from_slice(b"\\u003e");
            } else if byte == b'&' {
                output.extend_from_slice(b"\\u0026");
            } else if byte == 0xE2
                && raw.get(index + 1) == Some(&0x80)
                && matches!(raw.get(index + 2), Some(0xA8 | 0xA9))
            {
                output.extend_from_slice(if raw[index + 2] == 0xA8 {
                    b"\\u2028"
                } else {
                    b"\\u2029"
                });
                index += 2;
            } else {
                output.push(byte);
            }
        } else if byte == b'"' {
            is_in_string = true;
            output.push(byte);
        } else if !matches!(byte, b' ' | b'\t' | b'\n' | b'\r') {
            output.push(byte);
        }
        index += 1;
    }
    Ok(output)
}

fn encode_go_string(value: &str) -> Result<Vec<u8>, TriggerError> {
    Ok(to_go_json_vec(value)?)
}

/// Encodes the inbox document as Go marshals it: field order, compact, HTML-safe, raw events
/// compacted.
fn encode_inbox_document(events: &[PendingTriggerEvent]) -> Result<Vec<u8>, TriggerError> {
    let mut contents = b"{\"schemaVersion\":".to_vec();
    contents.extend(encode_go_string(TRIGGER_INBOX_SCHEMA_VERSION)?);
    contents.extend_from_slice(b",\"events\":[");
    for (index, event) in events.iter().enumerate() {
        if index > 0 {
            contents.push(b',');
        }
        contents.extend_from_slice(b"{\"id\":");
        contents.extend(encode_go_string(&event.id)?);
        contents.extend_from_slice(b",\"event\":");
        contents.extend(compact_go_raw_json(&event.event)?);
        contents.push(b'}');
    }
    contents.extend_from_slice(b"]}");
    Ok(contents)
}

/// JSON object members in document order with raw values.
struct OrderedMembers(Vec<(String, Box<RawValue>)>);

impl<'de> Deserialize<'de> for OrderedMembers {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        struct MembersVisitor;

        impl<'de> Visitor<'de> for MembersVisitor {
            type Value = OrderedMembers;

            fn expecting(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
                formatter.write_str("a JSON object")
            }

            fn visit_map<A: MapAccess<'de>>(self, mut map: A) -> Result<OrderedMembers, A::Error> {
                let mut members = Vec::new();
                while let Some((key, value)) = map.next_entry::<String, Box<RawValue>>()? {
                    members.push((key, value));
                }
                Ok(OrderedMembers(members))
            }
        }

        deserializer.deserialize_map(MembersVisitor)
    }
}

fn is_go_field_key(key: &str, field: &str) -> bool {
    key.eq_ignore_ascii_case(field)
}

/// Decodes a string field as Go does into a struct: `null` keeps the previous value.
fn decode_go_string_field(raw: &RawValue, previous: String) -> Result<String, TriggerError> {
    if raw.get() == "null" {
        return Ok(previous);
    }
    Ok(serde_json::from_str::<String>(raw.get())?)
}

/// Decodes an inbox document strictly, as Go's `DisallowUnknownFields` does.
fn decode_inbox_document(contents: &[u8]) -> Result<Vec<PendingTriggerEvent>, TriggerError> {
    let decode = || -> Result<Vec<PendingTriggerEvent>, TriggerError> {
        let members: OrderedMembers = serde_json::from_slice(contents)?;
        let mut schema_version = String::new();
        let mut events = Vec::new();
        for (key, value) in members.0 {
            if is_go_field_key(&key, "schemaVersion") {
                schema_version = decode_go_string_field(&value, schema_version)?;
                continue;
            }
            if !is_go_field_key(&key, "events") {
                return Err(invalid_document());
            }
            if value.get() == "null" {
                events = Vec::new();
                continue;
            }
            let elements: Vec<Box<RawValue>> = serde_json::from_str(value.get())?;
            events = elements
                .iter()
                .map(|element| decode_pending_event(element))
                .collect::<Result<_, _>>()?;
        }
        if schema_version != TRIGGER_INBOX_SCHEMA_VERSION {
            return Err(invalid_document());
        }
        Ok(events)
    };
    decode().map_err(|_| invalid_document())
}

fn decode_pending_event(element: &RawValue) -> Result<PendingTriggerEvent, TriggerError> {
    let members: OrderedMembers = serde_json::from_str(element.get())?;
    let mut id = String::new();
    let mut event = b"null".to_vec();
    for (key, value) in members.0 {
        if is_go_field_key(&key, "id") {
            id = decode_go_string_field(&value, id)?;
        } else if is_go_field_key(&key, "event") {
            event = compact_go_raw_json(value.get().as_bytes())?;
        } else {
            return Err(invalid_document());
        }
    }
    Ok(PendingTriggerEvent { id, event })
}

/// Encodes an event in Go's form `{"eventId","occurredAt","payload"}`.
///
/// The payload is encoded with Go `encoding/json` rules through [`to_go_json_vec`]; serialize
/// maps as `BTreeMap` so keys sort as Go sorts them.
///
/// # Errors
///
/// Returns an error when the payload cannot be serialized.
pub fn encode_trigger_event<Payload: Serialize>(
    event: &TriggerEvent<Payload>,
) -> Result<Vec<u8>, TriggerError> {
    let mut encoded = b"{\"eventId\":".to_vec();
    encoded.extend(encode_go_string(&event.id)?);
    encoded.extend_from_slice(b",\"occurredAt\":");
    encoded.extend(encode_go_string(event.occurred_at.as_wire_str())?);
    encoded.extend_from_slice(b",\"payload\":");
    encoded.extend(to_go_json_vec(&event.payload)?);
    encoded.push(b'}');
    Ok(encoded)
}

/// Decodes an event that [`encode_trigger_event`] or another SDK wrote, as Go's
/// `json.Unmarshal` into a struct.
///
/// Unknown fields are ignored, keys match case-insensitively, the last duplicate wins, and a
/// missing payload is null.
///
/// # Errors
///
/// Returns an error for a non-object, an invalid time, or a payload `Payload` cannot decode.
pub fn decode_trigger_event<Payload: DeserializeOwned>(
    raw: &[u8],
) -> Result<TriggerEvent<Payload>, TriggerError> {
    let not_object = || -> TriggerError {
        Box::new(ProjectConfigError::Invalid(
            "pending Trigger event is not a JSON object".to_string(),
        ))
    };
    let members: OrderedMembers = serde_json::from_slice(raw).map_err(|_| not_object())?;
    let mut event_id = String::new();
    let mut occurred_at = String::new();
    let mut payload: Option<Box<RawValue>> = None;
    for (key, value) in members.0 {
        if is_go_field_key(&key, "eventId") {
            event_id = decode_go_string_field(&value, event_id).map_err(|_| not_object())?;
        } else if is_go_field_key(&key, "occurredAt") {
            occurred_at = decode_go_string_field(&value, occurred_at).map_err(|_| not_object())?;
        } else if is_go_field_key(&key, "payload") {
            payload = Some(value);
        }
    }
    let occurred_at = Rfc3339NanoTimestamp::from_wire(&occurred_at).map_err(|_| {
        Box::new(ProjectConfigError::Invalid(
            "pending Trigger event time is invalid".to_string(),
        )) as TriggerError
    })?;
    let payload = match payload {
        Some(raw_payload) => serde_json::from_str(raw_payload.get())?,
        None => serde_json::from_str("null")?,
    };
    Ok(TriggerEvent {
        id: event_id,
        occurred_at,
        payload,
    })
}

/// Decodes one Trigger binding's configuration with serde, rejecting unknown fields when the
/// target type declares `deny_unknown_fields`.
///
/// The binding is the `triggerBindings` entry whose four identity fields equal `key`.
///
/// # Errors
///
/// Returns [`ProjectConfigError::ObjectNotFound`] when the project has no such binding,
/// [`ProjectConfigError::Invalid`] when its configuration is not an object or does not decode.
pub fn decode_trigger_configuration<Settings: DeserializeOwned>(
    configuration: &Configuration,
    key: &TriggerInboxKey,
) -> Result<Settings, ProjectConfigError> {
    let field_equals = |entry: &serde_json::Map<String, serde_json::Value>, name: &str, wanted: &str| {
        entry.get(name).and_then(serde_json::Value::as_str) == Some(wanted)
    };
    for entry in &configuration.trigger_bindings {
        if field_equals(entry, "connectorId", &key.connector_id)
            && field_equals(entry, "connectionName", &key.connection_name)
            && field_equals(entry, "triggerName", &key.trigger_name)
            && field_equals(entry, "bindingName", &key.binding_name)
        {
            let Some(value @ serde_json::Value::Object(_)) = entry.get("configuration") else {
                return Err(ProjectConfigError::Invalid(
                    "trigger binding configuration must be an object".to_string(),
                ));
            };
            return serde_json::from_value(value.clone()).map_err(|error| {
                ProjectConfigError::Invalid(format!(
                    "trigger binding configuration is invalid: {error}"
                ))
            });
        }
    }
    Err(ProjectConfigError::ObjectNotFound)
}

struct DurableTriggerTarget<Payload> {
    inbox: Arc<TriggerInbox>,
    target: Arc<dyn TriggerTarget<Payload>>,
    logger: Arc<dyn TriggerLogger>,
    lock: Mutex<HashMap<String, bool>>,
}

/// Stores acknowledged events in the project Trigger inbox until `target` consumes them.
///
/// The target consumes an event by returning `Ok` or an undeliverable marker; any other error
/// keeps it pending. Replay delivers pending events in order with [`deliver_trigger`], and a
/// source must deliver new events only after replay returns. Inbox failures return like target
/// failures, so callers retry them; when only a removal failed, a retry removes the event
/// without calling the target again. `prepare_trigger` refuses a write once the context is
/// cancelled. Payloads are stored with serde through Go JSON rules and decoded with serde on
/// replay. Logs WARN for undeliverable skips, ERROR for inbox failures, and INFO at the start
/// and end of a replay that finds pending events.
pub fn new_durable_trigger_target<Payload>(
    inbox: Arc<TriggerInbox>,
    target: Arc<dyn TriggerTarget<Payload>>,
    logger: Option<Arc<dyn TriggerLogger>>,
) -> Arc<dyn TriggerTarget<Payload>>
where
    Payload: Serialize + DeserializeOwned + Send + Sync + 'static,
{
    let identity = inbox.identity().clone();
    let logger = bind_trigger_log_attributes(
        logger,
        TriggerLogAttributes::from([
            (
                "connector".to_string(),
                TriggerLogValue::Text(identity.connector_id),
            ),
            (
                "connection".to_string(),
                TriggerLogValue::Text(identity.connection_name),
            ),
            (
                "trigger".to_string(),
                TriggerLogValue::Text(identity.trigger_name),
            ),
            (
                "binding".to_string(),
                TriggerLogValue::Text(identity.binding_name),
            ),
        ]),
    );
    Arc::new(DurableTriggerTarget {
        inbox,
        target,
        logger,
        lock: Mutex::new(HashMap::new()),
    })
}

fn event_and_error_attributes(
    event_id: &str,
    error: &(dyn std::error::Error + 'static),
) -> TriggerLogAttributes {
    let mut attributes = TriggerLogAttributes::from([(
        "event_id".to_string(),
        TriggerLogValue::Text(event_id.to_string()),
    )]);
    attributes.extend(trigger_error_attributes(error));
    attributes
}

impl<Payload> DurableTriggerTarget<Payload>
where
    Payload: Serialize + DeserializeOwned + Send + Sync + 'static,
{
    fn log(&self, context: &TriggerContext, level: TriggerLogLevel, message: &str, attributes: TriggerLogAttributes) {
        log_trigger_record(Some(self.logger.as_ref()), context, level, message, attributes);
    }

    /// Consumes one event under the target lock; returns whether it was skipped.
    fn consume(
        &self,
        pending_removal: &mut HashMap<String, bool>,
        context: &TriggerContext,
        event: &TriggerEvent<Payload>,
    ) -> Result<bool, TriggerError> {
        let is_skipped = match pending_removal.get(&event.id) {
            Some(is_skipped) => *is_skipped,
            None => match self.target.handle_trigger(context, event) {
                Ok(()) => false,
                Err(error) if is_trigger_undeliverable(error.as_ref()) => {
                    self.log(
                        context,
                        TriggerLogLevel::Warn,
                        "trigger event skipped: undeliverable",
                        event_and_error_attributes(&event.id, error.as_ref()),
                    );
                    true
                }
                Err(error) => return Err(error),
            },
        };
        // The enclosing delivery attempt must not also report this event as delivered.
        if is_skipped {
            context.record_skip();
        }
        if let Err(error) = self.inbox.remove(&event.id) {
            pending_removal.insert(event.id.clone(), is_skipped);
            self.log(
                context,
                TriggerLogLevel::Error,
                "trigger inbox remove failed",
                event_and_error_attributes(&event.id, error.as_ref()),
            );
            return Err(error);
        }
        pending_removal.remove(&event.id);
        Ok(is_skipped)
    }

    fn handle_pending(
        &self,
        context: &TriggerContext,
        event: &TriggerEvent<Payload>,
    ) -> Result<bool, TriggerError> {
        // The lock covers one attempt, so prepare_trigger never waits for a pending event's backoff.
        let mut pending_removal = self.lock.lock().unwrap_or_else(PoisonError::into_inner);
        let pending = match self.inbox.pending() {
            Ok(pending) => pending,
            Err(error) => {
                self.log(
                    context,
                    TriggerLogLevel::Error,
                    "trigger inbox read failed",
                    event_and_error_attributes(&event.id, error.as_ref()),
                );
                return Err(error);
            }
        };
        if !pending.iter().any(|stored| stored.id == event.id) {
            return Ok(false);
        }
        self.consume(&mut pending_removal, context, event)
    }
}

struct PendingReplayAttempt<'durable, Payload> {
    durable: &'durable DurableTriggerTarget<Payload>,
    is_skipped: Mutex<bool>,
}

impl<Payload> TriggerTarget<Payload> for PendingReplayAttempt<'_, Payload>
where
    Payload: Serialize + DeserializeOwned + Send + Sync + 'static,
{
    fn handle_trigger(
        &self,
        context: &TriggerContext,
        event: &TriggerEvent<Payload>,
    ) -> Result<(), TriggerError> {
        let is_skipped = self.durable.handle_pending(context, event)?;
        *self.is_skipped.lock().unwrap_or_else(PoisonError::into_inner) = is_skipped;
        Ok(())
    }
}

impl<Payload> TriggerTarget<Payload> for DurableTriggerTarget<Payload>
where
    Payload: Serialize + DeserializeOwned + Send + Sync + 'static,
{
    fn handle_trigger(
        &self,
        context: &TriggerContext,
        event: &TriggerEvent<Payload>,
    ) -> Result<(), TriggerError> {
        let mut pending_removal = self.lock.lock().unwrap_or_else(PoisonError::into_inner);
        self.consume(&mut pending_removal, context, event)
            .map(|_| ())
    }

    fn prepares_trigger_delivery(&self) -> bool {
        true
    }

    fn prepare_trigger(
        &self,
        context: &TriggerContext,
        event: &TriggerEvent<Payload>,
    ) -> Result<(), TriggerError> {
        if event.id.trim().is_empty() {
            return Err(Box::new(ProjectConfigError::Invalid(
                "Trigger event ID is required".to_string(),
            )));
        }
        let encoded = encode_trigger_event(event)?;
        let _guard = self.lock.lock().unwrap_or_else(PoisonError::into_inner);
        // A source that abandoned this acknowledgement, such as a timed-out webhook, must not
        // record late.
        context.cancellation().check_cancelled()?;
        if let Err(error) = self.inbox.add(PendingTriggerEvent {
            id: event.id.clone(),
            event: encoded,
        }) {
            self.log(
                context,
                TriggerLogLevel::Error,
                "trigger inbox write failed",
                event_and_error_attributes(&event.id, error.as_ref()),
            );
            return Err(error);
        }
        Ok(())
    }

    fn replays_trigger_deliveries(&self) -> bool {
        true
    }

    fn replay_trigger_deliveries(&self, context: &TriggerContext) -> Result<(), TriggerError> {
        let pending = match self.inbox.pending() {
            Ok(pending) => pending,
            Err(error) => {
                self.log(
                    context,
                    TriggerLogLevel::Error,
                    "trigger inbox read failed",
                    trigger_error_attributes(error.as_ref()),
                );
                return Err(error);
            }
        };
        if pending.is_empty() {
            return Ok(());
        }
        self.log(
            context,
            TriggerLogLevel::Info,
            "replaying pending trigger events",
            TriggerLogAttributes::from([(
                "count".to_string(),
                TriggerLogValue::Integer(i64::try_from(pending.len()).unwrap_or(i64::MAX)),
            )]),
        );
        let mut delivered: i64 = 0;
        let mut skipped: i64 = 0;
        for (index, stored) in pending.iter().enumerate() {
            let event: TriggerEvent<Payload> = match decode_trigger_event(&stored.event) {
                Ok(event) => event,
                Err(error) => {
                    self.log(
                        context,
                        TriggerLogLevel::Error,
                        "trigger inbox event decode failed",
                        event_and_error_attributes(&stored.id, error.as_ref()),
                    );
                    return Err(format!("decode pending Trigger event: {error}").into());
                }
            };
            let attempt = PendingReplayAttempt {
                durable: self,
                is_skipped: Mutex::new(false),
            };
            if let Err(error) = deliver_trigger(context, &attempt, &event, Some(self.logger.as_ref())) {
                let mut attributes = TriggerLogAttributes::from([
                    ("delivered".to_string(), TriggerLogValue::Integer(delivered)),
                    ("skipped".to_string(), TriggerLogValue::Integer(skipped)),
                    (
                        "remaining".to_string(),
                        TriggerLogValue::Integer(
                            i64::try_from(pending.len() - index).unwrap_or(i64::MAX),
                        ),
                    ),
                ]);
                attributes.extend(trigger_error_attributes(&error));
                self.log(
                    context,
                    TriggerLogLevel::Info,
                    "finished replaying pending trigger events",
                    attributes,
                );
                return Err(Box::new(error));
            }
            if *attempt.is_skipped.lock().unwrap_or_else(PoisonError::into_inner) {
                skipped += 1;
            } else {
                delivered += 1;
            }
        }
        self.log(
            context,
            TriggerLogLevel::Info,
            "finished replaying pending trigger events",
            TriggerLogAttributes::from([
                ("delivered".to_string(), TriggerLogValue::Integer(delivered)),
                ("skipped".to_string(), TriggerLogValue::Integer(skipped)),
                ("remaining".to_string(), TriggerLogValue::Integer(0)),
            ]),
        );
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn compacts_raw_json_with_go_html_escapes() {
        let compacted =
            compact_go_raw_json(b"{ \"label\" : \"<ready>&\u{2028}\", \"n\": [1, 2] }".as_slice())
                .expect("valid JSON");
        assert_eq!(
            String::from_utf8(compacted).expect("UTF-8"),
            "{\"label\":\"\\u003cready\\u003e\\u0026\\u2028\",\"n\":[1,2]}"
        );
        assert!(compact_go_raw_json(b"{").is_err());
    }

    #[test]
    fn inbox_document_round_trips() {
        let events = vec![PendingTriggerEvent {
            id: "event-é-1".to_string(),
            event: b"{\"label\":\"<ready>&\"}".to_vec(),
        }];
        let encoded = encode_inbox_document(&events).expect("encodes");
        assert_eq!(
            String::from_utf8(encoded.clone()).expect("UTF-8"),
            "{\"schemaVersion\":\"connectors.dex.dev/project-trigger-inbox/v1alpha1\",\"events\":[{\"id\":\"event-é-1\",\"event\":{\"label\":\"\\u003cready\\u003e\\u0026\"}}]}"
        );
        let decoded = decode_inbox_document(&encoded).expect("decodes");
        assert_eq!(decoded[0].id, "event-é-1");
        assert!(decode_inbox_document(b"{\"schemaVersion\":\"x\",\"events\":[]}").is_err());
        assert!(
            decode_inbox_document(
                b"{\"schemaVersion\":\"connectors.dex.dev/project-trigger-inbox/v1alpha1\",\"extra\":1}"
            )
            .is_err()
        );
    }

    #[test]
    fn trigger_event_round_trips_with_go_field_rules() {
        let event = TriggerEvent {
            id: "Ev1".to_string(),
            occurred_at: Rfc3339NanoTimestamp::from_wire("2026-10-29T12:30:45.5+08:00")
                .expect("timestamp"),
            payload: serde_json::json!({"b": 1, "a": "<x>"}),
        };
        let encoded = encode_trigger_event(&event).expect("encodes");
        assert_eq!(
            String::from_utf8(encoded.clone()).expect("UTF-8"),
            "{\"eventId\":\"Ev1\",\"occurredAt\":\"2026-10-29T12:30:45.5+08:00\",\"payload\":{\"a\":\"\\u003cx\\u003e\",\"b\":1}}"
        );
        let decoded: TriggerEvent<serde_json::Value> =
            decode_trigger_event(&encoded).expect("decodes");
        assert_eq!(decoded, event);
        let loose: TriggerEvent<serde_json::Value> = decode_trigger_event(
            b"{\"EVENTID\":\"a\",\"eventId\":\"b\",\"occurredAt\":\"2026-10-29T12:30:45Z\",\"other\":1}",
        )
        .expect("decodes loosely");
        assert_eq!(loose.id, "b");
        assert_eq!(loose.payload, serde_json::Value::Null);
    }
}
