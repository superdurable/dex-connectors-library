// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package projectconfig shares project-scoped, versioned configuration and credential storage between Dex Web and application replicas.
package projectconfig

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrObjectNotFound reports an absent object or exact version.
var ErrObjectNotFound = errors.New("project configuration object is absent")

// ErrConflict reports a failed conditional write; callers must reload the authoritative object.
var ErrConflict = errors.New("project configuration revision changed")

// ErrOutcomeUnknown reports a write whose acceptance must be reconciled before another mutation.
var ErrOutcomeUnknown = errors.New("project configuration write outcome is unknown")

// Object holds a versioned private object. Contents may contain secrets and must never enter a Flow or browser response.
type Object struct {
	// Key is relative to the constructor-owned bucket prefix.
	Key string
	// Version identifies the exact immutable object version.
	Version string
	// ETag is the opaque compare-and-swap token for this representation.
	ETag string
	// Contents contains private object bytes owned by the caller.
	Contents []byte
}

// String returns only identity; private contents are never formatted.
func (object Object) String() string {
	return fmt.Sprintf("projectconfig.Object{%q version=%q}", object.Key, object.Version)
}

// GoString keeps private contents out of Go-syntax formatting.
func (object Object) GoString() string { return object.String() }

// MarshalJSON rejects accidental transport of private object contents.
func (Object) MarshalJSON() ([]byte, error) {
	return nil, errors.New("private project objects cannot be serialized")
}

// ObjectStore provides strongly consistent reads and conditional, durable versioned writes.
// Implementations must never retry a conditional write with a different precondition.
type ObjectStore interface {
	// ReadObject reads an exact version, or the current version when version is empty.
	ReadObject(ctx context.Context, key, version string) (Object, error)
	// CreateObject creates a previously absent key and returns ErrConflict if it already exists.
	CreateObject(ctx context.Context, key string, contents []byte) (Object, error)
	// CompareAndSwapObject replaces a key only when its current ETag equals expectedETag.
	CompareAndSwapObject(ctx context.Context, key, expectedETag string, contents []byte) (Object, error)
}

// Scope binds a store to one project and independently configured Live or Preview credentials.
type Scope struct {
	// ProjectID is a trusted project identity, never an arbitrary browser path.
	ProjectID string `json:"projectId"`
	// Kind is live or preview.
	Kind string `json:"kind"`
	// SessionID is required only for preview.
	SessionID string `json:"sessionId,omitempty"`
}

var scopeIdentity = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)

// Prefix validates the fixed scope and returns its canonical relative storage prefix.
func (scope Scope) Prefix() (string, error) {
	if !scopeIdentity.MatchString(scope.ProjectID) || (scope.Kind != "live" && scope.Kind != "preview") || (scope.Kind == "live" && scope.SessionID != "") || (scope.Kind == "preview" && !scopeIdentity.MatchString(scope.SessionID)) {
		return "", errors.New("invalid project configuration scope")
	}
	prefix := "projects/" + scope.ProjectID + "/" + scope.Kind
	if scope.Kind == "preview" {
		prefix += "/" + scope.SessionID
	}
	return prefix, nil
}

func validateObjectKey(key string) error {
	if key == "" || len(key) > 1024 || strings.HasPrefix(key, "/") || strings.ContainsAny(key, "\\\x00\r\n") {
		return errors.New("invalid project object key")
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return errors.New("invalid project object key")
		}
	}
	return nil
}

// MarshalText rejects accidental textual transport of private material.
func (Object) MarshalText() ([]byte, error) {
	return nil, errors.New("private project objects cannot be serialized")
}

// MarshalYAML rejects accidental YAML transport of private material.
func (Object) MarshalYAML() (any, error) {
	return nil, errors.New("private project objects cannot be serialized")
}
