// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package forms

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	listResponsesOperationID = "listResponses"
	getResponseOperationID   = "getResponse"
)

// ListResponsesInput selects one page of a form's responses. Google documents
// no order for the listing, so read every page before treating a time as complete.
type ListResponsesInput struct {
	// FormID is the form ID, such as the formPicker unit's formId.
	FormID string `json:"formId"`
	// SubmittedAfter keeps responses submitted after, but not at, this time with Google's
	// "timestamp > N" filter; zero has no lower bound.
	SubmittedAfter time.Time `json:"submittedAfter,omitzero"`
	// SubmittedAtOrAfter keeps responses submitted at or after this time with Google's
	// "timestamp >= N" filter; zero has no lower bound. Set at most one of the two times.
	SubmittedAtOrAfter time.Time `json:"submittedAtOrAfter,omitzero"`
	// PageSize is 1 to 1000 responses; zero uses the connection's responsePageSize.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken continues a listing with its NextPageToken; blank reads the first page. Google requires
	// the same form and filter as the request that returned the token.
	PageToken string `json:"pageToken,omitempty"`
}

// ResponsePage is one page of a form's responses in Google's order.
type ResponsePage struct {
	// FormID is the form the responses answer.
	FormID string `json:"formId"`
	// Responses lists the page's responses. Google may return a short or empty page while
	// NextPageToken is set.
	Responses []FormResponse `json:"responses"`
	// NextPageToken continues the listing; blank means Google has no further pages.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// GetResponseInput identifies one response of one form.
type GetResponseInput struct {
	// FormID is the form ID, such as the formPicker unit's formId.
	FormID string `json:"formId"`
	// ResponseID is the response ID, such as a FormResponse ResponseID from listResponses.
	ResponseID string `json:"responseId"`
}

// FormResponse is one submitted response with its answers keyed by question ID.
type FormResponse struct {
	// ResponseID is the response ID. It stays the same when a respondent edits the response.
	ResponseID string `json:"responseId"`
	// FormID is the form the response answers.
	FormID string `json:"formId"`
	// CreatedAt is when the response was first submitted, in UTC.
	CreatedAt time.Time `json:"createdAt,omitzero"`
	// LastSubmittedAt is when the response was most recently submitted, in UTC. It is later than
	// CreatedAt after a respondent edits the response, and grade changes do not move it.
	LastSubmittedAt time.Time `json:"lastSubmittedAt"`
	// RespondentEmail is the respondent's address when the form collects it.
	RespondentEmail string `json:"respondentEmail,omitempty"`
	// TotalScore is the response's points when the form is a quiz and the response was graded.
	TotalScore *float64 `json:"totalScore,omitempty"`
	// Answers lists the answered questions sorted by question ID; Google omits a skipped question.
	Answers []FormAnswer `json:"answers"`
}

// FormAnswer is the answer to one question.
type FormAnswer struct {
	// QuestionID is the question's ID, as Form.QuestionByID finds it.
	QuestionID string `json:"questionId"`
	// Values holds a text answer as Google formats it: the selected option text, one value per
	// selected option for a checkbox question, the entered text, or a number, date, or time as text.
	// See QuestionType for each format.
	Values []string `json:"values,omitempty"`
	// Files lists the Google Drive files uploaded to a file upload question.
	Files []FormAnswerFile `json:"files,omitempty"`
	// Grade is the answer's grade when the form is a quiz.
	Grade *FormAnswerGrade `json:"grade,omitempty"`
}

// FormAnswerFile is one file uploaded to a file upload question.
type FormAnswerFile struct {
	// FileID is the uploaded file's Google Drive file ID.
	FileID string `json:"fileId"`
	// FileName is the file name stored in Google Drive.
	FileName string `json:"fileName,omitempty"`
	// MimeType is the file's MIME type in Google Drive.
	MimeType string `json:"mimeType,omitempty"`
}

// FormAnswerGrade is the grade of one quiz answer.
type FormAnswerGrade struct {
	// Score is the points awarded for the answer.
	Score float64 `json:"score"`
	// IsCorrect reports a correct answer; a zero score does not imply an incorrect one.
	IsCorrect bool `json:"isCorrect"`
}

// AnswerByQuestionID returns the answer to the question with questionID.
func (response FormResponse) AnswerByQuestionID(questionID string) (FormAnswer, bool) {
	for _, answer := range response.Answers {
		if answer.QuestionID == questionID {
			return answer, true
		}
	}
	return FormAnswer{}, false
}

// ListResponsesOperation implements the listResponses connector operation.
type ListResponsesOperation struct{ client *Client }

// GetResponseOperation implements the getResponse connector operation.
type GetResponseOperation struct{ client *Client }

var listResponsesReadBranches = readBranches{
	operationID: listResponsesOperationID, notFound: ListResponsesBranchNotFound,
	providerRejected: ListResponsesBranchProviderRejected, invalidResponse: ListResponsesBranchInvalidResponse,
	defect: ListResponsesBranchDefect,
}

var getResponseReadBranches = readBranches{
	operationID: getResponseOperationID, notFound: GetResponseBranchNotFound,
	providerRejected: GetResponseBranchProviderRejected, invalidResponse: GetResponseBranchInvalidResponse,
	defect: GetResponseBranchDefect,
}

type responseListResource struct {
	Responses     []responseResource `json:"responses"`
	NextPageToken string             `json:"nextPageToken"`
}

type responseResource struct {
	FormID            string                    `json:"formId"`
	ResponseID        string                    `json:"responseId"`
	CreateTime        time.Time                 `json:"createTime"`
	LastSubmittedTime time.Time                 `json:"lastSubmittedTime"`
	RespondentEmail   string                    `json:"respondentEmail"`
	Answers           map[string]answerResource `json:"answers"`
	TotalScore        *float64                  `json:"totalScore"`
}

type answerResource struct {
	QuestionID string `json:"questionId"`
	Grade      *struct {
		Score   float64 `json:"score"`
		Correct bool    `json:"correct"`
	} `json:"grade"`
	TextAnswers *struct {
		Answers []struct {
			Value string `json:"value"`
		} `json:"answers"`
	} `json:"textAnswers"`
	FileUploadAnswers *struct {
		Answers []FormAnswerFile `json:"answers"`
	} `json:"fileUploadAnswers"`
}

// Definition returns the immutable connector operation definition.
func (ListResponsesOperation) Definition() sdkgo.QueryDefinition { return ListResponsesDefinition }

// Invoke sends one forms.responses.list request and classifies its attempt.
// Invalid input selects defect without a provider request; an empty page is listed.
func (operation ListResponsesOperation) Invoke(call sdkgo.Call, input ListResponsesInput) sdkgo.QueryAttempt[ResponsePage] {
	client := operation.client
	query, pageSize, err := buildListResponsesQuery(input, client.responsePageSize)
	if err != nil {
		return sdkgo.NewQueryBranch(ListResponsesBranchDefect, ResponsePage{}, formsFailurePointer(sdkgo.FailureValidation, listResponsesOperationID, err.Error()), sdkgo.Receipt{})
	}
	credential, failure := client.resolveCredential(call, listResponsesOperationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListResponsesBranchDefect, ResponsePage{}, failure, sdkgo.Receipt{})
	}
	response, outcome := client.sendRead(call, &credential, listResponsesReadBranches, client.responsesURL(input.FormID, query))
	if outcome != nil {
		return queryAttemptFromReadOutcome[ResponsePage](outcome)
	}
	receipt := client.receipt(call, response.requestID, input.FormID)
	page, err := decodeResponsePage(response.body, input.FormID, pageSize)
	if err != nil {
		return sdkgo.NewQueryBranch(ListResponsesBranchInvalidResponse, ResponsePage{}, formsFailurePointer(sdkgo.FailureProtocol, listResponsesOperationID, "provider returned an invalid page of responses"), receipt)
	}
	return sdkgo.NewQueryBranch(ListResponsesBranchListed, page, nil, receipt)
}

// Definition returns the immutable connector operation definition.
func (GetResponseOperation) Definition() sdkgo.QueryDefinition { return GetResponseDefinition }

// Invoke sends one forms.responses.get request and classifies its attempt.
// A malformed form or response ID selects defect without a provider request.
func (operation GetResponseOperation) Invoke(call sdkgo.Call, input GetResponseInput) sdkgo.QueryAttempt[FormResponse] {
	client := operation.client
	if !isGoogleID(input.FormID) || !isGoogleID(input.ResponseID) {
		return sdkgo.NewQueryBranch(GetResponseBranchDefect, FormResponse{}, formsFailurePointer(sdkgo.FailureValidation, getResponseOperationID, "formId and responseId must be Google Forms IDs"), sdkgo.Receipt{})
	}
	credential, failure := client.resolveCredential(call, getResponseOperationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetResponseBranchDefect, FormResponse{}, failure, sdkgo.Receipt{})
	}
	response, outcome := client.sendRead(call, &credential, getResponseReadBranches, client.responseURL(input.FormID, input.ResponseID))
	if outcome != nil {
		return queryAttemptFromReadOutcome[FormResponse](outcome)
	}
	receipt := client.receipt(call, response.requestID, input.ResponseID)
	var resource responseResource
	if err := json.Unmarshal(response.body, &resource); err != nil {
		return sdkgo.NewQueryBranch(GetResponseBranchInvalidResponse, FormResponse{}, formsFailurePointer(sdkgo.FailureProtocol, getResponseOperationID, "provider returned an invalid response record"), receipt)
	}
	formResponse, err := convertResponseResource(resource, input.FormID)
	if err != nil || formResponse.ResponseID != input.ResponseID {
		return sdkgo.NewQueryBranch(GetResponseBranchInvalidResponse, FormResponse{}, formsFailurePointer(sdkgo.FailureProtocol, getResponseOperationID, "provider returned an invalid response record"), receipt)
	}
	return sdkgo.NewQueryBranch(GetResponseBranchFound, formResponse, nil, receipt)
}

// buildListResponsesQuery always sends pageSize, because Google returns up to 5000 responses without one.
func buildListResponsesQuery(input ListResponsesInput, defaultPageSize int) (url.Values, int, error) {
	if !isGoogleID(input.FormID) {
		return nil, 0, errors.New("formId must be a Google Forms form ID")
	}
	if !input.SubmittedAfter.IsZero() && !input.SubmittedAtOrAfter.IsZero() {
		return nil, 0, errors.New("set submittedAfter or submittedAtOrAfter, not both")
	}
	if len(input.PageToken) > maxPageTokenBytes || strings.ContainsFunc(input.PageToken, isUnsafePageTokenRune) {
		return nil, 0, errors.New("pageToken is not a Google Forms page token")
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = defaultPageSize
	}
	if pageSize < 1 || pageSize > maxResponsePageSize {
		return nil, 0, fmt.Errorf("pageSize must be from 1 to %d, or 0 for the connection default", maxResponsePageSize)
	}
	query := url.Values{"pageSize": {strconv.Itoa(pageSize)}}
	for _, filter := range []struct {
		operator string
		time     time.Time
	}{{">", input.SubmittedAfter}, {">=", input.SubmittedAtOrAfter}} {
		if filter.time.IsZero() {
			continue
		}
		utc := filter.time.UTC()
		if utc.Year() < 1 || utc.Year() > 9999 {
			return nil, 0, errors.New("submission time filters must be between years 1 and 9999")
		}
		query.Set("filter", "timestamp "+filter.operator+" "+utc.Format(time.RFC3339Nano))
	}
	if input.PageToken != "" {
		query.Set("pageToken", input.PageToken)
	}
	return query, pageSize, nil
}

func isUnsafePageTokenRune(character rune) bool {
	return character <= ' ' || character == 0x7f
}

func decodeResponsePage(content []byte, formID string, pageSize int) (ResponsePage, error) {
	var resource responseListResource
	if err := json.Unmarshal(content, &resource); err != nil {
		return ResponsePage{}, err
	}
	if len(resource.Responses) > pageSize {
		return ResponsePage{}, errors.New("page exceeds the requested page size")
	}
	page := ResponsePage{FormID: formID, Responses: make([]FormResponse, 0, len(resource.Responses)), NextPageToken: resource.NextPageToken}
	responseIDs := make(map[string]bool, len(resource.Responses))
	for _, responseResource := range resource.Responses {
		formResponse, err := convertResponseResource(responseResource, formID)
		if err != nil {
			return ResponsePage{}, err
		}
		if responseIDs[formResponse.ResponseID] {
			return ResponsePage{}, errors.New("page repeats a response")
		}
		responseIDs[formResponse.ResponseID] = true
		page.Responses = append(page.Responses, formResponse)
	}
	return page, nil
}

// convertResponseResource validates one untrusted response; a listed response omits its form ID.
func convertResponseResource(resource responseResource, formID string) (FormResponse, error) {
	if !isGoogleID(resource.ResponseID) || resource.LastSubmittedTime.IsZero() {
		return FormResponse{}, errors.New("response lacks a valid ID or submission time")
	}
	if resource.FormID != "" && resource.FormID != formID {
		return FormResponse{}, errors.New("response belongs to a different form")
	}
	formResponse := FormResponse{
		ResponseID: resource.ResponseID, FormID: formID, LastSubmittedAt: resource.LastSubmittedTime.UTC(),
		RespondentEmail: resource.RespondentEmail, TotalScore: resource.TotalScore,
		Answers: make([]FormAnswer, 0, len(resource.Answers)),
	}
	if !resource.CreateTime.IsZero() {
		formResponse.CreatedAt = resource.CreateTime.UTC()
	}
	for questionID, answerResource := range resource.Answers {
		if questionID == "" || (answerResource.QuestionID != "" && answerResource.QuestionID != questionID) {
			return FormResponse{}, errors.New("answer is keyed by a different question")
		}
		answer := FormAnswer{QuestionID: questionID}
		if answerResource.TextAnswers != nil {
			for _, textAnswer := range answerResource.TextAnswers.Answers {
				answer.Values = append(answer.Values, textAnswer.Value)
			}
		}
		if answerResource.FileUploadAnswers != nil {
			for _, file := range answerResource.FileUploadAnswers.Answers {
				if !isGoogleID(file.FileID) {
					return FormResponse{}, errors.New("uploaded file lacks a valid Drive file ID")
				}
				answer.Files = append(answer.Files, file)
			}
		}
		if answerResource.Grade != nil {
			answer.Grade = &FormAnswerGrade{Score: answerResource.Grade.Score, IsCorrect: answerResource.Grade.Correct}
		}
		formResponse.Answers = append(formResponse.Answers, answer)
	}
	sort.Slice(formResponse.Answers, func(i, j int) bool { return formResponse.Answers[i].QuestionID < formResponse.Answers[j].QuestionID })
	return formResponse, nil
}
