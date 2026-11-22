// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail

import "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"

// NewDurableMessageTriggerRunnerForTest builds the NewProjectMessageTriggerRunner runner over test inbox storage.
func NewDurableMessageTriggerRunnerForTest(
	connection Connection,
	configuration projectconfig.Configuration,
	openInbox func(projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error),
	config ProjectMessageTriggerRunnerConfig,
) (*MessageTriggerRunner, error) {
	return newDurableMessageTriggerRunner(connection, configuration, openInbox, config)
}

// SentMailCandidateQueryForSendForTest returns the read-back query of a send to these recipients.
func SentMailCandidateQueryForSendForTest(to []string) (string, bool) {
	recipients, failure := validateSendInput(SendMessageInput{To: to, Subject: "Progress", TextBody: "Update"}, "owner@example.com")
	if failure != nil {
		return "", false
	}
	return sentMailCandidateQuery(recipients), true
}

// SentMailCandidateQueryForReplyForTest returns the read-back query of a reply to this source.
func SentMailCandidateQueryForReplyForTest(source Message) (string, bool) {
	recipient, err := replyRecipient(source)
	if err != nil {
		return "", false
	}
	return sentMailCandidateQuery([]string{recipient}), true
}

// FormatRecipientsForTest returns the To: mailboxes a send to these recipients writes.
func FormatRecipientsForTest(to []string) ([]string, bool) {
	recipients, failure := validateSendInput(SendMessageInput{To: to, Subject: "Progress", TextBody: "Update"}, "owner@example.com")
	return recipients, failure == nil
}

// DecodeToHeaderForTest returns the getMessage to: list of one To header.
func DecodeToHeaderForTest(value string) []string {
	return splitAddressHeader(value)
}

// ReplyRecipientForTest returns the To: mailbox of a reply to this source.
func ReplyRecipientForTest(source Message) (string, bool) {
	recipient, err := replyRecipient(source)
	return recipient, err == nil
}
