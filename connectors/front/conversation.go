// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package front

import "time"

// ConversationStatus is the status Front reports for a conversation. The connector returns Front's value
// unchanged, including any value Front adds later, and never maps it to another vocabulary.
type ConversationStatus string

const (
	// ConversationStatusUnassigned is an open conversation without an assignee.
	ConversationStatusUnassigned ConversationStatus = "unassigned"
	// ConversationStatusAssigned is an open conversation with an assignee.
	ConversationStatusAssigned ConversationStatus = "assigned"
	// ConversationStatusArchived is an archived conversation, or a snoozed one, which Front reports as archived
	// with a scheduled reminder; Conversation.IsSnoozed tells them apart.
	ConversationStatusArchived ConversationStatus = "archived"
	// ConversationStatusDeleted is a conversation in the trash.
	ConversationStatusDeleted ConversationStatus = "deleted"
)

// ConversationStatuses returns the four statuses Front documents for a conversation, so a Flow can map them
// to its own vocabulary explicitly.
func ConversationStatuses() []ConversationStatus {
	return []ConversationStatus{ConversationStatusUnassigned, ConversationStatusAssigned, ConversationStatusArchived, ConversationStatusDeleted}
}

// ConversationStatusCategory is the category of a ticketing company's custom ticket status, such as the one
// a conversation's StatusID names. Front sets it only when ticketing is enabled.
type ConversationStatusCategory string

const (
	// ConversationStatusCategoryOpen is a ticket status waiting for the team.
	ConversationStatusCategoryOpen ConversationStatusCategory = "open"
	// ConversationStatusCategoryWaiting is a ticket status waiting for the customer or a third party.
	ConversationStatusCategoryWaiting ConversationStatusCategory = "waiting"
	// ConversationStatusCategoryResolved is a resolved ticket status.
	ConversationStatusCategoryResolved ConversationStatusCategory = "resolved"
)

// ConversationStatusChange is a status that updateConversation can set, in Front's write vocabulary, which
// differs from the statuses Front reports: open becomes assigned or unassigned, and deleted moves the
// conversation to the trash.
type ConversationStatusChange string

const (
	// ConversationStatusChangeOpen reopens an archived, snoozed, or trashed conversation.
	ConversationStatusChangeOpen ConversationStatusChange = "open"
	// ConversationStatusChangeArchived archives the conversation.
	ConversationStatusChangeArchived ConversationStatusChange = "archived"
	// ConversationStatusChangeDeleted moves the conversation to the trash; Front deletes it permanently only later.
	ConversationStatusChangeDeleted ConversationStatusChange = "deleted"
)

// ConversationStatusChanges returns the statuses updateConversation accepts. Front's spam status is not
// offered, because Front reports no status that a read-back could compare with it.
func ConversationStatusChanges() []ConversationStatusChange {
	return []ConversationStatusChange{ConversationStatusChangeOpen, ConversationStatusChangeArchived, ConversationStatusChangeDeleted}
}

// MaxBodyBytes bounds each message and comment body a Result carries, so durable Step state stays small.
// A longer body is cut at a UTF-8 boundary and flagged as truncated.
const MaxBodyBytes = 16 << 10

// Conversation is the connector's view of one Front conversation. Custom fields, links, and metadata are
// omitted. Times are UTC.
type Conversation struct {
	// ID is the Front conversation ID, such as cnv_55c8c149.
	ID string `json:"id"`
	// Type is Front's conversation type: conversation, discussion, or task.
	Type string `json:"type,omitempty"`
	// Subject is the conversation subject, or empty.
	Subject string `json:"subject,omitempty"`
	// Status is Front's status: unassigned, assigned, archived, or deleted.
	Status ConversationStatus `json:"status"`
	// StatusID is the ticket status ID, such as sts_5x, set only when the company uses ticketing.
	StatusID string `json:"statusId,omitempty"`
	// StatusCategory is the ticket status category: open, waiting, or resolved; empty without ticketing.
	StatusCategory ConversationStatusCategory `json:"statusCategory,omitempty"`
	// TicketIDs lists the conversation's ticket numbers, such as TICKET-1.
	TicketIDs []string `json:"ticketIds,omitempty"`
	// Assignee is the assigned teammate, or nil when the conversation is unassigned.
	Assignee *Teammate `json:"assignee,omitempty"`
	// Recipient is the conversation's main recipient, generally the sender of the last inbound message.
	Recipient *Recipient `json:"recipient,omitempty"`
	// Tags lists the conversation's tags.
	Tags []Tag `json:"tags,omitempty"`
	// IsPrivate reports a conversation in a teammate's personal inbox.
	IsPrivate bool `json:"isPrivate,omitempty"`
	// IsSnoozed reports an archived conversation with a scheduled reminder, which Front reopens later.
	IsSnoozed bool `json:"isSnoozed,omitempty"`
	// CreatedAt is when the conversation was created.
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt is when the conversation last changed, or nil when Front omits it.
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
	// WaitingSince is when the oldest unreplied message arrived, or nil when nothing waits for a reply.
	WaitingSince *time.Time `json:"waitingSince,omitempty"`
	// DueAt is a task conversation's due time, or nil.
	DueAt *time.Time `json:"dueAt,omitempty"`
}

// Teammate is a Front user, bot, or visitor that is assigned, authored a message, or wrote a comment.
type Teammate struct {
	// ID is the teammate ID, such as tea_2thf.
	ID string `json:"id"`
	// Email is the teammate's email address, or empty for a bot.
	Email string `json:"email,omitempty"`
	// FirstName is the teammate's first name, or empty.
	FirstName string `json:"firstName,omitempty"`
	// LastName is the teammate's last name, or empty.
	LastName string `json:"lastName,omitempty"`
}

// Recipient is one handle on a conversation or message, such as the customer's email address.
type Recipient struct {
	// Handle identifies the recipient on its channel, such as jane@acme.example.com or +12345678900.
	Handle string `json:"handle"`
	// Role is Front's role: from, to, cc, bcc, or reply-to.
	Role string `json:"role,omitempty"`
	// Name is the recipient's display name, or empty.
	Name string `json:"name,omitempty"`
	// ContactID is the Front contact with this handle, such as crd_1y8sp71, or empty when none exists.
	ContactID string `json:"contactId,omitempty"`
}

// Tag is a tag applied to a conversation.
type Tag struct {
	// ID is the tag ID, such as tag_13o8r1; Front adds, removes, and searches tags by ID.
	ID string `json:"id"`
	// Name is the tag name.
	Name string `json:"name,omitempty"`
}

// Message is one message in a conversation: an inbound customer message or a reply the team sent.
type Message struct {
	// ID is the message ID, such as msg_1q15qmtq.
	ID string `json:"id"`
	// Type is Front's channel type, such as email, sms, front_chat, or custom.
	Type string `json:"type,omitempty"`
	// IsInbound reports a message received from outside the company.
	IsInbound bool `json:"isInbound"`
	// IsDraft reports an unsent draft.
	IsDraft bool `json:"isDraft,omitempty"`
	// Subject is the message subject, or empty.
	Subject string `json:"subject,omitempty"`
	// Blurb is Front's short plain-text preview of the body.
	Blurb string `json:"blurb,omitempty"`
	// Body is the message body as Front stores it, HTML for email, cut at MaxBodyBytes.
	Body string `json:"body,omitempty"`
	// IsBodyTruncated reports that Body was cut at MaxBodyBytes.
	IsBodyTruncated bool `json:"isBodyTruncated,omitempty"`
	// Author is the teammate who sent an outbound message, or nil for an inbound one.
	Author *Teammate `json:"author,omitempty"`
	// Recipients lists the message's handles with their roles; the from handle of an inbound message is the requester.
	Recipients []Recipient `json:"recipients,omitempty"`
	// CreatedAt is when the message was sent or received.
	CreatedAt time.Time `json:"createdAt"`
}

// Comment is one internal comment that only teammates see.
type Comment struct {
	// ID is the comment ID, such as com_1ywg3f2.
	ID string `json:"id"`
	// Body is the comment text, which can hold Markdown, cut at MaxBodyBytes.
	Body string `json:"body,omitempty"`
	// IsBodyTruncated reports that Body was cut at MaxBodyBytes.
	IsBodyTruncated bool `json:"isBodyTruncated,omitempty"`
	// Author is the teammate who wrote the comment, or nil when Front omits it.
	Author *Teammate `json:"author,omitempty"`
	// IsPinned reports a comment pinned to the conversation.
	IsPinned bool `json:"isPinned,omitempty"`
	// PostedAt is when the comment was posted.
	PostedAt time.Time `json:"postedAt"`
}
