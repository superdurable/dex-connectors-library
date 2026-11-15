// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo

import "fmt"

// RepeatOnUnknown is how a Mutation may repeat a send whose outcome is unknown, such as a
// timeout, a connection lost after dispatch, or a 5xx. It is the manifest's
// execution.repeatOnUnknown. A definite provider refusal is never repeated.
type RepeatOnUnknown string

const (
	// RepeatOnUnknownSafe marks a read, an idempotency-keyed write, an absolute replacement,
	// upsert, or delete, or a write whose repeat is harmless. An unknown outcome retries
	// within the Step retry policy.
	RepeatOnUnknownSafe RepeatOnUnknown = "safe"
	// RepeatOnUnknownLimited marks a write that is not idempotent but whose duplicate is
	// acceptable, such as an email, a chat message, or a ticket. An unknown outcome reads
	// back when the connector can, then resends while fewer than MaxSends sends may have
	// taken effect; it selects uncertain only when the cap is reached and read-back cannot
	// confirm the write.
	RepeatOnUnknownLimited RepeatOnUnknown = "limited"
	// RepeatOnUnknownUnsafe marks a write whose duplicate does real, hard-to-undo harm. An
	// unknown outcome reads back only and never resends; an unconfirmed write selects
	// uncertain.
	RepeatOnUnknownUnsafe RepeatOnUnknown = "unsafe"
)

// MaxSendsCeiling is the largest send cap a manifest or an application may set for one
// Call ID. A manifest's execution.maxSends ranges from 2 to it; an application override
// ranges from 1 to it.
const MaxSendsCeiling int32 = 10

type maxSendsOption struct{ maxSends int32 }

func (option maxSendsOption) apply(config *runConfig) error {
	if err := validateMaxSendsOverride(option.maxSends); err != nil {
		return err
	}
	config.maxSendsOverride = option.maxSends
	return nil
}

// WithMaxSends replaces the send cap of one RunMutation call of a limited Mutation, so
// Call.MaxSends returns maxSends instead of MutationDefinition.MaxSends. maxSends ranges
// from 1, which disables resend, to MaxSendsCeiling. Every attempt of one Step execution
// must pass the same value; MutationStepConfig.MaxSends does that for a Connector Step.
// RunMutation fails the call to defect when the definition's RepeatOnUnknown is not
// limited, and RunQuery always does.
func WithMaxSends(maxSends int32) RunOption {
	return maxSendsOption{maxSends: maxSends}
}

// sendCapPolicy is the send cap a Call starts with and whether WithMaxSends may replace it.
type sendCapPolicy struct {
	maxSends      int32
	isOverridable bool
}

// noResendPolicy fits a Query and any Mutation that is not limited.
var noResendPolicy = sendCapPolicy{maxSends: 1}

func (definition MutationDefinition) sendCapPolicy() sendCapPolicy {
	if definition.RepeatOnUnknown != RepeatOnUnknownLimited {
		return noResendPolicy
	}
	return sendCapPolicy{maxSends: definition.MaxSends, isOverridable: true}
}

func validateRepeatOnUnknown(repeatOnUnknown RepeatOnUnknown, maxSends int32) error {
	switch repeatOnUnknown {
	case RepeatOnUnknownLimited:
		if maxSends < 2 || maxSends > MaxSendsCeiling {
			return fmt.Errorf("limited RepeatOnUnknown requires MaxSends within 2 through %d", MaxSendsCeiling)
		}
		return nil
	case "", RepeatOnUnknownSafe, RepeatOnUnknownUnsafe:
		if maxSends != 0 {
			return fmt.Errorf("MaxSends requires limited RepeatOnUnknown")
		}
		return nil
	default:
		return fmt.Errorf("RepeatOnUnknown %q must be safe, limited, or unsafe", repeatOnUnknown)
	}
}

func validateMaxSendsOverride(maxSends int32) error {
	if maxSends < 1 || maxSends > MaxSendsCeiling {
		return fmt.Errorf("max sends override must be within 1 through %d", MaxSendsCeiling)
	}
	return nil
}

// widenedExecuteAttempts keeps a raised send cap reachable under a definition policy that
// limits attempts: each extra send may need its own attempt.
func widenedExecuteAttempts(definitionAttempts, definitionMaxSends, overrideMaxSends int32) int32 {
	if definitionAttempts <= 0 || overrideMaxSends <= definitionMaxSends {
		return definitionAttempts
	}
	return definitionAttempts + overrideMaxSends - definitionMaxSends
}
