// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pollingtrigger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/triggerlog"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// feedNamePattern matches a stored feed name, which follows the manifest Trigger name format.
var feedNamePattern = regexp.MustCompile(`^[a-z][A-Za-z0-9]{1,62}$`)

// BindingPollerConfig configures a poller that owns one Trigger binding and its cursor.
type BindingPollerConfig[EVENT any] struct {
	// Definition is the immutable Trigger definition; Binding.Trigger must equal Definition.Trigger.
	Definition sdkgo.TriggerDefinition
	// Binding identifies the configured Trigger binding. Its name is the poller name for WakePoller.
	Binding sdkgo.TriggerBindingRef
	// Cursor persists the binding's position, normally LoadedProject.PollCursorStore.
	Cursor CursorStore
	// Fetcher reads the provider; its changes are this binding's events.
	Fetcher ChangeFetcher[sdkgo.TriggerEvent[EVENT]]
	// Target receives events. Wrap it with provider.NewDurableTriggerTarget so events are recorded in the
	// project Trigger inbox before the cursor commits; a non-durable target consumes each event first.
	Target sdkgo.TriggerTarget[EVENT]
	// Snapshot enables a keyed snapshot that the fetcher keeps in Cursor.Snapshot. Nil disables it.
	Snapshot *KeyedSnapshotOptions
}

// NewBindingPoller validates config and returns a Poller for NewRunner. It reads and writes nothing.
func NewBindingPoller[EVENT any](config BindingPollerConfig[EVENT]) (Poller, error) {
	if config.Target == nil {
		return nil, errors.New("poll Trigger target is required")
	}
	return newBindingPoller(config, config.Target, deliveryModeFor(config.Target), true)
}

func newBindingPoller[EVENT any](
	config BindingPollerConfig[EVENT],
	target sdkgo.TriggerTarget[EVENT],
	mode memberDeliveryMode,
	shouldReplay bool,
) (*cursorPoller[sdkgo.TriggerEvent[EVENT]], error) {
	if err := validateMemberBinding(config.Definition, config.Binding); err != nil {
		return nil, err
	}
	if config.Cursor == nil || config.Fetcher == nil {
		return nil, errors.New("poll Trigger cursor store and fetcher are required")
	}
	snapshot, err := resolvePollerSnapshot(config.Snapshot)
	if err != nil {
		return nil, err
	}
	selectEvent := func(change sdkgo.TriggerEvent[EVENT], _ *projectconfig.KeyedSnapshotEntry) []sdkgo.TriggerEvent[EVENT] {
		return []sdkgo.TriggerEvent[EVENT]{change}
	}
	member := newFeedMember(config.Definition, config.Binding, selectEvent, target, mode, shouldReplay)
	identity := bindingAttrs(config.Binding)
	return &cursorPoller[sdkgo.TriggerEvent[EVENT]]{
		name: config.Binding.Name, identity: identity, cursor: config.Cursor, fetcher: config.Fetcher,
		members: []pollMember[sdkgo.TriggerEvent[EVENT]]{member}, snapshot: snapshot, log: triggerlog.New(nil, identity...),
	}, nil
}

// FeedPollerConfig configures one provider change feed shared by several Trigger bindings of one
// connection, so the provider is read once per poll for all of them.
type FeedPollerConfig[CHANGE any] struct {
	// Name is the feed's storage identity (projectconfig.PollFeedKey.FeedName) and its poller name for
	// WakePoller. It is lower camel case, 2 to 63 characters, and must stay stable forever.
	Name string
	// Cursor persists the feed position, normally LoadedProject.PollFeedCursorStore.
	Cursor CursorStore
	// Fetcher reads the provider feed once per poll for every member.
	Fetcher ChangeFetcher[CHANGE]
	// Members are recorded and delivered in this order for every page. Every member uses the same
	// connector and connection, and at least one is required.
	Members []FeedMember[CHANGE]
	// Snapshot enables a keyed snapshot that the fetcher keeps in Cursor.Snapshot. When CHANGE
	// implements KeyedItem, each selector receives the change's entry from the committed snapshot.
	Snapshot *KeyedSnapshotOptions
}

// NewFeedPoller validates config and returns a Poller for NewRunner. It reads and writes nothing.
func NewFeedPoller[CHANGE any](config FeedPollerConfig[CHANGE]) (Poller, error) {
	if !feedNamePattern.MatchString(config.Name) {
		return nil, errors.New("poll feed name must be lower camel case, 2 to 63 characters")
	}
	if config.Cursor == nil || config.Fetcher == nil || len(config.Members) == 0 {
		return nil, errors.New("poll feed cursor store, fetcher, and members are required")
	}
	snapshot, err := resolvePollerSnapshot(config.Snapshot)
	if err != nil {
		return nil, err
	}
	members := make([]pollMember[CHANGE], 0, len(config.Members))
	first := config.Members[0]
	if first == nil {
		return nil, errors.New("poll feed member is nil")
	}
	for _, configured := range config.Members {
		if configured == nil {
			return nil, errors.New("poll feed member is nil")
		}
		binding := configured.memberBinding()
		if binding.Trigger.ConnectorID != first.memberBinding().Trigger.ConnectorID || binding.Connection != first.memberBinding().Connection {
			return nil, fmt.Errorf("poll feed %q members must share one connector and connection", config.Name)
		}
		members = append(members, configured.pollMember())
	}
	binding := first.memberBinding()
	identity := []slog.Attr{
		slog.String("connector", binding.Trigger.ConnectorID), slog.String("connection", binding.Connection.Name),
		slog.String("feed", config.Name),
	}
	return &cursorPoller[CHANGE]{
		name: config.Name, identity: identity, cursor: config.Cursor, fetcher: config.Fetcher,
		members: members, snapshot: snapshot, log: triggerlog.New(nil, identity...),
	}, nil
}

func resolvePollerSnapshot(options *KeyedSnapshotOptions) (*KeyedSnapshotOptions, error) {
	if options == nil {
		return nil, nil
	}
	resolved, err := resolveKeyedSnapshotOptions(*options)
	if err != nil {
		return nil, err
	}
	return &resolved, nil
}

// bindingTriggerSource adapts one binding poller to sdkgo.TriggerSource.
type bindingTriggerSource[EVENT any] struct {
	config   BindingPollerConfig[EVENT]
	schedule PollSchedule
}

// NewBindingTriggerSource runs a single-binding poll loop as an sdkgo.TriggerSource, so a generated
// per-Trigger factory keeps using sdkgo.NewTrigger. The target that NewTrigger passes to Run replaces
// config.Target, which must be nil. Because that target hides whether events are durable, the source
// records each event with sdkgo.PrepareTriggerDelivery and also consumes it before the cursor commits.
// NewTrigger replays a durable target before Run, so the source does not replay it again.
func NewBindingTriggerSource[EVENT any](config BindingPollerConfig[EVENT], schedule PollSchedule) (sdkgo.TriggerSource[EVENT], error) {
	if config.Target != nil {
		return nil, errors.New("poll Trigger source target must be nil; sdkgo.NewTrigger supplies it")
	}
	if _, err := newBindingPoller(config, sdkgo.TriggerTargetFunc[EVENT](rejectTriggerEvent[EVENT]), recordAndDeliverBeforeCommit, false); err != nil {
		return nil, err
	}
	if _, err := resolvePollSchedule(schedule); err != nil {
		return nil, err
	}
	return &bindingTriggerSource[EVENT]{config: config, schedule: schedule}, nil
}

// Run polls until ctx ends, delivering to target. It returns ctx.Err() on cancellation, or an error
// for a stored cursor document that can never be decoded.
func (source *bindingTriggerSource[EVENT]) Run(ctx context.Context, target sdkgo.TriggerTarget[EVENT]) error {
	if target == nil {
		return errors.New("poll Trigger source target is required")
	}
	poller, err := newBindingPoller(source.config, target, recordAndDeliverBeforeCommit, false)
	if err != nil {
		return err
	}
	runner, err := NewRunner(RunnerConfig{Connection: source.config.Binding.Connection, Schedule: source.schedule, Pollers: []Poller{poller}})
	if err != nil {
		return err
	}
	return runner.Run(ctx)
}

func rejectTriggerEvent[EVENT any](context.Context, sdkgo.TriggerEvent[EVENT]) error {
	return errors.New("poll Trigger source target is not configured")
}
