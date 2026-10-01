// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// CampaignStatus is Mailchimp's status of a campaign.
type CampaignStatus string

const (
	// CampaignStatusSave is a draft that has not been scheduled or sent; only a draft is sent.
	CampaignStatusSave CampaignStatus = "save"
	// CampaignStatusPaused is a paused RSS or automation campaign.
	CampaignStatusPaused CampaignStatus = "paused"
	// CampaignStatusSchedule is scheduled to send later.
	CampaignStatusSchedule CampaignStatus = "schedule"
	// CampaignStatusSending is being sent.
	CampaignStatusSending CampaignStatus = "sending"
	// CampaignStatusSent finished sending.
	CampaignStatusSent CampaignStatus = "sent"
	// CampaignStatusCanceled had its send canceled.
	CampaignStatusCanceled CampaignStatus = "canceled"
	// CampaignStatusCanceling is having its send canceled.
	CampaignStatusCanceling CampaignStatus = "canceling"
	// CampaignStatusArchived is archived.
	CampaignStatusArchived CampaignStatus = "archived"
)

// campaignReadFields limits the campaign read before a send to the fields Campaign holds.
var campaignReadFields = strings.Join([]string{
	"id", "web_id", "type", "status", "emails_sent", "create_time", "send_time", "settings.title",
	"settings.subject_line", "recipients.list_id", "recipients.list_name", "recipients.recipient_count",
}, ",")

// immediatelySendableCampaignTypes are the types Mailchimp sends at once; an RSS campaign sends on its schedule.
var immediatelySendableCampaignTypes = map[string]bool{"regular": true, "plaintext": true, "absplit": true, "variate": true}

// Campaign is the part of a Mailchimp campaign a send decision needs.
type Campaign struct {
	// ID is Mailchimp's campaign ID.
	ID string `json:"id"`
	// WebID is the ID of the campaign's page in the Mailchimp web app, or zero.
	WebID int64 `json:"webId,omitempty"`
	// Type is regular, plaintext, absplit, rss, or variate.
	Type string `json:"type"`
	// Status is the campaign status Mailchimp reported when the connector read it.
	Status CampaignStatus `json:"status"`
	// Title is the campaign's internal title, or empty.
	Title string `json:"title,omitempty"`
	// SubjectLine is the campaign's subject line, or empty.
	SubjectLine string `json:"subjectLine,omitempty"`
	// ListID is the audience the campaign sends to.
	ListID string `json:"listId,omitempty"`
	// ListName is that audience's name.
	ListName string `json:"listName,omitempty"`
	// RecipientCount is Mailchimp's count of the campaign's recipients.
	RecipientCount int `json:"recipientCount"`
	// EmailsSent is the number of emails Mailchimp sent for the campaign so far.
	EmailsSent int `json:"emailsSent"`
	// CreatedAt is when the campaign was created, or nil.
	CreatedAt *time.Time `json:"createdAt,omitempty"`
	// SentAt is when the campaign was sent, or nil.
	SentAt *time.Time `json:"sentAt,omitempty"`
}

type campaignWire struct {
	ID         string `json:"id"`
	WebID      int64  `json:"web_id"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	EmailsSent int    `json:"emails_sent"`
	CreateTime string `json:"create_time"`
	SendTime   string `json:"send_time"`
	Settings   struct {
		Title       string `json:"title"`
		SubjectLine string `json:"subject_line"`
	} `json:"settings"`
	Recipients struct {
		ListID         string `json:"list_id"`
		ListName       string `json:"list_name"`
		RecipientCount int    `json:"recipient_count"`
	} `json:"recipients"`
}

// IsSendingOrSent reports whether Mailchimp is sending or has sent the campaign.
func (status CampaignStatus) IsSendingOrSent() bool {
	return status == CampaignStatusSending || status == CampaignStatusSent
}

func decodeCampaignBody(body []byte, expectedID string) (Campaign, error) {
	var wire campaignWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return Campaign{}, errors.New("the campaign is not valid JSON")
	}
	if wire.ID != expectedID || wire.Status == "" || wire.Type == "" {
		return Campaign{}, errors.New("the campaign has another ID, or no status or type")
	}
	campaign := Campaign{
		ID: wire.ID, WebID: wire.WebID, Type: wire.Type, Status: CampaignStatus(wire.Status),
		Title: wire.Settings.Title, SubjectLine: wire.Settings.SubjectLine, ListID: wire.Recipients.ListID,
		ListName: wire.Recipients.ListName, RecipientCount: wire.Recipients.RecipientCount, EmailsSent: wire.EmailsSent,
	}
	var err error
	if campaign.CreatedAt, err = parseMailchimpTime(wire.CreateTime); err != nil {
		return Campaign{}, err
	}
	if campaign.SentAt, err = parseMailchimpTime(wire.SendTime); err != nil {
		return Campaign{}, err
	}
	return campaign, nil
}

// describeSendBlocker explains why a campaign that is not sending or sent cannot be sent now, or returns "".
func describeSendBlocker(campaign Campaign, expectedListID string) string {
	switch {
	case campaign.Status != CampaignStatusSave:
		return "the campaign is not a draft (status save), such as a scheduled, paused, canceled, or archived campaign"
	case !immediatelySendableCampaignTypes[campaign.Type]:
		return "the campaign type does not send immediately, such as an RSS campaign that sends on its schedule"
	case expectedListID != "" && campaign.ListID != expectedListID:
		return "the campaign targets another audience than expectedListId"
	default:
		return ""
	}
}
