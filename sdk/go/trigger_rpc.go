// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package connector

import (
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/dex/sdk-go/dex"
)

// TriggerRPCConfig configures a typed RPC endpoint for provider Trigger events.
//
// Definition must be a direct bound Flow method that delegates to TriggerRPC.Handle.
// HandleEvent contains application behavior after event deduplication. DuplicateEvent
// may return the application-specific successful duplicate response.
type TriggerRPCConfig[EVENT, OUTPUT any] struct {
	Definition                     dex.RPC[TriggerEvent[EVENT], OUTPUT]
	ProcessedEventIDsAttributeName string
	HandleEvent                    dex.RPC[TriggerEvent[EVENT], OUTPUT]
	DuplicateEvent                 dex.RPC[TriggerEvent[EVENT], OUTPUT]
	Options                        *dex.RPCOptions
}

// TriggerRPC supplies registration, default options, and transactional event deduplication for an RPC Trigger.
type TriggerRPC[EVENT, OUTPUT any] struct {
	definition        dex.RPC[TriggerEvent[EVENT], OUTPUT]
	processedEventIDs dex.Attribute[[]string]
	handleEvent       dex.RPC[TriggerEvent[EVENT], OUTPUT]
	duplicateEvent    dex.RPC[TriggerEvent[EVENT], OUTPUT]
	defaultOptions    *dex.RPCOptions
}

// NewTriggerRPC validates and constructs an RPC Trigger endpoint.
func NewTriggerRPC[EVENT, OUTPUT any](config TriggerRPCConfig[EVENT, OUTPUT]) (*TriggerRPC[EVENT, OUTPUT], error) {
	if config.Definition == nil || config.HandleEvent == nil {
		return nil, fmt.Errorf("Trigger RPC definition and event handler are required")
	}
	if strings.TrimSpace(config.ProcessedEventIDsAttributeName) == "" {
		return nil, fmt.Errorf("Trigger RPC processed event IDs Attribute name is required")
	}
	processedEventIDs := dex.DefineAttribute[[]string](config.ProcessedEventIDsAttributeName)
	options := cloneRPCOptions(config.Options)
	options.LockAttributes = append([]dex.AttributeLock{dex.LockAttribute(processedEventIDs)}, options.LockAttributes...)
	return &TriggerRPC[EVENT, OUTPUT]{
		definition: config.Definition, processedEventIDs: processedEventIDs,
		handleEvent: config.HandleEvent, duplicateEvent: config.DuplicateEvent, defaultOptions: options,
	}, nil
}

// MustNewTriggerRPC constructs an RPC Trigger endpoint or panics for invalid static application wiring.
func MustNewTriggerRPC[EVENT, OUTPUT any](config TriggerRPCConfig[EVENT, OUTPUT]) *TriggerRPC[EVENT, OUTPUT] {
	triggerRPC, err := NewTriggerRPC(config)
	if err != nil {
		panic(err)
	}
	return triggerRPC
}

// Definition returns the direct bound Flow method registered through dex.DefineRPC.
func (triggerRPC *TriggerRPC[EVENT, OUTPUT]) Definition() dex.RPC[TriggerEvent[EVENT], OUTPUT] {
	return triggerRPC.definition
}

// DefaultOptions returns independent RPC options that lock the deduplication Attribute plus application locks.
func (triggerRPC *TriggerRPC[EVENT, OUTPUT]) DefaultOptions() *dex.RPCOptions {
	return cloneRPCOptions(triggerRPC.defaultOptions)
}

// PersistenceAttribute returns the event-ID Attribute that the Flow must include in PersistenceSchema.
func (triggerRPC *TriggerRPC[EVENT, OUTPUT]) PersistenceAttribute() dex.AttributeDef {
	return triggerRPC.processedEventIDs
}

// Handle transactionally records a new event ID, then invokes application behavior.
//
// The Flow's direct bound RPC method delegates here. Register that method with Definition
// and DefaultOptions so a handler error rolls back the event ID with all application writes.
func (triggerRPC *TriggerRPC[EVENT, OUTPUT]) Handle(
	ctx dex.Context,
	event TriggerEvent[EVENT],
) (*dex.RPCResult[OUTPUT], error) {
	if strings.TrimSpace(event.ID) == "" {
		return nil, fmt.Errorf("Trigger RPC event ID is required")
	}
	processedEventIDs, err := triggerRPC.processedEventIDs.Get(ctx)
	var notFound *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &notFound) {
		return nil, err
	}
	for _, processedEventID := range processedEventIDs {
		if processedEventID != event.ID {
			continue
		}
		if triggerRPC.duplicateEvent != nil {
			return triggerRPC.duplicateEvent(ctx, event)
		}
		return &dex.RPCResult[OUTPUT]{}, nil
	}
	processedEventIDs = append(processedEventIDs, event.ID)
	if err := triggerRPC.processedEventIDs.Set(ctx, processedEventIDs); err != nil {
		return nil, err
	}
	return triggerRPC.handleEvent(ctx, event)
}

func cloneRPCOptions(options *dex.RPCOptions) *dex.RPCOptions {
	if options == nil {
		return &dex.RPCOptions{}
	}
	cloned := *options
	cloned.LockAttributes = append([]dex.AttributeLock(nil), options.LockAttributes...)
	cloned.LoadAttributeMaps = append([]dex.AttributeDef(nil), options.LoadAttributeMaps...)
	cloned.LoadAttributeMapInstances = append([]dex.AttributeMapLoad(nil), options.LoadAttributeMapInstances...)
	cloned.LoadChannels = append([]dex.ChannelDef(nil), options.LoadChannels...)
	cloned.LoadChannelMaps = append([]dex.ChannelDef(nil), options.LoadChannelMaps...)
	cloned.LoadChannelMapInstances = append([]dex.ChannelMapLoad(nil), options.LoadChannelMapInstances...)
	return &cloned
}
