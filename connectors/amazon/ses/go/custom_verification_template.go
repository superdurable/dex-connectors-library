// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ses

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const customVerificationTemplatesPath = "/v2/email/custom-verification-email-templates"

// CustomVerificationEmailTemplate is the complete regional verification message.
type CustomVerificationEmailTemplate struct {
	// Name identifies the custom verification template.
	Name string `json:"name"`
	// FromEmailAddress is the verified sender address.
	FromEmailAddress string `json:"fromEmailAddress"`
	// Subject is the verification email subject.
	Subject string `json:"subject"`
	// Content is the UTF-8 verification email body.
	Content string `json:"content"`
	// SuccessRedirectionURL receives a successfully verified recipient.
	SuccessRedirectionURL string `json:"successRedirectionURL"`
	// FailureRedirectionURL receives an unsuccessfully verified recipient.
	FailureRedirectionURL string `json:"failureRedirectionURL"`
}

// CustomVerificationEmailTemplateSummary is metadata from a list page.
type CustomVerificationEmailTemplateSummary struct {
	// Name identifies the custom verification template.
	Name string `json:"name"`
	// FromEmailAddress is the verified sender address.
	FromEmailAddress string `json:"fromEmailAddress"`
	// Subject is the verification email subject.
	Subject string `json:"subject"`
	// SuccessRedirectionURL receives a successfully verified recipient.
	SuccessRedirectionURL string `json:"successRedirectionURL"`
	// FailureRedirectionURL receives an unsuccessfully verified recipient.
	FailureRedirectionURL string `json:"failureRedirectionURL"`
}

// CustomVerificationEmailTemplatePage holds one bounded page of summaries.
type CustomVerificationEmailTemplatePage struct {
	// Templates contains at most PageSize summaries.
	Templates []CustomVerificationEmailTemplateSummary `json:"templates"`
	// NextToken continues the listing; blank means the final page.
	NextToken string `json:"nextToken,omitempty"`
}

func validateCustomVerificationTemplate(value CustomVerificationEmailTemplate) error {
	if err := validateTemplateName(value.Name); err != nil {
		return err
	}
	if !emailAddressPattern.MatchString(value.FromEmailAddress) {
		return errors.New("verification template sender is invalid")
	}
	if value.Subject == "" || len(value.Subject) > 998 || !utf8.ValidString(value.Subject) || strings.ContainsAny(value.Subject, "\r\n") {
		return errors.New("verification template subject must be one line of at most 998 UTF-8 bytes")
	}
	if value.Content == "" || len(value.Content) >= 10*1024*1024 || !utf8.ValidString(value.Content) {
		return errors.New("verification template body must be valid UTF-8 under 10 MiB")
	}
	for _, redirectURL := range []string{value.SuccessRedirectionURL, value.FailureRedirectionURL} {
		parsed, err := url.Parse(redirectURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			return errors.New("verification template redirect URLs must be absolute HTTP or HTTPS URLs")
		}
	}
	return nil
}

func customVerificationTemplatePath(name string) string {
	return customVerificationTemplatesPath + "/" + url.PathEscape(name)
}

func decodeCustomVerificationTemplate(name string, body []byte) (CustomVerificationEmailTemplate, error) {
	var wire struct {
		TemplateName          string `json:"TemplateName"`
		FromEmailAddress      string `json:"FromEmailAddress"`
		TemplateSubject       string `json:"TemplateSubject"`
		TemplateContent       string `json:"TemplateContent"`
		SuccessRedirectionURL string `json:"SuccessRedirectionURL"`
		FailureRedirectionURL string `json:"FailureRedirectionURL"`
	}
	if json.Unmarshal(body, &wire) != nil || (wire.TemplateName != "" && wire.TemplateName != name) {
		return CustomVerificationEmailTemplate{}, errors.New("SES returned invalid verification template JSON")
	}
	value := CustomVerificationEmailTemplate{Name: name, FromEmailAddress: wire.FromEmailAddress, Subject: wire.TemplateSubject, Content: wire.TemplateContent, SuccessRedirectionURL: wire.SuccessRedirectionURL, FailureRedirectionURL: wire.FailureRedirectionURL}
	if validateCustomVerificationTemplate(value) != nil {
		return CustomVerificationEmailTemplate{}, errors.New("SES returned invalid verification template content")
	}
	return value, nil
}

func encodeCustomVerificationTemplate(value CustomVerificationEmailTemplate, includeName bool) ([]byte, error) {
	wire := struct {
		TemplateName          string `json:"TemplateName,omitempty"`
		FromEmailAddress      string `json:"FromEmailAddress"`
		TemplateSubject       string `json:"TemplateSubject"`
		TemplateContent       string `json:"TemplateContent"`
		SuccessRedirectionURL string `json:"SuccessRedirectionURL"`
		FailureRedirectionURL string `json:"FailureRedirectionURL"`
	}{FromEmailAddress: value.FromEmailAddress, TemplateSubject: value.Subject, TemplateContent: value.Content, SuccessRedirectionURL: value.SuccessRedirectionURL, FailureRedirectionURL: value.FailureRedirectionURL}
	if includeName {
		wire.TemplateName = value.Name
	}
	return json.Marshal(wire)
}

// GetCustomVerificationEmailTemplateInput names one regional template.
type GetCustomVerificationEmailTemplateInput struct {
	// TemplateName is the exact name in SES.
	TemplateName string `json:"templateName"`
}

// GetCustomVerificationEmailTemplateOperation reads one verification template.
type GetCustomVerificationEmailTemplateOperation struct{ client *Client }

// GetCustomVerificationEmailTemplate binds the Query to this client.
func (client *Client) GetCustomVerificationEmailTemplate() GetCustomVerificationEmailTemplateOperation {
	return GetCustomVerificationEmailTemplateOperation{client}
}

// Definition returns the generated Query contract.
func (GetCustomVerificationEmailTemplateOperation) Definition() sdkgo.QueryDefinition {
	return GetCustomVerificationEmailTemplateDefinition
}

// Invoke reads one complete custom verification template.
func (operation GetCustomVerificationEmailTemplateOperation) Invoke(call sdkgo.Call, input GetCustomVerificationEmailTemplateInput) sdkgo.QueryAttempt[CustomVerificationEmailTemplate] {
	const operationID = "getCustomVerificationEmailTemplate"
	if err := validateTemplateName(input.TemplateName); err != nil {
		return templateQueryBranch[CustomVerificationEmailTemplate](operationID, GetCustomVerificationEmailTemplateBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	response := operation.client.request(call, http.MethodGet, customVerificationTemplatePath(input.TemplateName), nil)
	if attempt, failed := templateQueryFailure[CustomVerificationEmailTemplate](operationID, response, GetCustomVerificationEmailTemplateBranchMissing, GetCustomVerificationEmailTemplateBranchProviderRejected, GetCustomVerificationEmailTemplateBranchInvalidResponse, GetCustomVerificationEmailTemplateBranchDefect); failed {
		return attempt
	}
	value, err := decodeCustomVerificationTemplate(input.TemplateName, response.body)
	if err != nil {
		return templateQueryBranch[CustomVerificationEmailTemplate](operationID, GetCustomVerificationEmailTemplateBranchInvalidResponse, sdkgo.FailureProtocol, err.Error())
	}
	return sdkgo.NewQueryBranch(GetCustomVerificationEmailTemplateBranchFound, value, nil, sdkgo.Receipt{})
}

// ListCustomVerificationEmailTemplatesInput requests one regional list page.
type ListCustomVerificationEmailTemplatesInput struct {
	// NextToken resumes a previous page; blank starts at the beginning.
	NextToken string `json:"nextToken,omitempty"`
	// PageSize is 1 to 50; zero uses 10.
	PageSize int `json:"pageSize,omitempty"`
}

// ListCustomVerificationEmailTemplatesOperation reads regional template metadata.
type ListCustomVerificationEmailTemplatesOperation struct{ client *Client }

// ListCustomVerificationEmailTemplates binds the Query to this client.
func (client *Client) ListCustomVerificationEmailTemplates() ListCustomVerificationEmailTemplatesOperation {
	return ListCustomVerificationEmailTemplatesOperation{client}
}

// Definition returns the generated Query contract.
func (ListCustomVerificationEmailTemplatesOperation) Definition() sdkgo.QueryDefinition {
	return ListCustomVerificationEmailTemplatesDefinition
}

// Invoke reads one page of custom verification template metadata.
func (operation ListCustomVerificationEmailTemplatesOperation) Invoke(call sdkgo.Call, input ListCustomVerificationEmailTemplatesInput) sdkgo.QueryAttempt[CustomVerificationEmailTemplatePage] {
	const operationID = "listCustomVerificationEmailTemplates"
	if input.PageSize == 0 {
		input.PageSize = 10
	}
	if input.PageSize < 1 || input.PageSize > 50 || len(input.NextToken) > 4096 || strings.ContainsAny(input.NextToken, "\r\n") {
		return templateQueryBranch[CustomVerificationEmailTemplatePage](operationID, ListCustomVerificationEmailTemplatesBranchDefect, sdkgo.FailureValidation, "SES verification template page size or token is invalid")
	}
	query := url.Values{"PageSize": {strconv.Itoa(input.PageSize)}}
	if input.NextToken != "" {
		query.Set("NextToken", input.NextToken)
	}
	response := operation.client.request(call, http.MethodGet, customVerificationTemplatesPath+"?"+query.Encode(), nil)
	if attempt, failed := templateQueryFailure[CustomVerificationEmailTemplatePage](operationID, response, "", ListCustomVerificationEmailTemplatesBranchProviderRejected, ListCustomVerificationEmailTemplatesBranchInvalidResponse, ListCustomVerificationEmailTemplatesBranchDefect); failed {
		return attempt
	}
	var wire struct {
		NextToken string `json:"NextToken"`
		Templates []struct {
			TemplateName          string `json:"TemplateName"`
			FromEmailAddress      string `json:"FromEmailAddress"`
			TemplateSubject       string `json:"TemplateSubject"`
			SuccessRedirectionURL string `json:"SuccessRedirectionURL"`
			FailureRedirectionURL string `json:"FailureRedirectionURL"`
		} `json:"CustomVerificationEmailTemplates"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Templates == nil || len(wire.Templates) > input.PageSize || len(wire.NextToken) > 4096 {
		return templateQueryBranch[CustomVerificationEmailTemplatePage](operationID, ListCustomVerificationEmailTemplatesBranchInvalidResponse, sdkgo.FailureProtocol, "SES returned invalid verification template list")
	}
	page := CustomVerificationEmailTemplatePage{Templates: make([]CustomVerificationEmailTemplateSummary, 0, len(wire.Templates)), NextToken: wire.NextToken}
	for _, item := range wire.Templates {
		if validateTemplateName(item.TemplateName) != nil || !emailAddressPattern.MatchString(item.FromEmailAddress) {
			return templateQueryBranch[CustomVerificationEmailTemplatePage](operationID, ListCustomVerificationEmailTemplatesBranchInvalidResponse, sdkgo.FailureProtocol, "SES returned invalid verification template metadata")
		}
		page.Templates = append(page.Templates, CustomVerificationEmailTemplateSummary{Name: item.TemplateName, FromEmailAddress: item.FromEmailAddress, Subject: item.TemplateSubject, SuccessRedirectionURL: item.SuccessRedirectionURL, FailureRedirectionURL: item.FailureRedirectionURL})
	}
	return sdkgo.NewQueryBranch(ListCustomVerificationEmailTemplatesBranchPage, page, nil, sdkgo.Receipt{})
}
