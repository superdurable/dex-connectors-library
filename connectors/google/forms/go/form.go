// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package forms

import (
	"encoding/json"
	"errors"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const getFormOperationID = "getForm"

// FormItemKind names the kind of one form item.
type FormItemKind string

const (
	// FormItemKindQuestion is an item with one question.
	FormItemKindQuestion FormItemKind = "question"
	// FormItemKindQuestionGroup is a grid whose rows are separate questions.
	FormItemKindQuestionGroup FormItemKind = "questionGroup"
	// FormItemKindPageBreak starts a new page, or section, titled by the item.
	FormItemKindPageBreak FormItemKind = "pageBreak"
	// FormItemKindText displays a title and description without a question.
	FormItemKindText FormItemKind = "text"
	// FormItemKindImage displays an image.
	FormItemKindImage FormItemKind = "image"
	// FormItemKindVideo displays a video.
	FormItemKindVideo FormItemKind = "video"
	// FormItemKindUnknown is an item kind this connector version does not know; it keeps its ID and title.
	FormItemKindUnknown FormItemKind = "unknown"
)

// QuestionType names how a respondent answers a question and how its answer text is formatted.
type QuestionType string

const (
	// QuestionTypeRadio picks exactly one choice; the answer has one value.
	QuestionTypeRadio QuestionType = "radio"
	// QuestionTypeCheckbox picks any number of choices; the answer has one value per choice.
	QuestionTypeCheckbox QuestionType = "checkbox"
	// QuestionTypeDropDown picks exactly one choice from a menu; the answer has one value.
	QuestionTypeDropDown QuestionType = "dropDown"
	// QuestionTypeShortText is a one-line text answer.
	QuestionTypeShortText QuestionType = "shortText"
	// QuestionTypeParagraph is a multi-line text answer.
	QuestionTypeParagraph QuestionType = "paragraph"
	// QuestionTypeScale picks a number from Scale; the answer is the number as text.
	QuestionTypeScale QuestionType = "scale"
	// QuestionTypeDate is a date, formatted MM-DD, YYYY-MM-DD, MM-DD HH:MM, or YYYY-MM-DD HH:MM by
	// IncludesYear and IncludesTime.
	QuestionTypeDate QuestionType = "date"
	// QuestionTypeTime is a time of day, or an elapsed time when IsDuration, formatted HH:MM.
	QuestionTypeTime QuestionType = "time"
	// QuestionTypeFileUpload uploads files to Google Drive; the answer lists the files.
	QuestionTypeFileUpload QuestionType = "fileUpload"
	// QuestionTypeRating picks 1 to RatingScaleLevel icons; the answer is the number as text.
	QuestionTypeRating QuestionType = "rating"
	// QuestionTypeRadioGrid is one row of a grid that picks one column; the answer has one value.
	QuestionTypeRadioGrid QuestionType = "radioGrid"
	// QuestionTypeCheckboxGrid is one row of a grid that picks any number of columns.
	QuestionTypeCheckboxGrid QuestionType = "checkboxGrid"
	// QuestionTypeUnknown is a question kind this connector version does not know; it keeps its ID and title.
	QuestionTypeUnknown QuestionType = "unknown"
)

// GetFormInput identifies one form.
type GetFormInput struct {
	// FormID is the form ID, such as the formPicker unit's formId or the ID in
	// https://docs.google.com/forms/d/FORM_ID/edit. It is also the form's Drive file ID.
	FormID string `json:"formId"`
}

// Form is one form's content in form order.
type Form struct {
	// FormID is the form ID.
	FormID string `json:"formId"`
	// Title is the title respondents see.
	Title string `json:"title"`
	// DocumentTitle is the file name shown in Google Drive.
	DocumentTitle string `json:"documentTitle,omitempty"`
	// Description is the form description respondents see.
	Description string `json:"description,omitempty"`
	// ResponderURI is the link respondents open to submit a response; its ID differs from FormID.
	ResponderURI string `json:"responderUri,omitempty"`
	// LinkedSheetID is the Google Sheets spreadsheet that collects responses, when the form has one.
	LinkedSheetID string `json:"linkedSheetId,omitempty"`
	// RevisionID changes when the form content changes. Google documents it as opaque and valid for
	// only 24 hours, so compare it only between reads made close together.
	RevisionID string `json:"revisionId,omitempty"`
	// IsQuiz reports a quiz form, whose responses carry grades and a total score.
	IsQuiz bool `json:"isQuiz,omitempty"`
	// EmailCollectionType is Google's email collection setting, such as DO_NOT_COLLECT, VERIFIED, or
	// RESPONDER_INPUT; blank when Google does not report it. Collected addresses appear as
	// FormResponse.RespondentEmail.
	EmailCollectionType string `json:"emailCollectionType,omitempty"`
	// PublishState reports whether the form is published and accepting responses; nil for a legacy form
	// without publish settings.
	PublishState *FormPublishState `json:"publishState,omitempty"`
	// Items lists every item, including section headers and media, in form order.
	Items []FormItem `json:"items"`
}

// FormPublishState is a form's publishing state.
type FormPublishState struct {
	// IsPublished reports that the form is published and visible to responders.
	IsPublished bool `json:"isPublished"`
	// IsAcceptingResponses reports that the form accepts new responses.
	IsAcceptingResponses bool `json:"isAcceptingResponses"`
}

// FormItem is one item of a form.
type FormItem struct {
	// ItemID is the item ID.
	ItemID string `json:"itemId"`
	// Kind is the item kind.
	Kind FormItemKind `json:"kind"`
	// Title is the item title: the question text of a question item, or the shared prompt of a grid.
	Title string `json:"title,omitempty"`
	// Description is the item's help text.
	Description string `json:"description,omitempty"`
	// Questions lists the item's questions: one for a question item, one per row for a grid, and
	// none for other kinds.
	Questions []FormQuestion `json:"questions,omitempty"`
}

// FormQuestion is one question that a FormResponse can answer under QuestionID.
type FormQuestion struct {
	// QuestionID is the question ID that keys its answer in every FormResponse.
	QuestionID string `json:"questionId"`
	// ItemID is the ID of the item that contains the question.
	ItemID string `json:"itemId"`
	// Title is the question text: the item title for a question item, or the row title for a grid row.
	Title string `json:"title"`
	// Type is the question type.
	Type QuestionType `json:"type"`
	// IsRequired reports that a respondent must answer the question to submit.
	IsRequired bool `json:"isRequired"`
	// Choices lists the options of a choice question, or the columns of a grid row, in form order.
	Choices []FormChoice `json:"choices,omitempty"`
	// Scale is the range of a scale question.
	Scale *FormScale `json:"scale,omitempty"`
	// RatingScaleLevel is the number of icons a rating question offers.
	RatingScaleLevel int `json:"ratingScaleLevel,omitempty"`
	// IncludesYear reports that a date question asks for the year.
	IncludesYear bool `json:"includesYear,omitempty"`
	// IncludesTime reports that a date question asks for a time of day too.
	IncludesTime bool `json:"includesTime,omitempty"`
	// IsDuration reports that a time question asks for an elapsed time instead of a time of day.
	IsDuration bool `json:"isDuration,omitempty"`
	// PointValue is the most points a quiz question awards automatically; zero for an ungraded question.
	PointValue int `json:"pointValue,omitempty"`
}

// FormChoice is one option of a choice question or one column of a grid.
type FormChoice struct {
	// Value is the option text, which is also the answer value when a respondent picks it.
	Value string `json:"value"`
	// IsOther reports the Other option, whose answer value is the respondent's own text.
	IsOther bool `json:"isOther,omitempty"`
}

// FormScale is the numeric range of a scale question.
type FormScale struct {
	// Low is the lowest selectable number.
	Low int `json:"low"`
	// High is the highest selectable number.
	High int `json:"high"`
	// LowLabel describes the lowest number.
	LowLabel string `json:"lowLabel,omitempty"`
	// HighLabel describes the highest number.
	HighLabel string `json:"highLabel,omitempty"`
}

// Questions returns every question of the form in form order, including each grid row.
func (form Form) Questions() []FormQuestion {
	var questions []FormQuestion
	for _, item := range form.Items {
		questions = append(questions, item.Questions...)
	}
	return questions
}

// QuestionByID returns the question that answers keyed by questionID belong to.
func (form Form) QuestionByID(questionID string) (FormQuestion, bool) {
	for _, item := range form.Items {
		for _, question := range item.Questions {
			if question.QuestionID == questionID {
				return question, true
			}
		}
	}
	return FormQuestion{}, false
}

// GetFormOperation implements the getForm connector operation.
type GetFormOperation struct{ client *Client }

var getFormReadBranches = readBranches{
	operationID: getFormOperationID, notFound: GetFormBranchNotFound, providerRejected: GetFormBranchProviderRejected,
	invalidResponse: GetFormBranchInvalidResponse, defect: GetFormBranchDefect,
}

type formResource struct {
	FormID string `json:"formId"`
	Info   struct {
		Title         string `json:"title"`
		DocumentTitle string `json:"documentTitle"`
		Description   string `json:"description"`
	} `json:"info"`
	Settings struct {
		QuizSettings struct {
			IsQuiz bool `json:"isQuiz"`
		} `json:"quizSettings"`
		EmailCollectionType string `json:"emailCollectionType"`
	} `json:"settings"`
	Items           []itemResource `json:"items"`
	RevisionID      string         `json:"revisionId"`
	ResponderURI    string         `json:"responderUri"`
	LinkedSheetID   string         `json:"linkedSheetId"`
	PublishSettings *struct {
		PublishState *FormPublishState `json:"publishState"`
	} `json:"publishSettings"`
}

type itemResource struct {
	ItemID       string `json:"itemId"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	QuestionItem *struct {
		Question questionResource `json:"question"`
	} `json:"questionItem"`
	QuestionGroupItem *struct {
		Questions []questionResource `json:"questions"`
		Grid      *struct {
			Columns choiceQuestionResource `json:"columns"`
		} `json:"grid"`
	} `json:"questionGroupItem"`
	PageBreakItem *json.RawMessage `json:"pageBreakItem"`
	TextItem      *json.RawMessage `json:"textItem"`
	ImageItem     *json.RawMessage `json:"imageItem"`
	VideoItem     *json.RawMessage `json:"videoItem"`
}

type questionResource struct {
	QuestionID string `json:"questionId"`
	Required   bool   `json:"required"`
	Grading    *struct {
		PointValue int `json:"pointValue"`
	} `json:"grading"`
	ChoiceQuestion *choiceQuestionResource `json:"choiceQuestion"`
	TextQuestion   *struct {
		Paragraph bool `json:"paragraph"`
	} `json:"textQuestion"`
	ScaleQuestion *FormScale `json:"scaleQuestion"`
	DateQuestion  *struct {
		IncludeTime bool `json:"includeTime"`
		IncludeYear bool `json:"includeYear"`
	} `json:"dateQuestion"`
	TimeQuestion *struct {
		Duration bool `json:"duration"`
	} `json:"timeQuestion"`
	FileUploadQuestion *json.RawMessage `json:"fileUploadQuestion"`
	RowQuestion        *struct {
		Title string `json:"title"`
	} `json:"rowQuestion"`
	RatingQuestion *struct {
		RatingScaleLevel int `json:"ratingScaleLevel"`
	} `json:"ratingQuestion"`
}

type choiceQuestionResource struct {
	Type    string `json:"type"`
	Options []struct {
		Value   string `json:"value"`
		IsOther bool   `json:"isOther"`
	} `json:"options"`
}

// Definition returns the immutable connector operation definition.
func (GetFormOperation) Definition() sdkgo.QueryDefinition { return GetFormDefinition }

// Invoke sends one forms.get request and classifies its attempt. A malformed
// form ID selects defect without a provider request.
func (operation GetFormOperation) Invoke(call sdkgo.Call, input GetFormInput) sdkgo.QueryAttempt[Form] {
	client := operation.client
	if !isGoogleID(input.FormID) {
		return sdkgo.NewQueryBranch(GetFormBranchDefect, Form{}, formsFailurePointer(sdkgo.FailureValidation, getFormOperationID, "formId must be a Google Forms form ID"), sdkgo.Receipt{})
	}
	credential, failure := client.resolveCredential(call, getFormOperationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetFormBranchDefect, Form{}, failure, sdkgo.Receipt{})
	}
	response, outcome := client.sendRead(call, &credential, getFormReadBranches, client.formURL(input.FormID))
	if outcome != nil {
		return queryAttemptFromReadOutcome[Form](outcome)
	}
	receipt := client.receipt(call, response.requestID, input.FormID)
	var resource formResource
	if err := json.Unmarshal(response.body, &resource); err != nil {
		return sdkgo.NewQueryBranch(GetFormBranchInvalidResponse, Form{}, formsFailurePointer(sdkgo.FailureProtocol, getFormOperationID, "provider returned an invalid form"), receipt)
	}
	form, err := convertFormResource(resource)
	if err != nil || form.FormID != input.FormID {
		return sdkgo.NewQueryBranch(GetFormBranchInvalidResponse, Form{}, formsFailurePointer(sdkgo.FailureProtocol, getFormOperationID, "provider returned an invalid form"), receipt)
	}
	return sdkgo.NewQueryBranch(GetFormBranchFound, form, nil, receipt)
}

// convertFormResource validates one untrusted form; responses key answers by unique question IDs.
func convertFormResource(resource formResource) (Form, error) {
	if !isGoogleID(resource.FormID) {
		return Form{}, errors.New("form lacks a valid ID")
	}
	form := Form{
		FormID: resource.FormID, Title: resource.Info.Title, DocumentTitle: resource.Info.DocumentTitle,
		Description: resource.Info.Description, ResponderURI: resource.ResponderURI, LinkedSheetID: resource.LinkedSheetID,
		RevisionID: resource.RevisionID, IsQuiz: resource.Settings.QuizSettings.IsQuiz,
		EmailCollectionType: resource.Settings.EmailCollectionType, Items: make([]FormItem, 0, len(resource.Items)),
	}
	if resource.PublishSettings != nil {
		form.PublishState = resource.PublishSettings.PublishState
	}
	questionIDs := map[string]bool{}
	for _, itemResource := range resource.Items {
		item, err := convertItemResource(itemResource)
		if err != nil {
			return Form{}, err
		}
		for _, question := range item.Questions {
			if questionIDs[question.QuestionID] {
				return Form{}, errors.New("form repeats a question ID")
			}
			questionIDs[question.QuestionID] = true
		}
		form.Items = append(form.Items, item)
	}
	return form, nil
}

func convertItemResource(resource itemResource) (FormItem, error) {
	if resource.ItemID == "" {
		return FormItem{}, errors.New("form item lacks an ID")
	}
	item := FormItem{ItemID: resource.ItemID, Title: resource.Title, Description: resource.Description}
	kindCount := 0
	if resource.QuestionItem != nil {
		kindCount++
		item.Kind = FormItemKindQuestion
		question, err := convertQuestionResource(resource.QuestionItem.Question, resource.ItemID, resource.Title, nil)
		if err != nil {
			return FormItem{}, err
		}
		item.Questions = []FormQuestion{question}
	}
	if group := resource.QuestionGroupItem; group != nil {
		kindCount++
		item.Kind = FormItemKindQuestionGroup
		for _, questionResource := range group.Questions {
			title := ""
			if questionResource.RowQuestion != nil {
				title = questionResource.RowQuestion.Title
			}
			var gridColumns *choiceQuestionResource
			if group.Grid != nil {
				gridColumns = &group.Grid.Columns
			}
			question, err := convertQuestionResource(questionResource, resource.ItemID, title, gridColumns)
			if err != nil {
				return FormItem{}, err
			}
			item.Questions = append(item.Questions, question)
		}
	}
	for _, contentItem := range []struct {
		resource *json.RawMessage
		kind     FormItemKind
	}{
		{resource.PageBreakItem, FormItemKindPageBreak}, {resource.TextItem, FormItemKindText},
		{resource.ImageItem, FormItemKindImage}, {resource.VideoItem, FormItemKindVideo},
	} {
		if contentItem.resource != nil {
			kindCount++
			item.Kind = contentItem.kind
		}
	}
	switch kindCount {
	case 0:
		item.Kind = FormItemKindUnknown
	case 1:
	default:
		return FormItem{}, errors.New("form item has more than one kind")
	}
	return item, nil
}

// convertQuestionResource maps one question; gridColumns is set for the rows of a grid.
func convertQuestionResource(resource questionResource, itemID string, title string, gridColumns *choiceQuestionResource) (FormQuestion, error) {
	if resource.QuestionID == "" {
		return FormQuestion{}, errors.New("form question lacks an ID")
	}
	question := FormQuestion{QuestionID: resource.QuestionID, ItemID: itemID, Title: title, IsRequired: resource.Required, Type: QuestionTypeUnknown}
	if resource.Grading != nil {
		question.PointValue = resource.Grading.PointValue
	}
	kindCount := 0
	if choice := resource.ChoiceQuestion; choice != nil {
		kindCount++
		question.Type = choiceQuestionTypes[choice.Type]
		question.Choices = convertChoices(*choice)
	}
	if resource.TextQuestion != nil {
		kindCount++
		question.Type = QuestionTypeShortText
		if resource.TextQuestion.Paragraph {
			question.Type = QuestionTypeParagraph
		}
	}
	if resource.ScaleQuestion != nil {
		kindCount++
		scale := *resource.ScaleQuestion
		question.Type, question.Scale = QuestionTypeScale, &scale
	}
	if date := resource.DateQuestion; date != nil {
		kindCount++
		question.Type, question.IncludesYear, question.IncludesTime = QuestionTypeDate, date.IncludeYear, date.IncludeTime
	}
	if resource.TimeQuestion != nil {
		kindCount++
		question.Type, question.IsDuration = QuestionTypeTime, resource.TimeQuestion.Duration
	}
	if resource.FileUploadQuestion != nil {
		kindCount++
		question.Type = QuestionTypeFileUpload
	}
	if resource.RatingQuestion != nil {
		kindCount++
		question.Type, question.RatingScaleLevel = QuestionTypeRating, resource.RatingQuestion.RatingScaleLevel
	}
	if resource.RowQuestion != nil {
		kindCount++
		if gridColumns != nil {
			question.Type = gridQuestionTypes[gridColumns.Type]
			question.Choices = convertChoices(*gridColumns)
		}
	}
	if kindCount > 1 {
		return FormQuestion{}, errors.New("form question has more than one kind")
	}
	if question.Type == "" {
		question.Type = QuestionTypeUnknown
	}
	return question, nil
}

// choiceQuestionTypes maps Google's ChoiceType; an unknown value yields "" and then QuestionTypeUnknown.
var choiceQuestionTypes = map[string]QuestionType{
	"RADIO": QuestionTypeRadio, "CHECKBOX": QuestionTypeCheckbox, "DROP_DOWN": QuestionTypeDropDown,
}

var gridQuestionTypes = map[string]QuestionType{"RADIO": QuestionTypeRadioGrid, "CHECKBOX": QuestionTypeCheckboxGrid}

func convertChoices(choice choiceQuestionResource) []FormChoice {
	choices := make([]FormChoice, 0, len(choice.Options))
	for _, option := range choice.Options {
		choices = append(choices, FormChoice{Value: option.Value, IsOther: option.IsOther})
	}
	return choices
}
