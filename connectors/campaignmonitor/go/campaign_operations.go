// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package campaignmonitor

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// ListCampaignsInput selects draft, scheduled, or sent campaigns for a client.
type ListCampaignsInput struct {
	// ClientID is returned by ListClients.
	ClientID string `json:"clientId"`
	// Status is draft, scheduled, or sent; blank uses draft.
	Status string `json:"status,omitempty"`
	// PageNumber is one-based for sent campaigns; zero uses the first page.
	PageNumber int `json:"pageNumber,omitempty"`
	// PageSize is 1 to 1000 for sent campaigns; zero uses 100.
	PageSize int `json:"pageSize,omitempty"`
}

// CampaignProfile identifies a campaign without sender addresses or content.
type CampaignProfile struct {
	// ID is the opaque campaign ID.
	ID string `json:"id"`
	// Name is the campaign's internal name.
	Name string `json:"name"`
	// Subject is the email subject.
	Subject string `json:"subject"`
	// Status is draft, scheduled, or sent.
	Status string `json:"status"`
}

// CampaignsPage contains campaigns and pagination counts.
type CampaignsPage struct {
	// Campaigns are the selected campaign identities.
	Campaigns []CampaignProfile `json:"campaigns"`
	// PageNumber is the returned page number.
	PageNumber int `json:"pageNumber"`
	// PageSize is the returned page size.
	PageSize int `json:"pageSize"`
	// TotalRecords is the count across all pages.
	TotalRecords int `json:"totalRecords"`
	// NumberOfPages is the available page count.
	NumberOfPages int `json:"numberOfPages"`
}

// GetCampaignSummaryInput selects one sent campaign.
type GetCampaignSummaryInput struct {
	// CampaignID is returned by ListCampaigns or CreateDraftCampaign.
	CampaignID string `json:"campaignId"`
}

// CampaignSummary contains aggregate sent campaign metrics.
type CampaignSummary struct {
	// Name is the campaign's internal name.
	Name string `json:"name"`
	// Recipients is the provider recipient count.
	Recipients int `json:"recipients"`
	// TotalOpened is the total opens reported by the provider.
	TotalOpened int `json:"totalOpened"`
	// Clicks is the total clicks reported by the provider.
	Clicks int `json:"clicks"`
	// Unsubscribed is the number of unsubscribes.
	Unsubscribed int `json:"unsubscribed"`
}

// CreateDraftCampaignInput creates a draft from hosted HTML content.
type CreateDraftCampaignInput struct {
	// ClientID is returned by ListClients.
	ClientID string `json:"clientId"`
	// Name is unique among the client's campaigns.
	Name string `json:"name"`
	// Subject is the nonempty email subject.
	Subject string `json:"subject"`
	// FromName is the sender's display name.
	FromName string `json:"fromName"`
	// FromEmail is a complete sender email address.
	FromEmail string `json:"fromEmail"`
	// ReplyTo is a complete reply-to email address.
	ReplyTo string `json:"replyTo"`
	// HTMLURL is an HTTPS URL for Campaign Monitor to import email HTML.
	HTMLURL string `json:"htmlUrl"`
	// ListIDs are recipient list IDs.
	ListIDs []string `json:"listIds,omitempty"`
	// SegmentIDs are recipient segment IDs.
	SegmentIDs []string `json:"segmentIds,omitempty"`
	// InlineCSS overrides the provider's default when nonnil.
	InlineCSS *bool `json:"inlineCss,omitempty"`
}

// SendCampaignInput sends one draft campaign immediately.
type SendCampaignInput struct {
	// ClientID owns the campaign and enables read-back after an unknown send.
	ClientID string `json:"clientId"`
	// CampaignID is returned by CreateDraftCampaign or ListCampaigns.
	CampaignID string `json:"campaignId"`
	// ConfirmationEmail receives the provider's send confirmation.
	ConfirmationEmail string `json:"confirmationEmail"`
	// SendDate is YYYY-MM-DD HH:MM in the client's timezone; blank sends immediately.
	SendDate string `json:"sendDate,omitempty"`
}

// SendCampaignPreviewInput sends a draft preview to up to five addresses.
type SendCampaignPreviewInput struct {
	// CampaignID is returned by CreateDraftCampaign or ListCampaigns.
	CampaignID string `json:"campaignId"`
	// Recipients are one to five complete email addresses.
	Recipients []string `json:"recipients"`
}

// UnscheduleCampaignInput selects a scheduled campaign to move to drafts.
type UnscheduleCampaignInput struct {
	// CampaignID is returned by ListCampaigns.
	CampaignID string `json:"campaignId"`
}

// DeleteCampaignInput selects one campaign for deletion.
type DeleteCampaignInput struct {
	// CampaignID is returned by ListCampaigns.
	CampaignID string `json:"campaignId"`
}

// CampaignIdentity identifies the campaign changed by a Mutation.
type CampaignIdentity struct {
	// ID is the opaque campaign ID.
	ID string `json:"id"`
	// Name is known for draft creation; blank for send or deletion.
	Name string `json:"name"`
}

// ListCampaignsOperation reads a client's campaigns.
type ListCampaignsOperation struct{ client *Client }

// GetCampaignSummaryOperation reads sent campaign metrics.
type GetCampaignSummaryOperation struct{ client *Client }

// CreateDraftCampaignOperation creates a draft campaign.
type CreateDraftCampaignOperation struct{ client *Client }

// SendCampaignOperation sends or schedules a draft campaign.
type SendCampaignOperation struct{ client *Client }

// SendCampaignPreviewOperation sends a draft preview to selected recipients.
type SendCampaignPreviewOperation struct{ client *Client }

// UnscheduleCampaignOperation moves a scheduled campaign back to drafts.
type UnscheduleCampaignOperation struct{ client *Client }

// DeleteCampaignOperation deletes a campaign.
type DeleteCampaignOperation struct{ client *Client }

// ListCampaigns binds campaign discovery to this client.
func (client *Client) ListCampaigns() ListCampaignsOperation { return ListCampaignsOperation{client} }

// GetCampaignSummary binds sent campaign metrics to this client.
func (client *Client) GetCampaignSummary() GetCampaignSummaryOperation {
	return GetCampaignSummaryOperation{client}
}

// CreateDraftCampaign binds draft creation to this client.
func (client *Client) CreateDraftCampaign() CreateDraftCampaignOperation {
	return CreateDraftCampaignOperation{client}
}

// SendCampaign binds campaign sending or scheduling to this client.
func (client *Client) SendCampaign() SendCampaignOperation { return SendCampaignOperation{client} }

// SendCampaignPreview binds preview sending to this client.
func (client *Client) SendCampaignPreview() SendCampaignPreviewOperation {
	return SendCampaignPreviewOperation{client}
}

// UnscheduleCampaign binds unscheduling to this client.
func (client *Client) UnscheduleCampaign() UnscheduleCampaignOperation {
	return UnscheduleCampaignOperation{client}
}

// DeleteCampaign binds campaign deletion to this client.
func (client *Client) DeleteCampaign() DeleteCampaignOperation {
	return DeleteCampaignOperation{client}
}

// Definition returns the generated campaign discovery contract.
func (ListCampaignsOperation) Definition() sdkgo.QueryDefinition { return ListCampaignsDefinition }

// Definition returns the generated campaign summary contract.
func (GetCampaignSummaryOperation) Definition() sdkgo.QueryDefinition {
	return GetCampaignSummaryDefinition
}

// Definition returns the generated draft creation contract.
func (CreateDraftCampaignOperation) Definition() sdkgo.MutationDefinition {
	return CreateDraftCampaignDefinition
}

// Definition returns the generated campaign send contract.
func (SendCampaignOperation) Definition() sdkgo.MutationDefinition { return SendCampaignDefinition }

// Definition returns the generated preview send contract.
func (SendCampaignPreviewOperation) Definition() sdkgo.MutationDefinition {
	return SendCampaignPreviewDefinition
}

// Definition returns the generated unschedule contract.
func (UnscheduleCampaignOperation) Definition() sdkgo.MutationDefinition {
	return UnscheduleCampaignDefinition
}

// Definition returns the generated campaign deletion contract.
func (DeleteCampaignOperation) Definition() sdkgo.MutationDefinition { return DeleteCampaignDefinition }

// IdempotencyKey preserves identity across bounded draft creation attempts.
func (CreateDraftCampaignOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateDraftCampaignInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey preserves identity across bounded campaign send attempts.
func (SendCampaignOperation) IdempotencyKey(callID sdkgo.CallID, _ SendCampaignInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey preserves identity across bounded preview sends.
func (SendCampaignPreviewOperation) IdempotencyKey(callID sdkgo.CallID, _ SendCampaignPreviewInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey preserves identity across unscheduling attempts.
func (UnscheduleCampaignOperation) IdempotencyKey(callID sdkgo.CallID, _ UnscheduleCampaignInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey preserves identity across campaign deletions.
func (DeleteCampaignOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteCampaignInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke lists draft, scheduled, or sent campaigns for a client.
func (operation ListCampaignsOperation) Invoke(call sdkgo.Call, input ListCampaignsInput) sdkgo.QueryAttempt[CampaignsPage] {
	const name = "listCampaigns"
	if !validCampaignMonitorID(input.ClientID) || input.PageNumber < 0 || input.PageSize < 0 || input.PageSize > 1000 {
		return queryBranch(name, ListCampaignsBranchDefect, CampaignsPage{}, sdkgo.FailureValidation, "client ID or page settings are invalid")
	}
	status := input.Status
	if status == "" {
		status = "draft"
	}
	if status != "draft" && status != "scheduled" && status != "sent" {
		return queryBranch(name, ListCampaignsBranchDefect, CampaignsPage{}, sdkgo.FailureValidation, "campaign status is invalid")
	}
	if input.PageNumber == 0 {
		input.PageNumber = 1
	}
	if input.PageSize == 0 {
		input.PageSize = 100
	}
	path := "/clients/" + url.PathEscape(input.ClientID) + "/"
	switch status {
	case "draft":
		path += "drafts.json"
	case "scheduled":
		path += "scheduled.json"
	case "sent":
		path += "campaigns.json?page=" + strconv.Itoa(input.PageNumber) + "&pagesize=" + strconv.Itoa(input.PageSize)
	}
	return invokeQuery(call, operation.client, name, path, queryBranches{found: ListCampaignsBranchListed, rejected: ListCampaignsBranchProviderRejected, invalid: ListCampaignsBranchInvalidResponse, defect: ListCampaignsBranchDefect}, func(body []byte) (CampaignsPage, error) {
		type row struct {
			ID      string `json:"CampaignID"`
			Name    string `json:"Name"`
			Subject string `json:"Subject"`
		}
		var rows []row
		page := CampaignsPage{PageNumber: 1, NumberOfPages: 1}
		if status == "sent" {
			var wire struct {
				Results       []row `json:"Results"`
				PageNumber    int   `json:"PageNumber"`
				PageSize      int   `json:"PageSize"`
				TotalRecords  int   `json:"TotalNumberOfRecords"`
				NumberOfPages int   `json:"NumberOfPages"`
			}
			if err := json.Unmarshal(body, &wire); err != nil || wire.Results == nil || wire.PageNumber != input.PageNumber || wire.PageSize != input.PageSize || wire.TotalRecords < 0 || wire.NumberOfPages < 0 {
				return CampaignsPage{}, errors.New("sent campaigns are invalid")
			}
			rows = wire.Results
			page.PageNumber, page.PageSize, page.TotalRecords, page.NumberOfPages = wire.PageNumber, wire.PageSize, wire.TotalRecords, wire.NumberOfPages
		} else {
			if err := json.Unmarshal(body, &rows); err != nil || rows == nil {
				return CampaignsPage{}, errors.New("campaigns are invalid")
			}
			page.PageSize, page.TotalRecords = len(rows), len(rows)
		}
		page.Campaigns = make([]CampaignProfile, 0, len(rows))
		for _, item := range rows {
			if !validCampaignMonitorID(item.ID) || item.Name == "" {
				return CampaignsPage{}, errors.New("campaign identity is invalid")
			}
			page.Campaigns = append(page.Campaigns, CampaignProfile{ID: item.ID, Name: item.Name, Subject: item.Subject, Status: status})
		}
		return page, nil
	})
}

// Invoke reads aggregate results for one sent campaign.
func (operation GetCampaignSummaryOperation) Invoke(call sdkgo.Call, input GetCampaignSummaryInput) sdkgo.QueryAttempt[CampaignSummary] {
	const name = "getCampaignSummary"
	if !validCampaignMonitorID(input.CampaignID) {
		return queryBranch(name, GetCampaignSummaryBranchDefect, CampaignSummary{}, sdkgo.FailureValidation, "campaign ID is invalid")
	}
	return invokeQuery(call, operation.client, name, campaignPath(input.CampaignID)+"/summary.json", queryBranches{found: GetCampaignSummaryBranchFound, notFound: GetCampaignSummaryBranchNotFound, rejected: GetCampaignSummaryBranchProviderRejected, invalid: GetCampaignSummaryBranchInvalidResponse, defect: GetCampaignSummaryBranchDefect}, func(body []byte) (CampaignSummary, error) {
		var value CampaignSummary
		var wire struct {
			Name         string `json:"Name"`
			Recipients   int    `json:"Recipients"`
			TotalOpened  int    `json:"TotalOpened"`
			Clicks       int    `json:"Clicks"`
			Unsubscribed int    `json:"Unsubscribed"`
		}
		if err := json.Unmarshal(body, &wire); err != nil || wire.Name == "" || wire.Recipients < 0 || wire.TotalOpened < 0 || wire.Clicks < 0 || wire.Unsubscribed < 0 {
			return value, errors.New("campaign summary is invalid")
		}
		return CampaignSummary{Name: wire.Name, Recipients: wire.Recipients, TotalOpened: wire.TotalOpened, Clicks: wire.Clicks, Unsubscribed: wire.Unsubscribed}, nil
	})
}

// Invoke creates a draft and reads back by unique name after an unknown send.
func (operation CreateDraftCampaignOperation) Invoke(call sdkgo.Call, input CreateDraftCampaignInput) sdkgo.MutationAttempt[CampaignIdentity] {
	const name = "createDraftCampaign"
	if !validCampaignMonitorID(input.ClientID) || strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Subject) == "" || strings.TrimSpace(input.FromName) == "" || !validSubscriberEmail(input.FromEmail) || !validSubscriberEmail(input.ReplyTo) || !validOptionalListURL(input.HTMLURL) || input.HTMLURL == "" || len(input.ListIDs)+len(input.SegmentIDs) == 0 || !validCampaignIDs(input.ListIDs) || !validCampaignIDs(input.SegmentIDs) {
		return campaignMutationBranch(name, CreateDraftCampaignBranchDefect, CampaignIdentity{}, sdkgo.FailureValidation, "draft campaign fields are invalid")
	}
	payload := map[string]any{"Name": input.Name, "Subject": input.Subject, "FromName": input.FromName, "FromEmail": input.FromEmail, "ReplyTo": input.ReplyTo, "HtmlUrl": input.HTMLURL, "ListIDs": input.ListIDs, "SegmentIDs": input.SegmentIDs}
	if input.InlineCSS != nil {
		payload["InlineCss"] = *input.InlineCSS
	}
	readback := func() string { return operation.findDraftCampaign(call, input.ClientID, input.Name) }
	return operation.client.invokeLimitedCampaignMutation(call, name, "/campaigns/"+url.PathEscape(input.ClientID)+".json", payload, CreateDraftCampaignDefinition, CreateDraftCampaignBranchCreated, CreateDraftCampaignBranchProviderRejected, CreateDraftCampaignBranchDefect, http.StatusCreated, CampaignIdentity{Name: input.Name}, readback, func(body []byte) string {
		var id string
		if json.Unmarshal(body, &id) != nil || !validCampaignMonitorID(id) {
			return ""
		}
		return id
	})
}

func (operation CreateDraftCampaignOperation) findDraftCampaign(call sdkgo.Call, clientID, name string) string {
	body, status, _, err := operation.client.get(call, "/clients/"+url.PathEscape(clientID)+"/drafts.json")
	if err != nil || status != http.StatusOK {
		return ""
	}
	var rows []struct {
		ID   string `json:"CampaignID"`
		Name string `json:"Name"`
	}
	if json.Unmarshal(body, &rows) != nil {
		return ""
	}
	var found string
	for _, row := range rows {
		if row.Name == name {
			if found != "" || !validCampaignMonitorID(row.ID) {
				return ""
			}
			found = row.ID
		}
	}
	return found
}

// Invoke sends or schedules a draft with bounded retries and campaign read-back.
func (operation SendCampaignOperation) Invoke(call sdkgo.Call, input SendCampaignInput) sdkgo.MutationAttempt[CampaignIdentity] {
	const name = "sendCampaign"
	if !validCampaignMonitorID(input.ClientID) || !validCampaignMonitorID(input.CampaignID) || !validSubscriberEmail(input.ConfirmationEmail) {
		return campaignMutationBranch(name, SendCampaignBranchDefect, CampaignIdentity{}, sdkgo.FailureValidation, "client ID, campaign ID, or confirmation email is invalid")
	}
	sendDate := input.SendDate
	if sendDate == "" {
		sendDate = "Immediately"
	} else if _, err := time.Parse("2006-01-02 15:04", sendDate); err != nil {
		return campaignMutationBranch(name, SendCampaignBranchDefect, CampaignIdentity{}, sdkgo.FailureValidation, "send date must be YYYY-MM-DD HH:MM in the client's timezone")
	}
	payload := map[string]any{"ConfirmationEmail": input.ConfirmationEmail, "SendDate": sendDate}
	readback := func() string {
		if operation.isCampaignSentOrScheduled(call, input.ClientID, input.CampaignID, input.SendDate != "") {
			return input.CampaignID
		}
		return ""
	}
	return operation.client.invokeLimitedCampaignMutation(call, name, campaignPath(input.CampaignID)+"/send.json", payload, SendCampaignDefinition, SendCampaignBranchSent, SendCampaignBranchProviderRejected, SendCampaignBranchDefect, http.StatusOK, CampaignIdentity{ID: input.CampaignID}, readback, func(body []byte) string {
		if len(body) == 0 {
			return input.CampaignID
		}
		return ""
	})
}

func (operation SendCampaignOperation) isCampaignSentOrScheduled(call sdkgo.Call, clientID, campaignID string, isScheduled bool) bool {
	path := "/clients/" + url.PathEscape(clientID) + "/campaigns.json?page=1&pagesize=1000"
	if isScheduled {
		path = "/clients/" + url.PathEscape(clientID) + "/scheduled.json"
	}
	body, status, _, err := operation.client.get(call, path)
	if err != nil || status != http.StatusOK {
		return false
	}
	var rows []struct {
		ID string `json:"CampaignID"`
	}
	if isScheduled {
		if json.Unmarshal(body, &rows) != nil {
			return false
		}
	} else {
		var page struct {
			Results []struct {
				ID string `json:"CampaignID"`
			} `json:"Results"`
		}
		if json.Unmarshal(body, &page) != nil {
			return false
		}
		rows = page.Results
	}
	for _, item := range rows {
		if item.ID == campaignID {
			return true
		}
	}
	return false
}

func (client *Client) invokeLimitedCampaignMutation(call sdkgo.Call, name, path string, payload any, definition sdkgo.MutationDefinition, success, rejected, defect sdkgo.BranchID, successStatus int, identity CampaignIdentity, readback func() string, decodeID func([]byte) string) sdkgo.MutationAttempt[CampaignIdentity] {
	fence, err := singledispatch.NewFence(call, definition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return campaignMutationBranch(name, defect, CampaignIdentity{}, sdkgo.FailureLocalDefect, "campaign dispatch fence could not be constructed")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.ReadBackRequired || decision.Kind == singledispatch.ReadBackThenResendAllowed {
		if confirmedID := readback(); confirmedID != "" {
			identity.ID = confirmedID
			return sdkgo.NewMutationBranch(success, identity, nil, sdkgo.Receipt{})
		}
	}
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(CampaignIdentity{}, queryFailure(name, sdkgo.FailureTransport, "an earlier campaign action may have succeeded"), sdkgo.Receipt{})
	}
	var body []byte
	var status int
	var retryAfter time.Duration
	var requestErr error
	err = fence.Dispatch(func(ctx context.Context) error {
		body, status, retryAfter, requestErr = client.requestWithContext(ctx, call, http.MethodPost, path, payload)
		return requestErr
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[CampaignIdentity](queryFailure(name, sdkgo.FailureAvailability, "campaign dispatch was not recorded or sent"), 0)
	}
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
		if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[CampaignIdentity](queryFailure(name, sdkgo.FailureAvailability, "campaign retry checkpoint failed"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[CampaignIdentity](queryFailure(name, kind, "Campaign Monitor asked to retry the campaign action"), retryAfter)
	}
	if err != nil || status == http.StatusRequestTimeout || status >= 500 {
		if decision.MaximumPriorSends+1 >= call.MaxSends() {
			return sdkgo.NewMutationUncertain(CampaignIdentity{}, queryFailure(name, sdkgo.FailureTransport, "campaign outcome is unknown after bounded sends"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[CampaignIdentity](queryFailure(name, sdkgo.FailureTransport, "campaign outcome is unknown"), 0)
	}
	if status != successStatus {
		return campaignMutationBranch(name, rejected, CampaignIdentity{}, sdkgo.FailureProviderRejection, "Campaign Monitor rejected HTTP "+strconv.Itoa(status))
	}
	if id := decodeID(body); id != "" {
		identity.ID = id
	} else {
		return sdkgo.NewMutationUncertain(CampaignIdentity{}, queryFailure(name, sdkgo.FailureProtocol, "Campaign Monitor accepted an action but its outcome is unconfirmed"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(success, identity, nil, sdkgo.Receipt{})
}

// Invoke sends a draft preview with bounded possible duplicate sends.
func (operation SendCampaignPreviewOperation) Invoke(call sdkgo.Call, input SendCampaignPreviewInput) sdkgo.MutationAttempt[CampaignIdentity] {
	const name = "sendCampaignPreview"
	if !validCampaignMonitorID(input.CampaignID) || len(input.Recipients) == 0 || len(input.Recipients) > 5 {
		return campaignMutationBranch(name, SendCampaignPreviewBranchDefect, CampaignIdentity{}, sdkgo.FailureValidation, "campaign ID or preview recipients are invalid")
	}
	for _, recipient := range input.Recipients {
		if !validSubscriberEmail(recipient) {
			return campaignMutationBranch(name, SendCampaignPreviewBranchDefect, CampaignIdentity{}, sdkgo.FailureValidation, "preview email is invalid")
		}
	}
	payload := map[string]any{"PreviewRecipients": input.Recipients}
	return operation.client.invokeLimitedCampaignMutation(call, name, campaignPath(input.CampaignID)+"/sendpreview.json", payload, SendCampaignPreviewDefinition, SendCampaignPreviewBranchSent, SendCampaignPreviewBranchProviderRejected, SendCampaignPreviewBranchDefect, http.StatusOK, CampaignIdentity{ID: input.CampaignID}, func() string { return "" }, func(body []byte) string {
		if len(body) == 0 {
			return input.CampaignID
		}
		return ""
	})
}

// Invoke moves a scheduled campaign back to drafts.
func (operation UnscheduleCampaignOperation) Invoke(call sdkgo.Call, input UnscheduleCampaignInput) sdkgo.MutationAttempt[CampaignIdentity] {
	const name = "unscheduleCampaign"
	if !validCampaignMonitorID(input.CampaignID) {
		return campaignMutationBranch(name, UnscheduleCampaignBranchDefect, CampaignIdentity{}, sdkgo.FailureValidation, "campaign ID is invalid")
	}
	body, status, retryAfter, err := operation.client.request(call, http.MethodPost, campaignPath(input.CampaignID)+"/unschedule.json", nil)
	switch {
	case errors.Is(err, errCredential):
		return campaignMutationBranch(name, UnscheduleCampaignBranchDefect, CampaignIdentity{}, sdkgo.FailureAuthentication, "connection credentials are unavailable")
	case errors.Is(err, errRequest):
		return sdkgo.NewMutationRetry[CampaignIdentity](queryFailure(name, sdkgo.FailureTransport, "Campaign Monitor request failed"), 0)
	case status == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[CampaignIdentity](queryFailure(name, sdkgo.FailureRateLimit, "Campaign Monitor rate limited the request"), retryAfter)
	case status == http.StatusRequestTimeout || status >= 500:
		return sdkgo.NewMutationRetry[CampaignIdentity](queryFailure(name, sdkgo.FailureAvailability, "Campaign Monitor is temporarily unavailable"), retryAfter)
	case status != http.StatusOK && status != http.StatusNoContent:
		return campaignMutationBranch(name, UnscheduleCampaignBranchProviderRejected, CampaignIdentity{}, sdkgo.FailureProviderRejection, "Campaign Monitor rejected HTTP "+strconv.Itoa(status))
	case errors.Is(err, providerhttp.ErrBodyTooLarge):
		return campaignMutationBranch(name, UnscheduleCampaignBranchInvalidResponse, CampaignIdentity{}, sdkgo.FailureResponseTooLarge, "Campaign Monitor response exceeded the limit")
	case err != nil:
		return sdkgo.NewMutationRetry[CampaignIdentity](queryFailure(name, sdkgo.FailureTransport, "Campaign Monitor response was interrupted"), 0)
	case len(body) != 0:
		return campaignMutationBranch(name, UnscheduleCampaignBranchInvalidResponse, CampaignIdentity{}, sdkgo.FailureProtocol, "Campaign Monitor returned an unexpected response body")
	}
	return sdkgo.NewMutationBranch(UnscheduleCampaignBranchUnscheduled, CampaignIdentity{ID: input.CampaignID}, nil, sdkgo.Receipt{})
}

// Invoke deletes a campaign by ID.
func (operation DeleteCampaignOperation) Invoke(call sdkgo.Call, input DeleteCampaignInput) sdkgo.MutationAttempt[CampaignIdentity] {
	const name = "deleteCampaign"
	if !validCampaignMonitorID(input.CampaignID) {
		return campaignMutationBranch(name, DeleteCampaignBranchDefect, CampaignIdentity{}, sdkgo.FailureValidation, "campaign ID is invalid")
	}
	body, status, retryAfter, err := operation.client.request(call, http.MethodDelete, campaignPath(input.CampaignID)+".json", nil)
	switch {
	case errors.Is(err, errCredential):
		return campaignMutationBranch(name, DeleteCampaignBranchDefect, CampaignIdentity{}, sdkgo.FailureAuthentication, "connection credentials are unavailable")
	case errors.Is(err, errRequest):
		return sdkgo.NewMutationRetry[CampaignIdentity](queryFailure(name, sdkgo.FailureTransport, "Campaign Monitor request failed"), 0)
	case status == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[CampaignIdentity](queryFailure(name, sdkgo.FailureRateLimit, "Campaign Monitor rate limited the request"), retryAfter)
	case status == http.StatusRequestTimeout || status >= 500:
		return sdkgo.NewMutationRetry[CampaignIdentity](queryFailure(name, sdkgo.FailureAvailability, "Campaign Monitor is temporarily unavailable"), retryAfter)
	case status != http.StatusOK && status != http.StatusNoContent:
		return campaignMutationBranch(name, DeleteCampaignBranchProviderRejected, CampaignIdentity{}, sdkgo.FailureProviderRejection, "Campaign Monitor rejected HTTP "+strconv.Itoa(status))
	case errors.Is(err, providerhttp.ErrBodyTooLarge):
		return campaignMutationBranch(name, DeleteCampaignBranchInvalidResponse, CampaignIdentity{}, sdkgo.FailureResponseTooLarge, "Campaign Monitor response exceeded the limit")
	case err != nil:
		return sdkgo.NewMutationRetry[CampaignIdentity](queryFailure(name, sdkgo.FailureTransport, "Campaign Monitor response was interrupted"), 0)
	case len(body) != 0:
		return campaignMutationBranch(name, DeleteCampaignBranchInvalidResponse, CampaignIdentity{}, sdkgo.FailureProtocol, "Campaign Monitor returned an unexpected response body")
	}
	return sdkgo.NewMutationBranch(DeleteCampaignBranchDeleted, CampaignIdentity{ID: input.CampaignID}, nil, sdkgo.Receipt{})
}

func campaignPath(campaignID string) string { return "/campaigns/" + url.PathEscape(campaignID) }

func validCampaignIDs(ids []string) bool {
	for _, id := range ids {
		if !validCampaignMonitorID(id) {
			return false
		}
	}
	return true
}

func campaignMutationBranch(name string, branch sdkgo.BranchID, value CampaignIdentity, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CampaignIdentity] {
	failure := queryFailure(name, kind, message)
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}
