// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resend

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListEmailAttachmentsInput selects a page of sent-email attachments.
type ListEmailAttachmentsInput struct {
	// EmailID is the sent email ID.
	EmailID string `json:"emailId"`
	// Limit is the page size, from 1 through 100; zero uses Resend's default.
	Limit int `json:"limit,omitempty"`
	// After is the exclusive attachment ID cursor.
	After string `json:"after,omitempty"`
}

// GetEmailAttachmentInput selects a sent-email attachment.
type GetEmailAttachmentInput struct {
	// EmailID is the sent email ID.
	EmailID string `json:"emailId"`
	// AttachmentID is the attachment ID.
	AttachmentID string `json:"attachmentId"`
}

// ListReceivedEmailsInput selects a received-email page.
type ListReceivedEmailsInput struct {
	// Limit is the page size, from 1 through 100; zero uses Resend's default.
	Limit int `json:"limit,omitempty"`
	// After is the exclusive received-email ID cursor.
	After string `json:"after,omitempty"`
}

// GetReceivedEmailInput selects a received email.
type GetReceivedEmailInput struct {
	// ID is the received email ID.
	ID string `json:"id"`
}

// ListReceivedEmailAttachmentsInput selects a page of received-email attachments.
type ListReceivedEmailAttachmentsInput struct {
	// EmailID is the received email ID.
	EmailID string `json:"emailId"`
	// Limit is the page size, from 1 through 100; zero uses Resend's default.
	Limit int `json:"limit,omitempty"`
	// After is the exclusive attachment ID cursor.
	After string `json:"after,omitempty"`
}

// GetReceivedEmailAttachmentInput selects a received-email attachment.
type GetReceivedEmailAttachmentInput struct {
	// EmailID is the received email ID.
	EmailID string `json:"emailId"`
	// AttachmentID is the attachment ID.
	AttachmentID string `json:"attachmentId"`
}

// GetEmailMetricsInput filters aggregate email metrics.
type GetEmailMetricsInput struct {
	// StartDate is the inclusive ISO 8601 date or datetime; blank uses Resend's default.
	StartDate string `json:"startDate,omitempty"`
	// EndDate is the exclusive ISO 8601 date or datetime; blank means now.
	EndDate string `json:"endDate,omitempty"`
	// Timezone is an IANA timezone; blank uses UTC.
	Timezone string `json:"timezone,omitempty"`
	// Granularity is hourly, daily, weekly, or monthly; blank uses daily.
	Granularity string `json:"granularity,omitempty"`
	// Metrics names the requested measures; empty uses all available measures.
	Metrics []string `json:"metrics,omitempty"`
	// Dimensions names period, domain, email, or broadcast groupings.
	Dimensions []string `json:"dimensions,omitempty"`
	// DomainIDs filters to at most 100 sending domain IDs.
	DomainIDs []string `json:"domainIds,omitempty"`
	// EmailIDs filters to at most 100 email IDs.
	EmailIDs []string `json:"emailIds,omitempty"`
	// BroadcastIDs filters to at most 100 broadcast IDs.
	BroadcastIDs []string `json:"broadcastIds,omitempty"`
}

// EmailAttachment contains metadata but omits Resend's signed download URL.
type EmailAttachment struct {
	// ID is the attachment ID.
	ID string `json:"id"`
	// Filename is the optional original filename.
	Filename string `json:"filename"`
	// ContentType is the MIME content type.
	ContentType string `json:"content_type"`
	// ContentID identifies an inline attachment.
	ContentID string `json:"content_id"`
	// ContentDisposition is inline or attachment.
	ContentDisposition string `json:"content_disposition"`
	// Size is the attachment size in bytes.
	Size int64 `json:"size"`
}

// EmailAttachmentPage contains attachment metadata and a continuation flag.
type EmailAttachmentPage struct {
	// Data contains attachment metadata without signed download URLs.
	Data []EmailAttachment `json:"data"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"has_more"`
}

// ReceivedEmail contains safe metadata for a received email.
type ReceivedEmail struct {
	// ID is the received email ID.
	ID string `json:"id"`
	// From is the sender address.
	From string `json:"from"`
	// To contains recipient addresses.
	To []string `json:"to"`
	// Subject is the email subject.
	Subject string `json:"subject"`
	// MessageID is the original email Message-ID header.
	MessageID string `json:"message_id"`
	// CreatedAt is the provider receive timestamp.
	CreatedAt string `json:"created_at"`
	// Attachments contains safe attachment metadata.
	Attachments []EmailAttachment `json:"attachments"`
}

// ReceivedEmailPage contains one received-email page.
type ReceivedEmailPage struct {
	// Data contains received-email metadata.
	Data []ReceivedEmail `json:"data"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"has_more"`
}

// EmailMetrics contains aggregate measures and dimensional rows.
type EmailMetrics struct {
	// StartDate is the effective beginning of the range.
	StartDate string `json:"start_date"`
	// EndDate is the effective end of the range.
	EndDate string `json:"end_date"`
	// Metrics lists returned measure names.
	Metrics []string `json:"metrics"`
	// Dimensions lists returned groupings.
	Dimensions []string `json:"dimensions"`
	// Granularity is the effective period bucket size.
	Granularity string `json:"granularity"`
	// Totals contains measure values for the whole range.
	Totals map[string]float64 `json:"totals"`
	// Data contains dimensional rows with provider-defined measure keys.
	Data []map[string]any `json:"data,omitempty"`
}

// ListEmailAttachmentsOperation lists sent-email attachments.
type ListEmailAttachmentsOperation struct{ client *Client }

// GetEmailAttachmentOperation retrieves one sent-email attachment.
type GetEmailAttachmentOperation struct{ client *Client }

// ListReceivedEmailsOperation lists received emails.
type ListReceivedEmailsOperation struct{ client *Client }

// GetReceivedEmailOperation retrieves one received email.
type GetReceivedEmailOperation struct{ client *Client }

// ListReceivedEmailAttachmentsOperation lists received-email attachments.
type ListReceivedEmailAttachmentsOperation struct{ client *Client }

// GetReceivedEmailAttachmentOperation retrieves one received-email attachment.
type GetReceivedEmailAttachmentOperation struct{ client *Client }

// GetEmailMetricsOperation retrieves aggregate email metrics.
type GetEmailMetricsOperation struct{ client *Client }

// ListEmailAttachments binds sent attachment listing to this client.
func (client *Client) ListEmailAttachments() ListEmailAttachmentsOperation {
	return ListEmailAttachmentsOperation{client}
}

// GetEmailAttachment binds sent attachment retrieval to this client.
func (client *Client) GetEmailAttachment() GetEmailAttachmentOperation {
	return GetEmailAttachmentOperation{client}
}

// ListReceivedEmails binds received-email listing to this client.
func (client *Client) ListReceivedEmails() ListReceivedEmailsOperation {
	return ListReceivedEmailsOperation{client}
}

// GetReceivedEmail binds received-email retrieval to this client.
func (client *Client) GetReceivedEmail() GetReceivedEmailOperation {
	return GetReceivedEmailOperation{client}
}

// ListReceivedEmailAttachments binds received attachment listing to this client.
func (client *Client) ListReceivedEmailAttachments() ListReceivedEmailAttachmentsOperation {
	return ListReceivedEmailAttachmentsOperation{client}
}

// GetReceivedEmailAttachment binds received attachment retrieval to this client.
func (client *Client) GetReceivedEmailAttachment() GetReceivedEmailAttachmentOperation {
	return GetReceivedEmailAttachmentOperation{client}
}

// GetEmailMetrics binds email metrics retrieval to this client.
func (client *Client) GetEmailMetrics() GetEmailMetricsOperation {
	return GetEmailMetricsOperation{client}
}

// Definition returns the generated listEmailAttachments contract.
func (ListEmailAttachmentsOperation) Definition() sdkgo.QueryDefinition {
	return ListEmailAttachmentsDefinition
}

// Definition returns the generated getEmailAttachment contract.
func (GetEmailAttachmentOperation) Definition() sdkgo.QueryDefinition {
	return GetEmailAttachmentDefinition
}

// Definition returns the generated listReceivedEmails contract.
func (ListReceivedEmailsOperation) Definition() sdkgo.QueryDefinition {
	return ListReceivedEmailsDefinition
}

// Definition returns the generated getReceivedEmail contract.
func (GetReceivedEmailOperation) Definition() sdkgo.QueryDefinition {
	return GetReceivedEmailDefinition
}

// Definition returns the generated listReceivedEmailAttachments contract.
func (ListReceivedEmailAttachmentsOperation) Definition() sdkgo.QueryDefinition {
	return ListReceivedEmailAttachmentsDefinition
}

// Definition returns the generated getReceivedEmailAttachment contract.
func (GetReceivedEmailAttachmentOperation) Definition() sdkgo.QueryDefinition {
	return GetReceivedEmailAttachmentDefinition
}

// Definition returns the generated getEmailMetrics contract.
func (GetEmailMetricsOperation) Definition() sdkgo.QueryDefinition { return GetEmailMetricsDefinition }

// Invoke lists sent-email attachment metadata.
func (operation ListEmailAttachmentsOperation) Invoke(call sdkgo.Call, input ListEmailAttachmentsInput) sdkgo.QueryAttempt[EmailAttachmentPage] {
	return listEmailAttachments(operation.client, call, "listEmailAttachments", input.EmailID, input.Limit, input.After, false, ListEmailAttachmentsBranchListed, ListEmailAttachmentsBranchProviderRejected, ListEmailAttachmentsBranchInvalidResponse, ListEmailAttachmentsBranchDefect)
}

// Invoke retrieves sent-email attachment metadata.
func (operation GetEmailAttachmentOperation) Invoke(call sdkgo.Call, input GetEmailAttachmentInput) sdkgo.QueryAttempt[EmailAttachment] {
	return getEmailAttachment(operation.client, call, "getEmailAttachment", input.EmailID, input.AttachmentID, false, GetEmailAttachmentBranchFound, GetEmailAttachmentBranchNotFound, GetEmailAttachmentBranchProviderRejected, GetEmailAttachmentBranchInvalidResponse, GetEmailAttachmentBranchDefect)
}

// Invoke lists received-email metadata.
func (operation ListReceivedEmailsOperation) Invoke(call sdkgo.Call, input ListReceivedEmailsInput) sdkgo.QueryAttempt[ReceivedEmailPage] {
	query, err := listQuery(input.Limit, input.After, false)
	if err != nil {
		return queryFailure[ReceivedEmailPage]("listReceivedEmails", ListReceivedEmailsBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	return runQuery(operation.client, call, "listReceivedEmails", "/emails/receiving"+query, ListReceivedEmailsBranchListed, "", ListReceivedEmailsBranchProviderRejected, ListReceivedEmailsBranchInvalidResponse, ListReceivedEmailsBranchDefect, decodeReceivedEmailPage)
}

// Invoke retrieves received-email metadata.
func (operation GetReceivedEmailOperation) Invoke(call sdkgo.Call, input GetReceivedEmailInput) sdkgo.QueryAttempt[ReceivedEmail] {
	if strings.TrimSpace(input.ID) == "" {
		return queryFailure[ReceivedEmail]("getReceivedEmail", GetReceivedEmailBranchDefect, sdkgo.FailureValidation, "received email ID is required")
	}
	return runQuery(operation.client, call, "getReceivedEmail", "/emails/receiving/"+url.PathEscape(input.ID), GetReceivedEmailBranchFound, GetReceivedEmailBranchNotFound, GetReceivedEmailBranchProviderRejected, GetReceivedEmailBranchInvalidResponse, GetReceivedEmailBranchDefect, decodeReceivedEmail)
}

// Invoke lists received-email attachment metadata.
func (operation ListReceivedEmailAttachmentsOperation) Invoke(call sdkgo.Call, input ListReceivedEmailAttachmentsInput) sdkgo.QueryAttempt[EmailAttachmentPage] {
	return listEmailAttachments(operation.client, call, "listReceivedEmailAttachments", input.EmailID, input.Limit, input.After, true, ListReceivedEmailAttachmentsBranchListed, ListReceivedEmailAttachmentsBranchProviderRejected, ListReceivedEmailAttachmentsBranchInvalidResponse, ListReceivedEmailAttachmentsBranchDefect)
}

// Invoke retrieves received-email attachment metadata.
func (operation GetReceivedEmailAttachmentOperation) Invoke(call sdkgo.Call, input GetReceivedEmailAttachmentInput) sdkgo.QueryAttempt[EmailAttachment] {
	return getEmailAttachment(operation.client, call, "getReceivedEmailAttachment", input.EmailID, input.AttachmentID, true, GetReceivedEmailAttachmentBranchFound, GetReceivedEmailAttachmentBranchNotFound, GetReceivedEmailAttachmentBranchProviderRejected, GetReceivedEmailAttachmentBranchInvalidResponse, GetReceivedEmailAttachmentBranchDefect)
}

// Invoke retrieves email metrics with provider-side filters.
func (operation GetEmailMetricsOperation) Invoke(call sdkgo.Call, input GetEmailMetricsInput) sdkgo.QueryAttempt[EmailMetrics] {
	query, err := emailMetricsQuery(input)
	if err != nil {
		return queryFailure[EmailMetrics]("getEmailMetrics", GetEmailMetricsBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	return runQuery(operation.client, call, "getEmailMetrics", "/emails/metrics"+query, GetEmailMetricsBranchFound, "", GetEmailMetricsBranchProviderRejected, GetEmailMetricsBranchInvalidResponse, GetEmailMetricsBranchDefect, decodeEmailMetrics)
}

func listEmailAttachments(client *Client, call sdkgo.Call, operation, emailID string, limit int, after string, received bool, listed, rejected, invalid, defect sdkgo.BranchID) sdkgo.QueryAttempt[EmailAttachmentPage] {
	if strings.TrimSpace(emailID) == "" {
		return queryFailure[EmailAttachmentPage](operation, defect, sdkgo.FailureValidation, "email ID is required")
	}
	query, err := listQuery(limit, after, false)
	if err != nil {
		return queryFailure[EmailAttachmentPage](operation, defect, sdkgo.FailureValidation, err.Error())
	}
	prefix := "/emails/"
	if received {
		prefix = "/emails/receiving/"
	}
	return runQuery(client, call, operation, prefix+url.PathEscape(emailID)+"/attachments"+query, listed, "", rejected, invalid, defect, decodeEmailAttachmentPage)
}
func getEmailAttachment(client *Client, call sdkgo.Call, operation, emailID, attachmentID string, received bool, found, notFound, rejected, invalid, defect sdkgo.BranchID) sdkgo.QueryAttempt[EmailAttachment] {
	if strings.TrimSpace(emailID) == "" || strings.TrimSpace(attachmentID) == "" {
		return queryFailure[EmailAttachment](operation, defect, sdkgo.FailureValidation, "email and attachment IDs are required")
	}
	prefix := "/emails/"
	if received {
		prefix = "/emails/receiving/"
	}
	return runQuery(client, call, operation, prefix+url.PathEscape(emailID)+"/attachments/"+url.PathEscape(attachmentID), found, notFound, rejected, invalid, defect, decodeEmailAttachment)
}
func decodeEmailAttachment(body []byte) (EmailAttachment, error) {
	var value EmailAttachment
	err := json.Unmarshal(body, &value)
	if err == nil && value.ID == "" {
		err = fmt.Errorf("attachment ID missing")
	}
	return value, err
}
func decodeEmailAttachmentPage(body []byte) (EmailAttachmentPage, error) {
	var page EmailAttachmentPage
	err := json.Unmarshal(body, &page)
	if err == nil && page.Data == nil {
		err = fmt.Errorf("attachment page data missing")
	}
	return page, err
}
func decodeReceivedEmail(body []byte) (ReceivedEmail, error) {
	var value ReceivedEmail
	err := json.Unmarshal(body, &value)
	if err == nil && (value.ID == "" || value.From == "") {
		err = fmt.Errorf("received email fields missing")
	}
	return value, err
}
func decodeReceivedEmailPage(body []byte) (ReceivedEmailPage, error) {
	var page ReceivedEmailPage
	err := json.Unmarshal(body, &page)
	if err == nil && page.Data == nil {
		err = fmt.Errorf("received email page data missing")
	}
	return page, err
}
func decodeEmailMetrics(body []byte) (EmailMetrics, error) {
	var value EmailMetrics
	err := json.Unmarshal(body, &value)
	if err == nil && (value.StartDate == "" || value.EndDate == "" || value.Totals == nil) {
		err = fmt.Errorf("email metrics fields missing")
	}
	return value, err
}
func emailMetricsQuery(input GetEmailMetricsInput) (string, error) {
	values := url.Values{}
	for _, date := range []string{input.StartDate, input.EndDate} {
		if date == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", date); err != nil {
			if _, err := time.Parse(time.RFC3339Nano, date); err != nil {
				return "", fmt.Errorf("dates must use ISO 8601")
			}
		}
	}
	if input.StartDate != "" {
		values.Set("start_date", input.StartDate)
	}
	if input.EndDate != "" {
		values.Set("end_date", input.EndDate)
	}
	if input.Timezone != "" {
		if _, err := time.LoadLocation(input.Timezone); err != nil {
			return "", fmt.Errorf("timezone must be an IANA name")
		}
		values.Set("timezone", input.Timezone)
	}
	if input.Granularity != "" {
		if !oneOf(input.Granularity, "hourly", "daily", "weekly", "monthly") {
			return "", fmt.Errorf("granularity is invalid")
		}
		values.Set("granularity", input.Granularity)
	}
	if len(input.Metrics) > 0 {
		for _, metric := range input.Metrics {
			if !oneOf(metric, "received", "delivered", "complained", "suppressed", "bounced", "bounced_transient", "bounced_permanent", "bounced_undetermined", "opened", "clicked", "unsubscribed", "delivery_delayed", "failed", "sent", "unique_opened", "unique_clicked", "delivery_rate", "open_rate", "click_rate", "bounce_rate", "complaint_rate", "unsubscribe_rate") {
				return "", fmt.Errorf("metric name is invalid")
			}
		}
		values.Set("metrics", strings.Join(input.Metrics, ","))
	}
	if len(input.Dimensions) > 0 {
		for _, dimension := range input.Dimensions {
			if !oneOf(dimension, "period", "domain", "email", "broadcast") {
				return "", fmt.Errorf("dimension is invalid")
			}
		}
		values.Set("dimensions", strings.Join(input.Dimensions, ","))
	}
	if (contains(input.Dimensions, "email") && (contains(input.Dimensions, "broadcast") || len(input.BroadcastIDs) > 0)) || (contains(input.Dimensions, "broadcast") && len(input.EmailIDs) > 0) || (len(input.EmailIDs) > 0 && len(input.BroadcastIDs) > 0) {
		return "", fmt.Errorf("email and broadcast filters cannot be combined")
	}
	for _, entry := range []struct {
		name string
		ids  []string
	}{{"domain_id", input.DomainIDs}, {"email_id", input.EmailIDs}, {"broadcast_id", input.BroadcastIDs}} {
		if len(entry.ids) > 100 {
			return "", fmt.Errorf("ID filter exceeds 100 values")
		}
		for _, id := range entry.ids {
			if strings.TrimSpace(id) == "" || strings.Contains(id, ",") {
				return "", fmt.Errorf("ID filter contains an invalid value")
			}
		}
		if len(entry.ids) > 0 {
			values.Set(entry.name, strings.Join(entry.ids, ","))
		}
	}
	if len(values) == 0 {
		return "", nil
	}
	return "?" + values.Encode(), nil
}
func oneOf(value string, choices ...string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}
func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
