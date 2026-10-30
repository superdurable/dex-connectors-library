// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package teams

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var htmlWhitespaceReplacer = strings.NewReplacer("\r", " ", "\n", " ", "\t", " ")

const (
	maximumDisplayNameCharacters = 256
	maximumSubjectCharacters     = 1024
	maximumWebURLBytes           = 2048
)

// ContentType is the representation of a message body.
type ContentType string

const (
	// ContentTypeText is plain text; Teams shows it literally.
	ContentTypeText ContentType = "text"
	// ContentTypeHTML is the subset of HTML Teams renders, such as <p>, <b>, <a>, and <br>.
	ContentTypeHTML ContentType = "html"
)

// MessageImportance is how prominently Teams shows a message.
type MessageImportance string

const (
	// MessageImportanceNormal is an ordinary message.
	MessageImportanceNormal MessageImportance = "normal"
	// MessageImportanceHigh marks the message as important.
	MessageImportanceHigh MessageImportance = "high"
	// MessageImportanceUrgent marks the message as urgent; Teams documents it for chat messages.
	MessageImportanceUrgent MessageImportance = "urgent"
)

// MessageSender identifies who posted a message. Exactly one of UserID and ApplicationID is
// usually set; both are empty for a system event.
type MessageSender struct {
	// UserID is the sender's Microsoft Entra object ID when a person posted, including through this connector.
	UserID string `json:"userId,omitempty"`
	// ApplicationID is the sending app's ID when a bot or connector posted.
	ApplicationID string `json:"applicationId,omitempty"`
	// DisplayName is the sender's display name at the time Graph read the message.
	DisplayName string `json:"displayName,omitempty"`
}

// Message is one channel message or reply as Microsoft Graph returned it, with its body converted to text.
type Message struct {
	// ID is the message ID, a decimal number unique within its channel or thread, such as 1616989753153.
	ID string `json:"id"`
	// ReplyToID is the root message ID for a reply.
	ReplyToID string `json:"replyToId,omitempty"`
	// MessageType is Graph's messageType, such as message or systemEventMessage.
	MessageType string `json:"messageType"`
	// CreatedAt is when Teams stored the message.
	CreatedAt time.Time `json:"createdAt"`
	// LastEditedAt is when the message was last edited, or nil when it was never edited.
	LastEditedAt *time.Time `json:"lastEditedAt,omitempty"`
	// IsDeleted reports a deleted message; its Text is then empty.
	IsDeleted bool `json:"isDeleted,omitempty"`
	// Sender identifies who posted the message.
	Sender MessageSender `json:"sender"`
	// Subject is the plain-text subject of a root message, or empty.
	Subject string `json:"subject,omitempty"`
	// Importance is normal, high, or urgent.
	Importance MessageImportance `json:"importance,omitempty"`
	// ContentType is the body's representation in Teams.
	ContentType ContentType `json:"contentType,omitempty"`
	// Text is the body as plain text: HTML tags are removed, mentions keep their names, emoji keep their
	// characters, and block elements become line breaks.
	Text string `json:"text"`
	// IsTextTruncated reports that Text stopped at the requested maximum number of characters.
	IsTextTruncated bool `json:"isTextTruncated,omitempty"`
	// WebURL opens the message in Microsoft Teams.
	WebURL string `json:"webUrl,omitempty"`
}

// PostMessageOutput identifies a posted message. On providerRejected, uncertain, and defect only the
// requested team, channel, root message, or chat IDs are set.
type PostMessageOutput struct {
	// MessageID is the new message's ID.
	MessageID string `json:"messageId,omitempty"`
	// TeamID is the team of a channel message or reply.
	TeamID string `json:"teamId,omitempty"`
	// ChannelID is the channel of a channel message or reply.
	ChannelID string `json:"channelId,omitempty"`
	// ReplyToID is the root message a reply belongs to.
	ReplyToID string `json:"replyToId,omitempty"`
	// ChatID is the chat of a chat message.
	ChatID string `json:"chatId,omitempty"`
	// CreatedAt is when Teams stored the message.
	CreatedAt time.Time `json:"createdAt"`
	// Sender is the signed-in user Teams shows as the sender.
	Sender MessageSender `json:"sender"`
	// WebURL opens the message in Microsoft Teams.
	WebURL string `json:"webUrl,omitempty"`
	// IsConfirmedByReadBack reports that an earlier attempt sent the message without a confirmed outcome
	// and the connector found it by reading the channel or thread back instead of sending it again.
	IsConfirmedByReadBack bool `json:"isConfirmedByReadBack,omitempty"`
}

// chatMessageCreateBody is the Graph chatMessage a post creates.
type chatMessageCreateBody struct {
	Subject    string              `json:"subject,omitempty"`
	Importance MessageImportance   `json:"importance,omitempty"`
	Body       chatMessageBodyItem `json:"body"`
}

type chatMessageBodyItem struct {
	ContentType ContentType `json:"contentType"`
	Content     string      `json:"content"`
}

// chatMessageResource is the subset of a Graph chatMessage the connector reads.
type chatMessageResource struct {
	ID                 string               `json:"id"`
	ReplyToID          *string              `json:"replyToId"`
	MessageType        string               `json:"messageType"`
	CreatedDateTime    string               `json:"createdDateTime"`
	LastEditedDateTime *string              `json:"lastEditedDateTime"`
	DeletedDateTime    *string              `json:"deletedDateTime"`
	Subject            *string              `json:"subject"`
	Importance         string               `json:"importance"`
	WebURL             *string              `json:"webUrl"`
	ChatID             *string              `json:"chatId"`
	From               *chatMessageFrom     `json:"from"`
	Body               *chatMessageBodyItem `json:"body"`
	ChannelIdentity    *channelIdentity     `json:"channelIdentity"`
}

type chatMessageFrom struct {
	User        *graphIdentity `json:"user"`
	Application *graphIdentity `json:"application"`
}

type graphIdentity struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

type channelIdentity struct {
	TeamID    string `json:"teamId"`
	ChannelID string `json:"channelId"`
}

type chatMessageCollection struct {
	Value    []chatMessageResource `json:"value"`
	NextLink string                `json:"@odata.nextLink"`
}

// messageContent is a validated body with the text a read-back compares against.
type messageContent struct {
	body           chatMessageCreateBody
	comparisonText string
}

// buildMessageContent validates the body and its size; subject applies only to a root channel message.
func buildMessageContent(content string, contentType ContentType, importance MessageImportance, subject string, maxMessageBytes int64) (messageContent, error) {
	if contentType == "" {
		contentType = ContentTypeText
	}
	switch {
	case contentType != ContentTypeText && contentType != ContentTypeHTML:
		return messageContent{}, errors.New("contentType must be text or html")
	case importance != "" && importance != MessageImportanceNormal && importance != MessageImportanceHigh && importance != MessageImportanceUrgent:
		return messageContent{}, errors.New("importance must be normal, high, or urgent")
	case !utf8.ValidString(content) || !utf8.ValidString(subject):
		return messageContent{}, errors.New("content and subject must be valid UTF-8")
	case strings.TrimSpace(content) == "":
		return messageContent{}, errors.New("content is required")
	case strings.ContainsAny(subject, "\r\n"):
		return messageContent{}, errors.New("subject must be one line")
	case utf8.RuneCountInString(subject) > maximumSubjectCharacters:
		return messageContent{}, fmt.Errorf("subject must be at most %d characters", maximumSubjectCharacters)
	case int64(len(content)+len(subject)) > maxMessageBytes:
		return messageContent{}, fmt.Errorf("content and subject exceed the connection's maxMessageBytes limit of %d bytes", maxMessageBytes)
	}
	text := extractMessageText(contentType, content)
	if strings.TrimSpace(text) == "" {
		return messageContent{}, errors.New("content has no visible text")
	}
	return messageContent{
		body:           chatMessageCreateBody{Subject: subject, Importance: importance, Body: chatMessageBodyItem{ContentType: contentType, Content: content}},
		comparisonText: comparisonTextOf(text),
	}, nil
}

// decodeMessage converts one Graph chatMessage; an invalid ID or creation time makes the message unusable.
func decodeMessage(resource chatMessageResource, maxTextCharacters int) (Message, error) {
	if !messageIDPattern.MatchString(resource.ID) {
		return Message{}, errors.New("message ID is invalid")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, resource.CreatedDateTime)
	if err != nil {
		return Message{}, errors.New("message createdDateTime is invalid")
	}
	message := Message{
		ID: resource.ID, MessageType: boundedToken(resource.MessageType), CreatedAt: createdAt.UTC(),
		IsDeleted: resource.DeletedDateTime != nil, Sender: decodeSender(resource.From),
		Importance: MessageImportance(boundedToken(resource.Importance)),
	}
	if resource.ReplyToID != nil && messageIDPattern.MatchString(*resource.ReplyToID) {
		message.ReplyToID = *resource.ReplyToID
	}
	if resource.LastEditedDateTime != nil {
		if editedAt, err := time.Parse(time.RFC3339Nano, *resource.LastEditedDateTime); err == nil {
			editedAt = editedAt.UTC()
			message.LastEditedAt = &editedAt
		}
	}
	if resource.Subject != nil {
		message.Subject, _ = truncateCharacters(stripControlCharacters(*resource.Subject), maximumSubjectCharacters)
	}
	if resource.WebURL != nil {
		message.WebURL = safeWebURL(*resource.WebURL)
	}
	if resource.Body != nil && !message.IsDeleted {
		message.ContentType = ContentType(boundedToken(string(resource.Body.ContentType)))
		message.Text, message.IsTextTruncated = truncateCharacters(extractMessageText(resource.Body.ContentType, resource.Body.Content), maxTextCharacters)
	}
	return message, nil
}

func decodeSender(from *chatMessageFrom) MessageSender {
	var sender MessageSender
	var identity *graphIdentity
	switch {
	case from == nil:
		return sender
	case from.User != nil && isSafeProviderToken(from.User.ID):
		identity, sender.UserID = from.User, from.User.ID
	case from.Application != nil && isSafeProviderToken(from.Application.ID):
		identity, sender.ApplicationID = from.Application, from.Application.ID
	default:
		return sender
	}
	sender.DisplayName, _ = truncateCharacters(stripControlCharacters(identity.DisplayName), maximumDisplayNameCharacters)
	return sender
}

// postedOutput fills the requested IDs with the message Graph stored.
func postedOutput(requested PostMessageOutput, message Message, isConfirmedByReadBack bool) PostMessageOutput {
	output := requested
	output.MessageID, output.CreatedAt, output.Sender, output.WebURL = message.ID, message.CreatedAt, message.Sender, message.WebURL
	output.IsConfirmedByReadBack = isConfirmedByReadBack
	return output
}

// extractMessageText returns a body as plain text; text bodies are kept, HTML bodies keep only visible text.
func extractMessageText(contentType ContentType, content string) string {
	if contentType != ContentTypeHTML {
		return strings.TrimSpace(strings.ReplaceAll(content, "\r\n", "\n"))
	}
	var text messageTextBuilder
	tokenizer := html.NewTokenizer(strings.NewReader(content))
	skippedElementDepth := 0
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return collapseTextLines(text.builtText())
		case html.TextToken:
			if skippedElementDepth == 0 {
				text.writeText(string(tokenizer.Text()))
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			switch token.DataAtom {
			case atom.Script, atom.Style:
				if token.Type == html.StartTagToken {
					skippedElementDepth++
				}
			case atom.Br:
				text.writeLineBreak()
			case atom.Img:
				text.writeText(attributeValue(token, "alt"))
			case 0:
				// Teams renders emoji as <emoji alt="👍">; keep the character an acknowledgement may use.
				if token.Data == "emoji" {
					text.writeText(attributeValue(token, "alt"))
				}
			default:
				if isBlockElement(token.DataAtom) {
					text.writeBlockBoundary()
				}
			}
		case html.EndTagToken:
			token := tokenizer.Token()
			switch {
			case token.DataAtom == atom.Script || token.DataAtom == atom.Style:
				skippedElementDepth = max(0, skippedElementDepth-1)
			case isBlockElement(token.DataAtom):
				text.writeBlockBoundary()
			}
		}
	}
}

// messageTextBuilder joins adjacent block boundaries, such as </p><p>, into one line break.
type messageTextBuilder struct {
	builder       strings.Builder
	isAtLineStart bool
}

// writeText follows HTML whitespace rules: a newline in text is a space, and only <br> and blocks break lines.
func (text *messageTextBuilder) writeText(value string) {
	value = htmlWhitespaceReplacer.Replace(value)
	text.builder.WriteString(value)
	if strings.TrimSpace(value) != "" {
		text.isAtLineStart = false
	}
}

// writeLineBreak always breaks, so consecutive <br> elements keep a blank line.
func (text *messageTextBuilder) writeLineBreak() {
	text.builder.WriteByte('\n')
	text.isAtLineStart = true
}

func (text *messageTextBuilder) writeBlockBoundary() {
	if text.builder.Len() > 0 && !text.isAtLineStart {
		text.writeLineBreak()
	}
}

func (text *messageTextBuilder) builtText() string { return text.builder.String() }

func isBlockElement(element atom.Atom) bool {
	switch element {
	case atom.P, atom.Div, atom.Li, atom.Tr, atom.Blockquote, atom.Pre, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Ul, atom.Ol, atom.Table:
		return true
	default:
		return false
	}
}

func attributeValue(token html.Token, name string) string {
	for _, attribute := range token.Attr {
		if attribute.Key == name {
			return attribute.Val
		}
	}
	return ""
}

// collapseTextLines trims each line, joins runs of spaces, and keeps at most one blank line between paragraphs.
func collapseTextLines(text string) string {
	var lines []string
	isPreviousBlank := true
	for _, line := range strings.Split(text, "\n") {
		collapsed := strings.Join(strings.Fields(line), " ")
		if collapsed == "" {
			if !isPreviousBlank {
				lines = append(lines, "")
			}
			isPreviousBlank = true
			continue
		}
		lines = append(lines, collapsed)
		isPreviousBlank = false
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// comparisonTextOf reduces text to single-spaced words, so a read-back ignores Teams' whitespace and markup changes.
func comparisonTextOf(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func truncateCharacters(text string, maximum int) (string, bool) {
	if maximum <= 0 || utf8.RuneCountInString(text) <= maximum {
		return text, false
	}
	runes := []rune(text)
	return string(runes[:maximum]), true
}

func stripControlCharacters(text string) string {
	return strings.Map(func(character rune) rune {
		if unicode.IsControl(character) || character == utf8.RuneError {
			return -1
		}
		return character
	}, text)
}

// boundedToken keeps a short enum-like Graph value such as message or high, and drops anything else.
func boundedToken(value string) string {
	if len(value) > 64 || !isSafeProviderToken(value) {
		return ""
	}
	return value
}

// safeWebURL keeps an absolute HTTPS Teams link and drops anything else.
func safeWebURL(value string) string {
	if len(value) > maximumWebURLBytes || !strings.HasPrefix(value, "https://") {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || !isSafeProviderURL(value) {
		return ""
	}
	return value
}

func isSafeProviderURL(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] <= ' ' || value[index] > '~' {
			return false
		}
	}
	return true
}

func validateTeamAndChannel(teamID string, channelID string) error {
	if !teamIDPattern.MatchString(teamID) {
		return errors.New("teamId must be a team GUID such as fbe2bf47-16c8-47cf-b4a5-4b9b187c508b")
	}
	if !channelIDPattern.MatchString(channelID) {
		return errors.New("channelId must be a channel ID such as 19:4a95f7d8db4c4e7fae857bcebe0623e6@thread.tacv2")
	}
	return nil
}

func validateMessageID(fieldName string, messageID string) error {
	if !messageIDPattern.MatchString(messageID) {
		return fmt.Errorf("%s must be a decimal Teams message ID such as 1616989510408", fieldName)
	}
	return nil
}

func channelMessagesPath(teamID string, channelID string) string {
	return "/teams/" + teamID + "/channels/" + channelID + "/messages"
}

func threadRepliesPath(teamID string, channelID string, messageID string) string {
	return channelMessagesPath(teamID, channelID) + "/" + messageID + "/replies"
}
