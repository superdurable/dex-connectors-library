// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

const (
	triggerInboxSchema = "connectors.dex.dev/project-trigger-inbox/v1alpha1"
	// maxPendingTriggerEvents bounds one binding's backlog; a source must stop acknowledging new events beyond it.
	maxPendingTriggerEvents = 1000
	// maxTriggerInboxBytes bounds one inbox object.
	maxTriggerInboxBytes = 8 << 20
	// maxTriggerInboxAttempts bounds compare-and-swap retries against concurrent writers.
	maxTriggerInboxAttempts = 8
)

// triggerNamePattern matches a manifest Trigger name, which follows the operation identifier format.
var triggerNamePattern = regexp.MustCompile(`^[a-z][A-Za-z0-9]{1,62}$`)

// ErrTriggerInboxFull reports a binding whose pending backlog reached its bound.
var ErrTriggerInboxFull = errors.New("project trigger inbox is full")

// TriggerInboxKey identifies one application binding of a connector Trigger.
type TriggerInboxKey struct {
	// ConnectorID is the manifest's connector identifier.
	ConnectorID string
	// ConnectionName is the application-declared connection the Trigger reads.
	ConnectionName string
	// TriggerName is the manifest Trigger name.
	TriggerName string
	// BindingName is the application's binding of that Trigger.
	BindingName string
}

// PendingTriggerEvent is one acknowledged Trigger event that the application has not yet consumed.
type PendingTriggerEvent struct {
	// ID is the source's stable event identity.
	ID string `json:"id"`
	// Event is the complete encoded event, decoded only by the consuming application.
	Event json.RawMessage `json:"event"`
}

type triggerInboxDocument struct {
	SchemaVersion string                `json:"schemaVersion"`
	Events        []PendingTriggerEvent `json:"events"`
}

// TriggerInbox stores acknowledged Trigger events in project storage until the application consumes them,
// so a restarted or replaced replica delivers them in order. Writes use compare-and-swap against concurrent replicas.
type TriggerInbox struct {
	objects ObjectStore
	key     string
}

// NewTriggerInbox binds one Trigger binding's inbox in scope without reading or writing objects.
func NewTriggerInbox(objects ObjectStore, scope Scope, key TriggerInboxKey) (*TriggerInbox, error) {
	if objects == nil {
		return nil, errors.New("trigger inbox object store is required")
	}
	prefix, err := scope.Prefix()
	if err != nil {
		return nil, err
	}
	if !regexpConnectorID(key.ConnectorID) || !triggerNamePattern.MatchString(key.TriggerName) {
		return nil, errors.New("trigger inbox connector or trigger identity is invalid")
	}
	for _, name := range []string{key.ConnectionName, key.BindingName} {
		if strings.TrimSpace(name) != name || name == "" || len(name) > 256 || strings.ContainsAny(name, "\x00\r\n") {
			return nil, errors.New("trigger inbox connection or binding name is invalid")
		}
	}
	encode := base64.RawURLEncoding.EncodeToString
	path := prefix + "/trigger-inboxes/" + key.ConnectorID + "/" + encode([]byte(key.ConnectionName)) + "/" + key.TriggerName + "/" + encode([]byte(key.BindingName)) + "/head"
	return &TriggerInbox{objects: objects, key: path}, nil
}

// Pending returns the pending events in acknowledgement order.
func (inbox *TriggerInbox) Pending(ctx context.Context) ([]PendingTriggerEvent, error) {
	document, _, err := inbox.read(ctx)
	return document.Events, err
}

// Add appends an acknowledged event once; an event whose ID is already pending is unchanged.
func (inbox *TriggerInbox) Add(ctx context.Context, event PendingTriggerEvent) error {
	if strings.TrimSpace(event.ID) == "" || !json.Valid(event.Event) {
		return errors.New("trigger inbox event identity or contents are invalid")
	}
	return inbox.update(ctx, func(events []PendingTriggerEvent) ([]PendingTriggerEvent, error) {
		for _, pending := range events {
			if pending.ID == event.ID {
				return events, nil
			}
		}
		if len(events) >= maxPendingTriggerEvents {
			return nil, ErrTriggerInboxFull
		}
		return append(events, event), nil
	})
}

// Remove deletes a consumed event; removing an absent event succeeds.
func (inbox *TriggerInbox) Remove(ctx context.Context, eventID string) error {
	return inbox.update(ctx, func(events []PendingTriggerEvent) ([]PendingTriggerEvent, error) {
		for index, pending := range events {
			if pending.ID == eventID {
				return append(events[:index:index], events[index+1:]...), nil
			}
		}
		return events, nil
	})
}

// update applies change to the current document and writes it conditionally, retrying a bounded number
// of times when another replica changed the inbox first. change must be deterministic for one input.
func (inbox *TriggerInbox) update(ctx context.Context, change func([]PendingTriggerEvent) ([]PendingTriggerEvent, error)) error {
	for attempt := 0; attempt < maxTriggerInboxAttempts; attempt++ {
		document, etag, err := inbox.read(ctx)
		if err != nil {
			return err
		}
		events, err := change(document.Events)
		if err != nil {
			return err
		}
		if equalPendingEvents(events, document.Events) {
			return nil
		}
		contents, err := json.Marshal(triggerInboxDocument{SchemaVersion: triggerInboxSchema, Events: events})
		if err != nil {
			return errors.New("trigger inbox encoding failed")
		}
		if len(contents) > maxTriggerInboxBytes {
			return ErrTriggerInboxFull
		}
		if etag == "" {
			_, err = inbox.objects.CreateObject(ctx, inbox.key, contents)
		} else {
			_, err = inbox.objects.CompareAndSwapObject(ctx, inbox.key, etag, contents)
		}
		if errors.Is(err, ErrOutcomeUnknown) {
			observed, readErr := inbox.objects.ReadObject(ctx, inbox.key, "")
			if readErr == nil && bytes.Equal(observed.Contents, contents) {
				return nil
			}
		}
		if errors.Is(err, ErrConflict) || errors.Is(err, ErrOutcomeUnknown) {
			continue
		}
		return err
	}
	return ErrConflict
}

func (inbox *TriggerInbox) read(ctx context.Context) (triggerInboxDocument, string, error) {
	object, err := inbox.objects.ReadObject(ctx, inbox.key, "")
	if errors.Is(err, ErrObjectNotFound) {
		return triggerInboxDocument{SchemaVersion: triggerInboxSchema}, "", nil
	}
	if err != nil {
		return triggerInboxDocument{}, "", err
	}
	var document triggerInboxDocument
	if strictJSON(object.Contents, &document) != nil || document.SchemaVersion != triggerInboxSchema || object.ETag == "" {
		return triggerInboxDocument{}, "", errors.New("trigger inbox document is invalid")
	}
	return document, object.ETag, nil
}

func equalPendingEvents(left, right []PendingTriggerEvent) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].ID != right[index].ID {
			return false
		}
	}
	return true
}
