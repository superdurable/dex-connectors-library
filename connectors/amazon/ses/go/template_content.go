// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ses

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

var templateNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// EmailTemplateContent is the subject and text and/or HTML body stored by SES.
type EmailTemplateContent struct {
	// Subject is a UTF-8 subject, at most 998 bytes, without a line break.
	Subject string `json:"subject"`
	// Text is the UTF-8 plain text body; blank means no text part.
	Text string `json:"text,omitempty"`
	// HTML is the UTF-8 HTML body; blank means no HTML part.
	HTML string `json:"html,omitempty"`
}

// EmailTemplate names one stored template and its complete content.
type EmailTemplate struct {
	// Name is the regional SES template name.
	Name string `json:"name"`
	// Content contains its subject and body parts.
	Content EmailTemplateContent `json:"content"`
}

func validateTemplateName(name string) error {
	if !templateNamePattern.MatchString(name) {
		return errors.New("templateName must contain 1 to 64 letters, digits, underscores, or hyphens")
	}
	return nil
}

func validateTemplateContent(content EmailTemplateContent) error {
	if content.Subject == "" || len(content.Subject) > 998 || !utf8.ValidString(content.Subject) || strings.ContainsAny(content.Subject, "\r\n") {
		return errors.New("template subject must be 1 to 998 UTF-8 bytes without line breaks")
	}
	if content.Text == "" && content.HTML == "" {
		return errors.New("template needs a text or HTML body")
	}
	if len(content.Text) > 100*1024 || len(content.HTML) > 100*1024 || !utf8.ValidString(content.Text) || !utf8.ValidString(content.HTML) {
		return errors.New("template bodies must be valid UTF-8 and at most 100 KiB each")
	}
	return nil
}

func decodeEmailTemplate(name string, body []byte) (EmailTemplate, error) {
	var wire struct {
		TemplateName    string `json:"TemplateName"`
		TemplateContent struct {
			Subject string `json:"Subject"`
			Text    string `json:"Text"`
			HTML    string `json:"Html"`
		} `json:"TemplateContent"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return EmailTemplate{}, errors.New("SES returned invalid template JSON")
	}
	if wire.TemplateName != "" && wire.TemplateName != name {
		return EmailTemplate{}, errors.New("SES returned the wrong template name")
	}
	content := EmailTemplateContent{Subject: wire.TemplateContent.Subject, Text: wire.TemplateContent.Text, HTML: wire.TemplateContent.HTML}
	if err := validateTemplateContent(content); err != nil {
		return EmailTemplate{}, errors.New("SES returned invalid template content")
	}
	return EmailTemplate{Name: name, Content: content}, nil
}
