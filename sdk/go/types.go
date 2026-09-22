// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package connector defines the provider-neutral runtime contract used by Dex connectors.
package connector

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// CallID is the stable identity of one provider call across retries.
type CallID string

func NewCallID() CallID {
	return CallID(uuid.NewString())
}

func (id CallID) Validate() error {
	if id == "" {
		return fmt.Errorf("call ID is required")
	}
	if _, err := uuid.Parse(string(id)); err != nil {
		return fmt.Errorf("call ID must be a UUID: %w", err)
	}
	return nil
}

// ConnectionRef is a logical credential reference safe to persist in a Flow.
type ConnectionRef struct {
	Provider string `json:"provider" yaml:"provider"`
	Name     string `json:"name" yaml:"name"`
}

func (ref ConnectionRef) Validate() error {
	if ref.Provider == "" || ref.Name == "" {
		return fmt.Errorf("connection provider and name are required")
	}
	return nil
}

type OperationKind string

const (
	OperationQuery  OperationKind = "QUERY"
	OperationAction OperationKind = "ACTION"
)

type ActionOutcome string

const (
	ActionSucceeded ActionOutcome = "SUCCEEDED"
	ActionFailed    ActionOutcome = "FAILED"
	ActionUnknown   ActionOutcome = "UNKNOWN"
)

// Receipt contains only safe provider correlation data.
type Receipt struct {
	CallID            CallID            `json:"callId"`
	Provider          string            `json:"provider"`
	ProviderObjectID  string            `json:"providerObjectId,omitempty"`
	ProviderRequestID string            `json:"providerRequestId,omitempty"`
	Outcome           ActionOutcome     `json:"outcome"`
	ObservedAt        time.Time         `json:"observedAt"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}

type Result[T any] struct {
	Value   T                 `json:"value"`
	Receipt *Receipt          `json:"receipt,omitempty"`
	Meta    map[string]string `json:"meta,omitempty"`
}
