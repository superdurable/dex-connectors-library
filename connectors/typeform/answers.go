// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package typeform

import (
	"bytes"
	"encoding/json"
	"time"
)

// Typeform answer types. Every FormAnswer sets the value field its Type names.
const (
	// AnswerTypeText answers short_text, long_text, and, in the Responses API, dropdown questions in Text.
	AnswerTypeText = "text"
	// AnswerTypeEmail answers an email question in Email.
	AnswerTypeEmail = "email"
	// AnswerTypeURL answers website, calendly, and google_calendar questions in URL.
	AnswerTypeURL = "url"
	// AnswerTypeFileURL answers a file_upload question in FileURL.
	AnswerTypeFileURL = "file_url"
	// AnswerTypeDate answers a date question in Date.
	AnswerTypeDate = "date"
	// AnswerTypePhoneNumber answers a phone_number question in PhoneNumber.
	AnswerTypePhoneNumber = "phone_number"
	// AnswerTypeNumber answers number, rating, opinion_scale, and nps questions in Number.
	AnswerTypeNumber = "number"
	// AnswerTypeBoolean answers legal and yes_no questions in Boolean.
	AnswerTypeBoolean = "boolean"
	// AnswerTypeChoice answers a single-selection choice question in Choice.
	AnswerTypeChoice = "choice"
	// AnswerTypeChoices answers a multiple-selection, ranking, or checkbox question in Choices.
	AnswerTypeChoices = "choices"
	// AnswerTypePayment answers a payment question in Payment.
	AnswerTypePayment = "payment"
	// AnswerTypeSignature answers a signature question in Signature.
	AnswerTypeSignature = "signature"
	// AnswerTypeMultiFormat answers a video or audio question in MultiFormat.
	AnswerTypeMultiFormat = "multi_format"
)

// FormResponse is one completed response with typed answers. The responseSubmitted Trigger and
// listResponses return the same shape, so a Flow can process pushed and backfilled responses alike.
type FormResponse struct {
	// Token identifies the response. Typeform documents it as the response ID, and listResponses pages
	// with it through before and after.
	Token string `json:"token"`
	// LandedAt is when the respondent opened the form, in UTC.
	LandedAt time.Time `json:"landedAt,omitzero"`
	// SubmittedAt is when the respondent submitted the form, in UTC.
	SubmittedAt time.Time `json:"submittedAt,omitzero"`
	// Answers lists the answered questions; Typeform omits a question the respondent skipped.
	Answers []FormAnswer `json:"answers"`
	// HiddenFields maps each hidden field name to its value; a non-string value keeps its JSON text.
	HiddenFields map[string]string `json:"hiddenFields,omitempty"`
	// Variables lists the form's variables and their final values, such as score.
	Variables []FormVariable `json:"variables,omitempty"`
	// Score is the calculated score when the form computes one.
	Score *float64 `json:"score,omitempty"`
	// EndingRef is the ref of the ending screen the respondent reached, from webhooks only.
	EndingRef string `json:"endingRef,omitempty"`
}

// FormAnswer is one typed answer, keyed by its field. Exactly one value field is set for a known Type;
// an answer type this connector does not know keeps its field and Type with no value.
type FormAnswer struct {
	// FieldID is the question's field ID, as Form.Fields lists it.
	FieldID string `json:"fieldId"`
	// FieldRef is the question's ref; a webhook fills it from the form definition when the answer omits it.
	FieldRef string `json:"fieldRef,omitempty"`
	// FieldType is the question type, such as short_text, multiple_choice, or rating.
	FieldType string `json:"fieldType"`
	// FieldTitle is the question text from the webhook's form definition; listResponses leaves it blank.
	FieldTitle string `json:"fieldTitle,omitempty"`
	// Type is the answer type, one of the AnswerType constants, which names the value field that is set.
	Type string `json:"type"`
	// Text is a text answer.
	Text string `json:"text,omitempty"`
	// Email is an email answer.
	Email string `json:"email,omitempty"`
	// URL is a url answer, such as a website or a Calendly booking link.
	URL string `json:"url,omitempty"`
	// FileURL is the download link of an uploaded file; Typeform does not promise its shape.
	FileURL string `json:"fileUrl,omitempty"`
	// Date is a date answer exactly as Typeform sends it: YYYY-MM-DD in webhooks, and an RFC 3339
	// midnight timestamp in listResponses.
	Date string `json:"date,omitempty"`
	// PhoneNumber is a phone number answer, such as +14155550100.
	PhoneNumber string `json:"phoneNumber,omitempty"`
	// Number is a number, rating, or opinion scale answer.
	Number *float64 `json:"number,omitempty"`
	// Boolean is a yes/no or legal answer.
	Boolean *bool `json:"boolean,omitempty"`
	// Choice is a single selected choice.
	Choice *FormAnswerChoice `json:"choice,omitempty"`
	// Choices are the selected choices, in the respondent's order for a ranking question.
	Choices *FormAnswerChoices `json:"choices,omitempty"`
	// Payment is a payment answer. The card's last four digits and cardholder name are not copied.
	Payment *FormAnswerPayment `json:"payment,omitempty"`
	// Signature is a signature answer.
	Signature *FormAnswerSignature `json:"signature,omitempty"`
	// MultiFormat is a video or audio question's answer, or its typed text alternative.
	MultiFormat *FormAnswerMultiFormat `json:"multiFormat,omitempty"`
}

// FormAnswerChoice is one selected choice, or the respondent's own text when Other is set.
type FormAnswerChoice struct {
	// ID is the choice ID.
	ID string `json:"id,omitempty"`
	// Ref is the choice ref.
	Ref string `json:"ref,omitempty"`
	// Label is the choice text.
	Label string `json:"label,omitempty"`
	// Other is the respondent's own answer for an Other option.
	Other string `json:"other,omitempty"`
}

// FormAnswerChoices are the selected choices; IDs, Labels, and Refs are parallel lists when present.
type FormAnswerChoices struct {
	// IDs are the selected choice IDs.
	IDs []string `json:"ids,omitempty"`
	// Labels are the selected choice texts.
	Labels []string `json:"labels,omitempty"`
	// Refs are the selected choice refs.
	Refs []string `json:"refs,omitempty"`
	// Other is the respondent's own answer for an Other option.
	Other string `json:"other,omitempty"`
}

// FormAnswerPayment is the connector-safe part of a payment answer.
type FormAnswerPayment struct {
	// Amount is the charged amount in the form's currency, as Typeform's decimal string.
	Amount string `json:"amount"`
	// IsSuccessful is true when Typeform processed the payment.
	IsSuccessful bool `json:"isSuccessful"`
}

// FormAnswerSignature is a signature answer.
type FormAnswerSignature struct {
	// URL is the signature image's download link.
	URL string `json:"url"`
	// Method is how the respondent signed: typed, drawn, or uploaded.
	Method string `json:"method,omitempty"`
}

// FormAnswerMultiFormat is a video or audio question's answer.
type FormAnswerMultiFormat struct {
	// Text is the respondent's typed answer when they chose not to record.
	Text string `json:"text,omitempty"`
	// VideoID identifies a recorded video for the Responses API's media download.
	VideoID string `json:"videoId,omitempty"`
	// AudioID identifies a recorded audio answer for the Responses API's media download.
	AudioID string `json:"audioId,omitempty"`
	// Transcript is the recording's transcript; Typeform may deliver it later than the webhook.
	Transcript string `json:"transcript,omitempty"`
}

// FormVariable is one form variable and its final value.
type FormVariable struct {
	// Key is the variable name, such as score.
	Key string `json:"key"`
	// Type is text or number and names the value field that is set.
	Type string `json:"type"`
	// Text is a text variable's value.
	Text string `json:"text,omitempty"`
	// Number is a number variable's value.
	Number *float64 `json:"number,omitempty"`
}

// AnswerByFieldRef returns the answer to the question with ref, and false when the respondent skipped it.
func (response FormResponse) AnswerByFieldRef(ref string) (FormAnswer, bool) {
	for _, answer := range response.Answers {
		if ref != "" && answer.FieldRef == ref {
			return answer, true
		}
	}
	return FormAnswer{}, false
}

// AnswerByFieldID returns the answer to the question with fieldID, and false when the respondent skipped it.
func (response FormResponse) AnswerByFieldID(fieldID string) (FormAnswer, bool) {
	for _, answer := range response.Answers {
		if fieldID != "" && answer.FieldID == fieldID {
			return answer, true
		}
	}
	return FormAnswer{}, false
}

// typeformResponseFields are the response members the webhook and the Responses API share.
type typeformResponseFields struct {
	Token       string                     `json:"token"`
	LandedAt    string                     `json:"landed_at"`
	SubmittedAt string                     `json:"submitted_at"`
	Answers     []typeformAnswer           `json:"answers"`
	Hidden      map[string]json.RawMessage `json:"hidden"`
	Variables   []typeformVariable         `json:"variables"`
	Calculated  *struct {
		Score *float64 `json:"score"`
	} `json:"calculated"`
	Ending *struct {
		Ref string `json:"ref"`
	} `json:"ending"`
}

type typeformAnswer struct {
	Type  string `json:"type"`
	Field struct {
		ID   string `json:"id"`
		Ref  string `json:"ref"`
		Type string `json:"type"`
	} `json:"field"`
	Text        *string           `json:"text"`
	Email       *string           `json:"email"`
	URL         *string           `json:"url"`
	FileURL     *string           `json:"file_url"`
	Date        *string           `json:"date"`
	PhoneNumber *string           `json:"phone_number"`
	Number      *float64          `json:"number"`
	Boolean     *bool             `json:"boolean"`
	Choice      *FormAnswerChoice `json:"choice"`
	Choices     json.RawMessage   `json:"choices"`
	Payment     *struct {
		Amount  string `json:"amount"`
		Success bool   `json:"success"`
	} `json:"payment"`
	Signature *struct {
		URL  string `json:"url"`
		Type string `json:"type"`
	} `json:"signature"`
	MultiFormat *struct {
		Text       string `json:"text"`
		VideoID    string `json:"video_id"`
		AudioID    string `json:"audio_id"`
		Transcript string `json:"transcript"`
	} `json:"multi_format"`
}

type typeformVariable struct {
	Key    string   `json:"key"`
	Type   string   `json:"type"`
	Text   string   `json:"text"`
	Number *float64 `json:"number"`
}

// typeformFieldDefinition is what a webhook's form definition says about one field.
type typeformFieldDefinition struct {
	ref   string
	title string
}

// convert validates the shared members. definitions fills refs and titles from a webhook's definition.
func (fields typeformResponseFields) convert(definitions map[string]typeformFieldDefinition) (FormResponse, error) {
	if !typeformIDPattern.MatchString(fields.Token) {
		return FormResponse{}, errTypeformResponseMalformed
	}
	landedAt, err := parseOptionalTypeformTimestamp(fields.LandedAt)
	if err != nil {
		return FormResponse{}, err
	}
	submittedAt, err := parseOptionalTypeformTimestamp(fields.SubmittedAt)
	if err != nil {
		return FormResponse{}, err
	}
	response := FormResponse{Token: fields.Token, LandedAt: landedAt, SubmittedAt: submittedAt, Answers: make([]FormAnswer, 0, len(fields.Answers))}
	for _, answer := range fields.Answers {
		converted, err := answer.convert(definitions)
		if err != nil {
			return FormResponse{}, err
		}
		response.Answers = append(response.Answers, converted)
	}
	if len(fields.Hidden) > 0 {
		response.HiddenFields = make(map[string]string, len(fields.Hidden))
		for name, value := range fields.Hidden {
			response.HiddenFields[name] = hiddenFieldText(value)
		}
	}
	for _, variable := range fields.Variables {
		if variable.Key == "" {
			return FormResponse{}, errTypeformResponseMalformed
		}
		response.Variables = append(response.Variables, FormVariable(variable))
	}
	if fields.Calculated != nil {
		response.Score = fields.Calculated.Score
	}
	if fields.Ending != nil {
		response.EndingRef = fields.Ending.Ref
	}
	return response, nil
}

// hiddenFieldText returns a string value's text and any other JSON value's compact JSON text.
func hiddenFieldText(value json.RawMessage) string {
	var text string
	if json.Unmarshal(value, &text) == nil {
		return text
	}
	if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return ""
	}
	var compacted bytes.Buffer
	if json.Compact(&compacted, value) != nil {
		return ""
	}
	return compacted.String()
}

// convert requires the field ID and both types, and the value field the answer type names.
func (answer typeformAnswer) convert(definitions map[string]typeformFieldDefinition) (FormAnswer, error) {
	if answer.Field.ID == "" || answer.Field.Type == "" || answer.Type == "" {
		return FormAnswer{}, errTypeformResponseMalformed
	}
	converted := FormAnswer{FieldID: answer.Field.ID, FieldRef: answer.Field.Ref, FieldType: answer.Field.Type, Type: answer.Type}
	if definition, isDefined := definitions[answer.Field.ID]; isDefined {
		converted.FieldTitle = definition.title
		if converted.FieldRef == "" {
			converted.FieldRef = definition.ref
		}
	}
	isValuePresent := true
	switch answer.Type {
	case AnswerTypeText:
		converted.Text, isValuePresent = derefString(answer.Text)
	case AnswerTypeEmail:
		converted.Email, isValuePresent = derefString(answer.Email)
	case AnswerTypeURL:
		converted.URL, isValuePresent = derefString(answer.URL)
	case AnswerTypeFileURL:
		converted.FileURL, isValuePresent = derefString(answer.FileURL)
	case AnswerTypeDate:
		converted.Date, isValuePresent = derefString(answer.Date)
	case AnswerTypePhoneNumber:
		converted.PhoneNumber, isValuePresent = derefString(answer.PhoneNumber)
	case AnswerTypeNumber:
		converted.Number, isValuePresent = answer.Number, answer.Number != nil
	case AnswerTypeBoolean:
		converted.Boolean, isValuePresent = answer.Boolean, answer.Boolean != nil
	case AnswerTypeChoice:
		converted.Choice, isValuePresent = answer.Choice, answer.Choice != nil
	case AnswerTypeChoices:
		choices, err := decodeAnswerChoices(answer.Choices)
		if err != nil {
			return FormAnswer{}, err
		}
		converted.Choices = choices
	case AnswerTypePayment:
		if isValuePresent = answer.Payment != nil; isValuePresent {
			converted.Payment = &FormAnswerPayment{Amount: answer.Payment.Amount, IsSuccessful: answer.Payment.Success}
		}
	case AnswerTypeSignature:
		if isValuePresent = answer.Signature != nil; isValuePresent {
			converted.Signature = &FormAnswerSignature{URL: answer.Signature.URL, Method: answer.Signature.Type}
		}
	case AnswerTypeMultiFormat:
		if isValuePresent = answer.MultiFormat != nil; isValuePresent {
			multiFormat := FormAnswerMultiFormat(*answer.MultiFormat)
			converted.MultiFormat = &multiFormat
		}
	}
	if !isValuePresent {
		return FormAnswer{}, errTypeformResponseMalformed
	}
	return converted, nil
}

// decodeAnswerChoices accepts both documented shapes: an object of parallel lists, or, for a checkbox,
// an array of choice objects.
func decodeAnswerChoices(value json.RawMessage) (*FormAnswerChoices, error) {
	trimmed := bytes.TrimSpace(value)
	switch {
	case len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")):
		return nil, errTypeformResponseMalformed
	case trimmed[0] == '[':
		var selected []FormAnswerChoice
		if json.Unmarshal(trimmed, &selected) != nil {
			return nil, errTypeformResponseMalformed
		}
		choices := &FormAnswerChoices{}
		for _, choice := range selected {
			choices.IDs = append(choices.IDs, choice.ID)
			choices.Labels = append(choices.Labels, choice.Label)
			choices.Refs = append(choices.Refs, choice.Ref)
			if choice.Other != "" {
				choices.Other = choice.Other
			}
		}
		return choices, nil
	default:
		var choices FormAnswerChoices
		if json.Unmarshal(trimmed, &choices) != nil {
			return nil, errTypeformResponseMalformed
		}
		return &choices, nil
	}
}

func derefString(value *string) (string, bool) {
	if value == nil {
		return "", false
	}
	return *value, true
}

// formatTypeformTimestamp formats a since or until filter as UTC to the second.
func formatTypeformTimestamp(value time.Time) string {
	return value.UTC().Format(typeformTimestampLayout)
}
