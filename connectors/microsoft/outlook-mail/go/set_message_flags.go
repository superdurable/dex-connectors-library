// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookmail

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const setMessageFlagsOperationID = "setMessageFlags"

// SetMessageFlagsInput sets one message's state to absolute values; a nil field leaves that state alone.
type SetMessageFlagsInput struct {
	// MessageID is the message's id from searchMessages or getMessage.
	MessageID string `json:"messageId"`
	// IsRead marks the message read when true and unread when false.
	IsRead *bool `json:"isRead,omitempty"`
	// FlagStatus sets the follow-up flag to notFlagged, flagged, or complete.
	FlagStatus *FlagStatus `json:"flagStatus,omitempty"`
	// Categories replaces the message's categories with these names; an empty list removes every category.
	// Names compare without case, at most MaxListedCategories of at most MaxCategoryCharacters each. A name
	// missing from the mailbox's category list is still applied, without a color.
	Categories *[]string `json:"categories,omitempty"`
}

// MessageFlags is a message's state after setMessageFlags.
type MessageFlags struct {
	// MessageID is the message's immutable ID.
	MessageID string `json:"messageId"`
	// IsRead reports whether the message is marked read.
	IsRead bool `json:"isRead"`
	// FlagStatus is the follow-up flag.
	FlagStatus FlagStatus `json:"flagStatus"`
	// Categories lists the message's categories.
	Categories []string `json:"categories,omitempty"`
	// WasAlreadyApplied reports that the message already held every requested value, so nothing was written.
	WasAlreadyApplied bool `json:"wasAlreadyApplied,omitempty"`
}

// SetMessageFlagsOperation implements the setMessageFlags Mutation.
type SetMessageFlagsOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (SetMessageFlagsOperation) Definition() sdkgo.MutationDefinition {
	return SetMessageFlagsDefinition
}

// IdempotencyKey uses the stable connector Call ID. Absolute values are safe to write again, so Graph
// receives no key.
func (SetMessageFlagsOperation) IdempotencyKey(callID sdkgo.CallID, _ SetMessageFlagsInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads the message's state, then sends one PATCH with only the values that differ.
func (operation SetMessageFlagsOperation) Invoke(call sdkgo.Call, input SetMessageFlagsInput) sdkgo.MutationAttempt[MessageFlags] {
	input.MessageID = strings.TrimSpace(input.MessageID)
	if err := validateSetMessageFlagsInput(input); err != nil {
		return sdkgo.NewMutationBranch(SetMessageFlagsBranchDefect, MessageFlags{}, graphFailurePointer(sdkgo.FailureValidation, setMessageFlagsOperationID, err.Error()), sdkgo.Receipt{})
	}
	session, failed := operation.client.openSession(call, setMessageFlagsOperationID)
	if failed != nil {
		attempt, _ := updateAttemptForExchange[MessageFlags](*failed, sdkgo.Receipt{}, setMessageFlagsBranches)
		return attempt
	}
	messagePath := "/messages/" + url.PathEscape(input.MessageID)
	read := session.exchange(graphRequest{method: http.MethodGet, path: messagePath, query: graphQuery{{name: "$select", value: "id,isRead,flag,categories"}}})
	if attempt, isTerminal := updateAttemptForExchange[MessageFlags](read, session.receipt(read, input.MessageID), setMessageFlagsBranches); isTerminal {
		return attempt
	}
	message, err := decodeGraphMessage(read.body)
	if err != nil {
		return sdkgo.NewMutationRetry[MessageFlags](graphFailure(sdkgo.FailureProtocol, setMessageFlagsOperationID, "Microsoft Graph returned an invalid message: "+err.Error()), 0)
	}
	current := message.summarize()
	flags := MessageFlags{MessageID: message.ID, IsRead: current.IsRead, FlagStatus: current.FlagStatus, Categories: current.Categories}
	changes := map[string]any{}
	if input.IsRead != nil && *input.IsRead != flags.IsRead {
		changes["isRead"], flags.IsRead = *input.IsRead, *input.IsRead
	}
	if input.FlagStatus != nil && *input.FlagStatus != flags.FlagStatus {
		changes["flag"], flags.FlagStatus = map[string]FlagStatus{"flagStatus": *input.FlagStatus}, *input.FlagStatus
	}
	if input.Categories != nil && !hasSameCategories(*input.Categories, message.Categories) {
		categories := append([]string{}, *input.Categories...)
		changes["categories"], flags.Categories = categories, categories
	}
	if len(changes) == 0 {
		flags.WasAlreadyApplied = true
		return sdkgo.NewMutationBranch(SetMessageFlagsBranchUpdated, flags, nil, session.receipt(read, message.ID))
	}
	updated := session.exchange(graphRequest{method: http.MethodPatch, path: messagePath, payload: changes})
	receipt := session.receipt(updated, message.ID)
	if attempt, isTerminal := updateAttemptForExchange[MessageFlags](updated, receipt, setMessageFlagsBranches); isTerminal {
		return attempt
	}
	return sdkgo.NewMutationBranch(SetMessageFlagsBranchUpdated, flags, nil, receipt)
}

var setMessageFlagsBranches = updateBranches{
	notFound: SetMessageFlagsBranchNotFound, providerRejected: SetMessageFlagsBranchProviderRejected, defect: SetMessageFlagsBranchDefect,
}

func validateSetMessageFlagsInput(input SetMessageFlagsInput) error {
	if err := validateGraphID("messageId", input.MessageID); err != nil {
		return err
	}
	if input.IsRead == nil && input.FlagStatus == nil && input.Categories == nil {
		return errors.New("set at least one of isRead, flagStatus, and categories")
	}
	if input.FlagStatus != nil {
		switch *input.FlagStatus {
		case FlagStatusNotFlagged, FlagStatusFlagged, FlagStatusComplete:
		default:
			return errors.New("flagStatus must be notFlagged, flagged, or complete")
		}
	}
	if input.Categories == nil {
		return nil
	}
	if len(*input.Categories) > MaxListedCategories {
		return fmt.Errorf("categories can hold at most %d names", MaxListedCategories)
	}
	seen := map[string]bool{}
	for _, category := range *input.Categories {
		if strings.TrimSpace(category) != category || category == "" {
			return errors.New("categories must be names without surrounding spaces")
		}
		if err := validateSingleLine("categories", category, MaxCategoryCharacters); err != nil {
			return err
		}
		if seen[strings.ToLower(category)] {
			return errors.New("categories must not repeat a name")
		}
		seen[strings.ToLower(category)] = true
	}
	return nil
}

// hasSameCategories compares category names as sets without case, as Outlook does.
func hasSameCategories(requested []string, current []string) bool {
	if len(requested) != len(current) {
		return false
	}
	names := map[string]bool{}
	for _, category := range current {
		names[strings.ToLower(category)] = true
	}
	for _, category := range requested {
		if !names[strings.ToLower(category)] {
			return false
		}
	}
	return true
}
