// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/mail"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// TicketStatus is Zoho Desk's own ticket status name, such as Open or a custom status like
// Waiting for Customer. An administrator can add custom statuses, and each falls under one
// TicketStatusType; the organization's list, with each status's type, is the allowedValues of the
// status field in GET /api/v1/organizationFields?module=tickets. Results pass through every value.
type TicketStatus string

const (
	// TicketStatusOpen is Zoho Desk's default Open status, of type Open.
	TicketStatusOpen TicketStatus = "Open"
	// TicketStatusOnHold is Zoho Desk's default On Hold status, of type On Hold; SLA timers pause.
	TicketStatusOnHold TicketStatus = "On Hold"
	// TicketStatusEscalated is Zoho Desk's default Escalated status, of type Open.
	TicketStatusEscalated TicketStatus = "Escalated"
	// TicketStatusClosed is Zoho Desk's default Closed status, of type Closed.
	TicketStatusClosed TicketStatus = "Closed"
)

// DefaultTicketStatuses returns the statuses a new Zoho Desk organization has, in Zoho Desk's
// order: Open, On Hold, Escalated, and Closed. An organization may rename them or add more.
func DefaultTicketStatuses() []TicketStatus {
	return []TicketStatus{TicketStatusOpen, TicketStatusOnHold, TicketStatusEscalated, TicketStatusClosed}
}

// TicketStatusType is the category Zoho Desk assigns every status, default or custom.
type TicketStatusType string

const (
	// TicketStatusTypeOpen covers statuses that still need an agent, such as Open and Escalated.
	TicketStatusTypeOpen TicketStatusType = "Open"
	// TicketStatusTypeOnHold covers statuses that wait on someone else, such as On Hold.
	TicketStatusTypeOnHold TicketStatusType = "On Hold"
	// TicketStatusTypeClosed covers resolved statuses, such as Closed.
	TicketStatusTypeClosed TicketStatusType = "Closed"
)

// TicketStatusTypes returns Zoho Desk's three status types: Open, On Hold, and Closed.
func TicketStatusTypes() []TicketStatusType {
	return []TicketStatusType{TicketStatusTypeOpen, TicketStatusTypeOnHold, TicketStatusTypeClosed}
}

// TicketPriority is Zoho Desk's own priority name. Zoho Desk defines High, Medium, and Low, and an
// organization may add custom priorities; Results pass through every value.
type TicketPriority string

const (
	// TicketPriorityHigh is Zoho Desk's High priority.
	TicketPriorityHigh TicketPriority = "High"
	// TicketPriorityMedium is Zoho Desk's Medium priority.
	TicketPriorityMedium TicketPriority = "Medium"
	// TicketPriorityLow is Zoho Desk's Low priority.
	TicketPriorityLow TicketPriority = "Low"
)

// DefaultTicketPriorities returns Zoho Desk's system-defined priorities: High, Medium, and Low.
func DefaultTicketPriorities() []TicketPriority {
	return []TicketPriority{TicketPriorityHigh, TicketPriorityMedium, TicketPriorityLow}
}

// Ticket is the connector's view of one Zoho Desk ticket. Custom fields, phone numbers, shared
// departments, and blueprint details are omitted. IDs are Zoho Desk's numeric IDs as decimal
// strings, and an empty ID means Zoho Desk reported none, such as an unassigned ticket.
type Ticket struct {
	// ID is the Zoho Desk ticket ID.
	ID string `json:"id"`
	// TicketNumber is the ticket's serial number that agents see, such as 101.
	TicketNumber string `json:"ticketNumber,omitempty"`
	// Subject is the ticket subject.
	Subject string `json:"subject,omitempty"`
	// DescriptionHTML is the ticket description as the HTML Zoho Desk stores. searchTickets leaves
	// it empty to keep pages small.
	DescriptionHTML string `json:"descriptionHtml,omitempty"`
	// IsDescriptionTruncated reports that DescriptionHTML was cut at MaxTextBytes.
	IsDescriptionTruncated bool `json:"isDescriptionTruncated,omitempty"`
	// Status is Zoho Desk's status name, including custom statuses.
	Status TicketStatus `json:"status"`
	// StatusType is the status's category. The connector reports Zoho Desk's spellings OPEN,
	// ONHOLD, and CLOSED as the TicketStatusType constants and passes any other value through.
	StatusType TicketStatusType `json:"statusType,omitempty"`
	// Priority is Zoho Desk's priority name, or empty when the ticket has none.
	Priority TicketPriority `json:"priority,omitempty"`
	// Channel is the channel that created the ticket, such as Email, Web, or Phone.
	Channel string `json:"channel,omitempty"`
	// Classification is the ticket's classification, such as Problem or Question, or empty.
	Classification string `json:"classification,omitempty"`
	// DepartmentID is the ticket's department.
	DepartmentID string `json:"departmentId,omitempty"`
	// ContactID is the contact who raised the ticket.
	ContactID string `json:"contactId,omitempty"`
	// AssigneeID is the assigned agent, or empty when unassigned.
	AssigneeID string `json:"assigneeId,omitempty"`
	// TeamID is the assigned team, or empty.
	TeamID string `json:"teamId,omitempty"`
	// Email is the ticket's email address, which Zoho Desk's email search filter matches.
	Email string `json:"email,omitempty"`
	// IsEscalated reports that Zoho Desk escalated the ticket.
	IsEscalated bool `json:"isEscalated,omitempty"`
	// IsOverdue reports that the ticket is past its due date.
	IsOverdue bool `json:"isOverdue,omitempty"`
	// IsSpam reports that the ticket is marked as spam.
	IsSpam bool `json:"isSpam,omitempty"`
	// ThreadCount is Zoho Desk's count of the ticket's threads.
	ThreadCount int `json:"threadCount,omitempty"`
	// CommentCount is Zoho Desk's count of the ticket's comments.
	CommentCount int `json:"commentCount,omitempty"`
	// CreatedAt is when the ticket was created.
	CreatedAt time.Time `json:"createdAt"`
	// ModifiedAt is when the ticket last changed.
	ModifiedAt time.Time `json:"modifiedAt"`
	// DueAt is when the ticket is due, or nil.
	DueAt *time.Time `json:"dueAt,omitempty"`
	// ClosedAt is when the ticket was closed, or nil.
	ClosedAt *time.Time `json:"closedAt,omitempty"`
	// WebURL is the agent interface link to the ticket.
	WebURL string `json:"webUrl,omitempty"`
}

// IsClosed reports that the ticket's status type is Closed.
func (ticket Ticket) IsClosed() bool { return ticket.StatusType == TicketStatusTypeClosed }

// TicketContact is the contact who raised a ticket.
type TicketContact struct {
	// ID is the Zoho Desk contact ID.
	ID string `json:"id"`
	// FirstName is the contact's first name, or empty.
	FirstName string `json:"firstName,omitempty"`
	// LastName is the contact's last name, or empty.
	LastName string `json:"lastName,omitempty"`
	// Email is the contact's email address, or empty when Zoho Desk has none.
	Email string `json:"email,omitempty"`
}

// MaxTextBytes bounds each description, thread summary, and comment a Result carries, so durable
// Step state stays small. Longer text is cut at a UTF-8 boundary and flagged.
const MaxTextBytes = 16 << 10

const (
	maximumSubjectRunes      = 255
	maximumDescriptionRunes  = 65535
	maximumPicklistRunes     = 120
	maximumContactNameRunes  = 80
	zohoTimestampWireLayout  = "2006-01-02T15:04:05.000Z"
	searchTermForbiddenRunes = ",*${}"
)

// zohoID decodes a Zoho Desk ID that arrives as a JSON string, as documented, or as a number.
type zohoID string

// UnmarshalJSON accepts a numeric ID as a JSON string or number, and null as no ID.
func (id *zohoID) UnmarshalJSON(contents []byte) error {
	value, err := decodeStringOrNumber(contents)
	if err != nil {
		return errors.New("ID is neither a string nor a number")
	}
	if value != "" && !zohoIDPattern.MatchString(value) {
		return errors.New("ID is not a numeric Zoho Desk ID")
	}
	*id = zohoID(value)
	return nil
}

// zohoCount decodes a count that Zoho Desk sends as a string such as "121" or as a number.
type zohoCount int

// UnmarshalJSON accepts a non-negative count as a JSON string or number, and null as zero.
func (count *zohoCount) UnmarshalJSON(contents []byte) error {
	value, err := decodeStringOrNumber(contents)
	if err != nil {
		return errors.New("count is neither a string nor a number")
	}
	if value == "" {
		*count = 0
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return errors.New("count is not a non-negative integer")
	}
	*count = zohoCount(parsed)
	return nil
}

// zohoBoolean decodes a flag that Zoho Desk sends as a JSON boolean or as "true" or "false".
type zohoBoolean bool

// UnmarshalJSON accepts true and false as JSON booleans or strings, and null or "" as false.
func (flag *zohoBoolean) UnmarshalJSON(contents []byte) error {
	switch string(bytes.TrimSpace(contents)) {
	case "true", `"true"`:
		*flag = true
	case "false", `"false"`, "null", `""`:
		*flag = false
	default:
		return errors.New("flag is not a boolean")
	}
	return nil
}

func decodeStringOrNumber(contents []byte) (string, error) {
	trimmed := bytes.TrimSpace(contents)
	if string(trimmed) == "null" {
		return "", nil
	}
	var text string
	if err := json.Unmarshal(trimmed, &text); err == nil {
		return text, nil
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		return "", err
	}
	return number.String(), nil
}

// deskTicketWire is the ticket JSON the connector reads; Zoho Desk's nulls decode as zero values.
type deskTicketWire struct {
	ID             zohoID           `json:"id"`
	TicketNumber   string           `json:"ticketNumber"`
	Subject        string           `json:"subject"`
	Description    string           `json:"description"`
	Status         string           `json:"status"`
	StatusType     string           `json:"statusType"`
	Priority       string           `json:"priority"`
	Channel        string           `json:"channel"`
	Classification string           `json:"classification"`
	DepartmentID   zohoID           `json:"departmentId"`
	ContactID      zohoID           `json:"contactId"`
	AssigneeID     zohoID           `json:"assigneeId"`
	TeamID         zohoID           `json:"teamId"`
	Email          string           `json:"email"`
	IsEscalated    zohoBoolean      `json:"isEscalated"`
	IsOverDue      zohoBoolean      `json:"isOverDue"`
	IsSpam         zohoBoolean      `json:"isSpam"`
	ThreadCount    zohoCount        `json:"threadCount"`
	CommentCount   zohoCount        `json:"commentCount"`
	CreatedTime    string           `json:"createdTime"`
	ModifiedTime   string           `json:"modifiedTime"`
	DueDate        string           `json:"dueDate"`
	ClosedTime     string           `json:"closedTime"`
	WebURL         string           `json:"webUrl"`
	Contact        *deskContactWire `json:"contact"`
}

type deskContactWire struct {
	ID        zohoID `json:"id"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
}

// decodeTicketWire validates one ticket and converts it to the connector's view.
func decodeTicketWire(wire deskTicketWire) (Ticket, error) {
	if wire.ID == "" {
		return Ticket{}, errors.New("ticket ID is missing")
	}
	if strings.TrimSpace(wire.Status) == "" {
		return Ticket{}, errors.New("ticket status is missing")
	}
	createdAt, err := parseZohoTimestamp("createdTime", wire.CreatedTime)
	if err != nil {
		return Ticket{}, err
	}
	modifiedAt, err := parseZohoTimestamp("modifiedTime", wire.ModifiedTime)
	if err != nil {
		return Ticket{}, err
	}
	description, isDescriptionTruncated := truncateUTF8(wire.Description, MaxTextBytes)
	ticket := Ticket{
		ID: string(wire.ID), TicketNumber: wire.TicketNumber, Subject: wire.Subject,
		DescriptionHTML: description, IsDescriptionTruncated: isDescriptionTruncated,
		Status: TicketStatus(wire.Status), StatusType: statusTypeFromWire(wire.StatusType), Priority: TicketPriority(wire.Priority),
		Channel: wire.Channel, Classification: wire.Classification, DepartmentID: string(wire.DepartmentID),
		ContactID: string(wire.ContactID), AssigneeID: string(wire.AssigneeID), TeamID: string(wire.TeamID), Email: wire.Email,
		IsEscalated: bool(wire.IsEscalated), IsOverdue: bool(wire.IsOverDue), IsSpam: bool(wire.IsSpam),
		ThreadCount: int(wire.ThreadCount), CommentCount: int(wire.CommentCount),
		CreatedAt: createdAt, ModifiedAt: modifiedAt, WebURL: wire.WebURL,
	}
	if ticket.DueAt, err = parseOptionalZohoTimestamp("dueDate", wire.DueDate); err != nil {
		return Ticket{}, err
	}
	if ticket.ClosedAt, err = parseOptionalZohoTimestamp("closedTime", wire.ClosedTime); err != nil {
		return Ticket{}, err
	}
	return ticket, nil
}

// decodeTicketBody decodes the ticket object Zoho Desk returns from ticket reads and writes.
func decodeTicketBody(body []byte, expectedTicketID string) (Ticket, *deskContactWire, error) {
	var wire deskTicketWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return Ticket{}, nil, errors.New("ticket response is not a ticket object")
	}
	ticket, err := decodeTicketWire(wire)
	if err != nil {
		return Ticket{}, nil, err
	}
	if expectedTicketID != "" && ticket.ID != expectedTicketID {
		return Ticket{}, nil, errors.New("ticket response is for another ticket")
	}
	return ticket, wire.Contact, nil
}

func contactFromWire(wire *deskContactWire) *TicketContact {
	if wire == nil || wire.ID == "" {
		return nil
	}
	return &TicketContact{ID: string(wire.ID), FirstName: wire.FirstName, LastName: wire.LastName, Email: wire.Email}
}

// statusTypeFromWire reports Zoho Desk's documented spellings, such as ONHOLD and On Hold, as one constant.
func statusTypeFromWire(value string) TicketStatusType {
	switch strings.ToUpper(strings.ReplaceAll(value, " ", "")) {
	case "OPEN":
		return TicketStatusTypeOpen
	case "ONHOLD":
		return TicketStatusTypeOnHold
	case "CLOSED":
		return TicketStatusTypeClosed
	default:
		return TicketStatusType(value)
	}
}

func parseZohoTimestamp(fieldName string, value string) (time.Time, error) {
	instant, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s is not an ISO 8601 timestamp", fieldName)
	}
	return instant.UTC(), nil
}

func parseOptionalZohoTimestamp(fieldName string, value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	instant, err := parseZohoTimestamp(fieldName, value)
	if err != nil {
		return nil, err
	}
	return &instant, nil
}

// plainTextToZohoHTML escapes text for Zoho Desk's HTML description, keeping line breaks as <br>.
func plainTextToZohoHTML(text string) string {
	escaped := html.EscapeString(strings.ReplaceAll(text, "\r\n", "\n"))
	return strings.ReplaceAll(escaped, "\n", "<br>")
}

func truncateUTF8(value string, maxBytes int) (string, bool) {
	if len(value) <= maxBytes {
		return value, false
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut], true
}

// validatePicklistValue accepts one status or priority name that cannot change a search or a write.
func validatePicklistValue(fieldName string, value string) error {
	if value == "" || value != strings.TrimSpace(value) || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maximumPicklistRunes {
		return fmt.Errorf("%s %q must be 1 to %d characters without surrounding spaces", fieldName, value, maximumPicklistRunes)
	}
	for _, character := range value {
		if unicode.IsControl(character) || strings.ContainsRune(searchTermForbiddenRunes, character) {
			return fmt.Errorf("%s %q cannot contain control characters or any of , * $ { }", fieldName, value)
		}
	}
	return nil
}

func validateTextInput(fieldName string, text string, maximumRunes int) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("%s is required", fieldName)
	}
	if !utf8.ValidString(text) {
		return fmt.Errorf("%s must be UTF-8 text", fieldName)
	}
	if utf8.RuneCountInString(text) > maximumRunes {
		return fmt.Errorf("%s is longer than Zoho Desk's %d-character limit", fieldName, maximumRunes)
	}
	return nil
}

func validateContactName(fieldName string, name string) error {
	if name != strings.TrimSpace(name) || !utf8.ValidString(name) || utf8.RuneCountInString(name) > maximumContactNameRunes {
		return fmt.Errorf("%s must be at most %d characters without surrounding spaces", fieldName, maximumContactNameRunes)
	}
	return nil
}

// isBareEmailAddress accepts one address that also cannot change a Zoho Desk search term.
func isBareEmailAddress(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Name == "" && address.Address == value && !strings.ContainsAny(value, " \"'()<>;"+searchTermForbiddenRunes)
}

func formatZohoTimestamp(instant time.Time) string {
	return instant.UTC().Format(zohoTimestampWireLayout)
}
