// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package graphtest

import (
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"
	"time"
)

// SeedMessage is a received message a test places in the mailbox.
type SeedMessage struct {
	// Folder is a well-known name or folder ID; blank is inbox.
	Folder            string
	FromName          string
	FromAddress       string
	ReplyTo           []string
	To                []string
	Cc                []string
	Subject           string
	Body              string
	IsHTMLBody        bool
	ReceivedAt        time.Time
	IsRead            bool
	FlagStatus        string
	Categories        []string
	InternetMessageID string
	Attachments       []SeedAttachment
}

// SeedAttachment is one attachment of a SeedMessage; Kind is file, item, or reference.
type SeedAttachment struct {
	Name        string
	ContentType string
	Size        int64
	IsInline    bool
	Kind        string
}

// MessageView is a message's stored state, for assertions.
type MessageView struct {
	ID                 string
	FolderID           string
	Subject            string
	Body               string
	BodyContentType    string
	To                 []string
	Cc                 []string
	Bcc                []string
	ReplyTo            []string
	IsRead             bool
	IsDraft            bool
	FlagStatus         string
	Categories         []string
	ExtendedProperties map[string]string
	ConversationID     string
	InternetMessageID  string
}

// Delivery is one message the fake accepted for sending; a repeated send of one draft delivers again.
type Delivery struct {
	MessageID  string
	Subject    string
	Recipients []string
	Body       string
}

type mailboxState struct {
	address    string
	folders    map[string]*folderState
	wellKnown  map[string]string
	messages   map[string]*messageState
	deliveries []Delivery
	sequence   int
}

type folderState struct {
	id          string
	displayName string
	parentID    string
}

type addressState struct {
	name    string
	address string
}

type messageState struct {
	id                 string
	folderID           string
	subject            string
	from               *addressState
	replyTo            []addressState
	to                 []addressState
	cc                 []addressState
	bcc                []addressState
	receivedAt         time.Time
	createdAt          time.Time
	sentAt             *time.Time
	isRead             bool
	isDraft            bool
	flagStatus         string
	categories         []string
	bodyContentType    string
	body               string
	internetMessageID  string
	conversationID     string
	attachments        []attachmentState
	extendedProperties map[string]string
	sendCompletesAt    *time.Time
}

type attachmentState struct {
	id string
	SeedAttachment
}

var htmlTagPattern = regexp.MustCompile(`<[^>]*>`)

func newMailboxState(address string) *mailboxState {
	mailbox := &mailboxState{address: address, folders: map[string]*folderState{}, wellKnown: map[string]string{}, messages: map[string]*messageState{}}
	root := mailbox.addFolder("Top of Information Store", "")
	mailbox.wellKnown["msgfolderroot"] = root
	for _, folder := range []struct{ name, displayName string }{
		{"inbox", "Inbox"}, {"drafts", "Drafts"}, {"sentitems", "Sent Items"}, {"archive", "Archive"},
		{"deleteditems", "Deleted Items"}, {"junkemail", "Junk Email"}, {"outbox", "Outbox"},
	} {
		mailbox.wellKnown[folder.name] = mailbox.addFolder(folder.displayName, root)
	}
	return mailbox
}

// AddFolder creates a folder below the mailbox root and returns its ID.
func (server *Server) AddFolder(displayName string) string {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return server.mailbox.addFolder(displayName, server.mailbox.wellKnown["msgfolderroot"])
}

// FolderID returns the ID of a well-known folder or passes an ID through.
func (server *Server) FolderID(nameOrID string) string {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	if folder := server.mailbox.resolveFolder(nameOrID); folder != nil {
		return folder.id
	}
	return ""
}

// AddMessage stores a received message and returns its immutable ID.
func (server *Server) AddMessage(seed SeedMessage) string {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	mailbox := server.mailbox
	folder := mailbox.resolveFolder(seed.Folder)
	if seed.Folder == "" {
		folder = mailbox.resolveFolder("inbox")
	}
	if folder == nil {
		panic("graphtest: unknown seed folder " + seed.Folder)
	}
	message := mailbox.newMessage(folder.id, server.now())
	message.subject, message.receivedAt, message.isRead = seed.Subject, seed.ReceivedAt.UTC(), seed.IsRead
	message.from = &addressState{name: seed.FromName, address: seed.FromAddress}
	message.replyTo, message.to, message.cc = addresses(seed.ReplyTo), addresses(seed.To), addresses(seed.Cc)
	message.flagStatus, message.categories = firstNonEmpty(seed.FlagStatus, "notFlagged"), append([]string(nil), seed.Categories...)
	message.body, message.bodyContentType = seed.Body, "text"
	if seed.IsHTMLBody {
		message.bodyContentType = "html"
	}
	sentAt := seed.ReceivedAt.UTC().Add(-time.Minute)
	message.sentAt = &sentAt
	if seed.InternetMessageID != "" {
		message.internetMessageID = seed.InternetMessageID
	}
	for _, attachment := range seed.Attachments {
		mailbox.sequence++
		message.attachments = append(message.attachments, attachmentState{id: fmt.Sprintf("AAMkAttachment-%d=", mailbox.sequence), SeedAttachment: attachment})
	}
	return message.id
}

// Message returns one message's stored state.
func (server *Server) Message(id string) (MessageView, bool) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.mailbox.completeDueSends(server.now())
	message, ok := server.mailbox.messages[id]
	if !ok {
		return MessageView{}, false
	}
	return message.view(), true
}

// MessagesInFolder returns a folder's messages, newest received first.
func (server *Server) MessagesInFolder(nameOrID string) []MessageView {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.mailbox.completeDueSends(server.now())
	folder := server.mailbox.resolveFolder(nameOrID)
	if folder == nil {
		return nil
	}
	var views []MessageView
	for _, message := range server.mailbox.sortedMessages(func(message *messageState) bool { return message.folderID == folder.id }) {
		views = append(views, message.view())
	}
	return views
}

// Deliveries returns every send the fake accepted, in order.
func (server *Server) Deliveries() []Delivery {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return append([]Delivery(nil), server.mailbox.deliveries...)
}

func (mailbox *mailboxState) addFolder(displayName string, parentID string) string {
	mailbox.sequence++
	id := fmt.Sprintf("AAMkFolder-%d_%s=", mailbox.sequence, strings.ReplaceAll(strings.ToLower(displayName), " ", ""))
	mailbox.folders[id] = &folderState{id: id, displayName: displayName, parentID: parentID}
	return id
}

func (mailbox *mailboxState) resolveFolder(nameOrID string) *folderState {
	if id, ok := mailbox.wellKnown[strings.ToLower(nameOrID)]; ok {
		return mailbox.folders[id]
	}
	return mailbox.folders[nameOrID]
}

func (mailbox *mailboxState) newMessage(folderID string, now time.Time) *messageState {
	mailbox.sequence++
	message := &messageState{
		id: fmt.Sprintf("AAMkMessage-%d_immutable-%d=", mailbox.sequence, mailbox.sequence*7919), folderID: folderID,
		createdAt: now.UTC().Truncate(time.Second), receivedAt: now.UTC().Truncate(time.Second), flagStatus: "notFlagged",
		internetMessageID: fmt.Sprintf("<fake-%d@outlook.example>", mailbox.sequence), conversationID: fmt.Sprintf("conversation-%d", mailbox.sequence),
		extendedProperties: map[string]string{}, bodyContentType: "text",
	}
	mailbox.messages[message.id] = message
	return message
}

// completeDueSends moves accepted drafts to Sent Items once their send completes.
func (mailbox *mailboxState) completeDueSends(now time.Time) {
	for _, message := range mailbox.messages {
		if message.sendCompletesAt != nil && !now.Before(*message.sendCompletesAt) {
			mailbox.completeSend(message, *message.sendCompletesAt)
		}
	}
}

func (mailbox *mailboxState) completeSend(message *messageState, at time.Time) {
	sentAt := at.UTC().Truncate(time.Second)
	message.isDraft, message.sendCompletesAt, message.sentAt = false, nil, &sentAt
	message.folderID = mailbox.wellKnown["sentitems"]
}

func (mailbox *mailboxState) sortedMessages(include func(*messageState) bool) []*messageState {
	var selected []*messageState
	for _, message := range mailbox.messages {
		if include(message) {
			selected = append(selected, message)
		}
	}
	sort.Slice(selected, func(left, right int) bool {
		if !selected[left].receivedAt.Equal(selected[right].receivedAt) {
			return selected[left].receivedAt.After(selected[right].receivedAt)
		}
		return selected[left].id > selected[right].id
	})
	return selected
}

func (message *messageState) view() MessageView {
	properties := map[string]string{}
	for id, value := range message.extendedProperties {
		properties[id] = value
	}
	return MessageView{
		ID: message.id, FolderID: message.folderID, Subject: message.subject, Body: message.body, BodyContentType: message.bodyContentType,
		To: addressList(message.to), Cc: addressList(message.cc), Bcc: addressList(message.bcc), ReplyTo: addressList(message.replyTo),
		IsRead: message.isRead, IsDraft: message.isDraft, FlagStatus: message.flagStatus, Categories: append([]string(nil), message.categories...),
		ExtendedProperties: properties, ConversationID: message.conversationID, InternetMessageID: message.internetMessageID,
	}
}

func (message *messageState) recipients() []string {
	return append(append(addressList(message.to), addressList(message.cc)...), addressList(message.bcc)...)
}

// plainText removes HTML markup, as Graph does for Prefer: outlook.body-content-type="text".
func (message *messageState) plainText() string {
	if message.bodyContentType != "html" {
		return message.body
	}
	return strings.TrimSpace(html.UnescapeString(htmlTagPattern.ReplaceAllString(message.body, "\n")))
}

func addresses(values []string) []addressState {
	var list []addressState
	for _, value := range values {
		list = append(list, addressState{address: value})
	}
	return list
}

func addressList(list []addressState) []string {
	var values []string
	for _, item := range list {
		values = append(values, item.address)
	}
	return values
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
