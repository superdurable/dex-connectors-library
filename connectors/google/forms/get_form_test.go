// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package forms_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/forms"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// intakeFormResource has one item of every documented kind, plus a kind Google may add later.
const intakeFormResource = `{"formId":"1FAIpQLintake","info":{"title":"Vendor intake","documentTitle":"Vendor intake (2026)","description":"New vendor request"},` +
	`"settings":{"quizSettings":{"isQuiz":true},"emailCollectionType":"VERIFIED"},"revisionId":"00000042",` +
	`"responderUri":"https://docs.google.com/forms/d/e/1FAIpQLSresponder/viewform","linkedSheetId":"sheet_1",` +
	`"publishSettings":{"publishState":{"isPublished":true,"isAcceptingResponses":true}},"items":[` +
	`{"itemId":"it_name","title":"Company name","questionItem":{"question":{"questionId":"q_name","required":true,"textQuestion":{}}}},` +
	`{"itemId":"it_notes","title":"Notes","description":"Anything else","questionItem":{"question":{"questionId":"q_notes","textQuestion":{"paragraph":true}}}},` +
	`{"itemId":"it_tier","title":"Tier","questionItem":{"question":{"questionId":"q_tier","grading":{"pointValue":2},"choiceQuestion":{"type":"RADIO","options":[{"value":"Gold"},{"value":"Silver"},{"isOther":true}]}}}},` +
	`{"itemId":"it_regions","title":"Regions","questionItem":{"question":{"questionId":"q_regions","choiceQuestion":{"type":"CHECKBOX","options":[{"value":"EU"},{"value":"US"}]}}}},` +
	`{"itemId":"it_country","title":"Country","questionItem":{"question":{"questionId":"q_country","choiceQuestion":{"type":"DROP_DOWN","options":[{"value":"DE"}]}}}},` +
	`{"itemId":"it_section","title":"Details","pageBreakItem":{}},` +
	`{"itemId":"it_risk","title":"Risk","questionItem":{"question":{"questionId":"q_risk","scaleQuestion":{"low":1,"high":5,"lowLabel":"Low","highLabel":"High"}}}},` +
	`{"itemId":"it_start","title":"Start date","questionItem":{"question":{"questionId":"q_start","dateQuestion":{"includeYear":true}}}},` +
	`{"itemId":"it_call","title":"Call length","questionItem":{"question":{"questionId":"q_call","timeQuestion":{"duration":true}}}},` +
	`{"itemId":"it_contract","title":"Contract","questionItem":{"question":{"questionId":"q_contract","fileUploadQuestion":{"folderId":"fld_uploads","maxFiles":1}}}},` +
	`{"itemId":"it_rating","title":"Rating","questionItem":{"question":{"questionId":"q_rating","ratingQuestion":{"ratingScaleLevel":5,"iconType":"STAR"}}}},` +
	`{"itemId":"it_grid","title":"Certifications","questionGroupItem":{"grid":{"columns":{"type":"CHECKBOX","options":[{"value":"ISO"},{"value":"SOC2"}]}},` +
	`"questions":[{"questionId":"q_grid_eu","rowQuestion":{"title":"EU entity"}},{"questionId":"q_grid_us","required":true,"rowQuestion":{"title":"US entity"}}]}},` +
	`{"itemId":"it_logo","imageItem":{"image":{"altText":"Logo"}}},` +
	`{"itemId":"it_video","title":"Intro","videoItem":{"video":{"youtubeUri":"https://youtu.be/x"}}},` +
	`{"itemId":"it_info","title":"Thanks","textItem":{}},` +
	`{"itemId":"it_future","title":"Future item","futureItem":{}},` +
	`{"itemId":"it_future_q","title":"Future question","questionItem":{"question":{"questionId":"q_future","futureQuestion":{}}}}]}`

func TestGetFormReturnsEveryItemAndQuestionInFormOrder(t *testing.T) {
	fake := newFakeForms(t, func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(t, response, http.StatusOK, intakeFormResource)
	})
	client := newFormsClient(t, fake.URL)

	result, err := sdkgo.RunQuery(newDexContext("get-form"), client.GetForm(), formsConnection, forms.GetFormInput{FormID: "1FAIpQLintake"})
	require.NoError(t, err)
	require.Equal(t, forms.GetFormBranchFound, result.Branch)
	require.Nil(t, result.Failure)
	form := result.Value
	require.Equal(t, "Vendor intake", form.Title)
	require.Equal(t, "Vendor intake (2026)", form.DocumentTitle)
	require.Equal(t, "https://docs.google.com/forms/d/e/1FAIpQLSresponder/viewform", form.ResponderURI)
	require.Equal(t, "sheet_1", form.LinkedSheetID)
	require.True(t, form.IsQuiz)
	require.Equal(t, "VERIFIED", form.EmailCollectionType)
	require.Equal(t, &forms.FormPublishState{IsPublished: true, IsAcceptingResponses: true}, form.PublishState)

	kinds := []forms.FormItemKind{}
	for _, item := range form.Items {
		kinds = append(kinds, item.Kind)
	}
	require.Equal(t, []forms.FormItemKind{
		forms.FormItemKindQuestion, forms.FormItemKindQuestion, forms.FormItemKindQuestion, forms.FormItemKindQuestion,
		forms.FormItemKindQuestion, forms.FormItemKindPageBreak, forms.FormItemKindQuestion, forms.FormItemKindQuestion,
		forms.FormItemKindQuestion, forms.FormItemKindQuestion, forms.FormItemKindQuestion, forms.FormItemKindQuestionGroup,
		forms.FormItemKindImage, forms.FormItemKindVideo, forms.FormItemKindText, forms.FormItemKindUnknown, forms.FormItemKindQuestion,
	}, kinds)

	types := map[string]forms.QuestionType{}
	for _, question := range form.Questions() {
		types[question.QuestionID] = question.Type
	}
	require.Equal(t, map[string]forms.QuestionType{
		"q_name": forms.QuestionTypeShortText, "q_notes": forms.QuestionTypeParagraph, "q_tier": forms.QuestionTypeRadio,
		"q_regions": forms.QuestionTypeCheckbox, "q_country": forms.QuestionTypeDropDown, "q_risk": forms.QuestionTypeScale,
		"q_start": forms.QuestionTypeDate, "q_call": forms.QuestionTypeTime, "q_contract": forms.QuestionTypeFileUpload,
		"q_rating": forms.QuestionTypeRating, "q_grid_eu": forms.QuestionTypeCheckboxGrid, "q_grid_us": forms.QuestionTypeCheckboxGrid,
		"q_future": forms.QuestionTypeUnknown,
	}, types)

	tier, ok := form.QuestionByID("q_tier")
	require.True(t, ok)
	require.Equal(t, forms.FormQuestion{
		QuestionID: "q_tier", ItemID: "it_tier", Title: "Tier", Type: forms.QuestionTypeRadio, PointValue: 2,
		Choices: []forms.FormChoice{{Value: "Gold"}, {Value: "Silver"}, {IsOther: true}},
	}, tier)
	gridRow, ok := form.QuestionByID("q_grid_us")
	require.True(t, ok)
	require.Equal(t, forms.FormQuestion{
		QuestionID: "q_grid_us", ItemID: "it_grid", Title: "US entity", Type: forms.QuestionTypeCheckboxGrid, IsRequired: true,
		Choices: []forms.FormChoice{{Value: "ISO"}, {Value: "SOC2"}},
	}, gridRow)
	require.Equal(t, "Certifications", form.Items[11].Title)
	risk, _ := form.QuestionByID("q_risk")
	require.Equal(t, &forms.FormScale{Low: 1, High: 5, LowLabel: "Low", HighLabel: "High"}, risk.Scale)
	start, _ := form.QuestionByID("q_start")
	require.True(t, start.IncludesYear)
	require.False(t, start.IncludesTime)
	call, _ := form.QuestionByID("q_call")
	require.True(t, call.IsDuration)
	rating, _ := form.QuestionByID("q_rating")
	require.Equal(t, 5, rating.RatingScaleLevel)
	_, ok = form.QuestionByID("q_missing")
	require.False(t, ok)

	require.Equal(t, "1FAIpQLintake", result.Receipt.ProviderObjectID)
	require.Equal(t, "google-request", result.Receipt.ProviderRequestID)
	request := fake.recorded()[0]
	require.Equal(t, http.MethodGet, request.method)
	require.Equal(t, "/v1/forms/1FAIpQLintake", request.path)
	require.Equal(t, "Bearer "+formsTestToken, request.header.Get("Authorization"))
}

func TestGetFormKeepsALegacyFormWithoutPublishSettings(t *testing.T) {
	fake := newFakeForms(t, func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(t, response, http.StatusOK, `{"formId":"legacy","info":{"title":"Old"},"items":[]}`)
	})
	result, err := sdkgo.RunQuery(newDexContext("get-legacy"), newFormsClient(t, fake.URL).GetForm(), formsConnection, forms.GetFormInput{FormID: "legacy"})
	require.NoError(t, err)
	require.Equal(t, forms.GetFormBranchFound, result.Branch)
	require.Nil(t, result.Value.PublishState)
	require.Empty(t, result.Value.Items)
	require.NotNil(t, result.Value.Items)
}

func TestGetFormClassifiesMissingRejectedAndMalformedForms(t *testing.T) {
	for _, test := range []struct {
		name       string
		formID     string
		status     int
		body       string
		wantBranch sdkgo.BranchID
		wantKind   sdkgo.FailureKind
		wantCalls  int
	}{
		{name: "missing form", formID: "gone", status: http.StatusNotFound, body: googleError(404, "NOT_FOUND", ""), wantBranch: forms.GetFormBranchNotFound, wantKind: sdkgo.FailureNotFound, wantCalls: 1},
		{name: "no access", formID: "private", status: http.StatusForbidden, body: googleError(403, "PERMISSION_DENIED", "ACCESS_TOKEN_SCOPE_INSUFFICIENT"), wantBranch: forms.GetFormBranchProviderRejected, wantKind: sdkgo.FailureAuthorization, wantCalls: 1},
		{name: "bad request", formID: "bad", status: http.StatusBadRequest, body: googleError(400, "INVALID_ARGUMENT", ""), wantBranch: forms.GetFormBranchProviderRejected, wantKind: sdkgo.FailureProviderRejection, wantCalls: 1},
		{name: "malformed JSON", formID: "f1", status: http.StatusOK, body: `{"formId":`, wantBranch: forms.GetFormBranchInvalidResponse, wantKind: sdkgo.FailureProtocol, wantCalls: 1},
		{name: "different form ID", formID: "f1", status: http.StatusOK, body: `{"formId":"f2","items":[]}`, wantBranch: forms.GetFormBranchInvalidResponse, wantKind: sdkgo.FailureProtocol, wantCalls: 1},
		{name: "question without ID", formID: "f1", status: http.StatusOK, body: `{"formId":"f1","items":[{"itemId":"i","questionItem":{"question":{"textQuestion":{}}}}]}`, wantBranch: forms.GetFormBranchInvalidResponse, wantKind: sdkgo.FailureProtocol, wantCalls: 1},
		{name: "repeated question ID", formID: "f1", status: http.StatusOK, body: `{"formId":"f1","items":[{"itemId":"a","questionItem":{"question":{"questionId":"q","textQuestion":{}}}},{"itemId":"b","questionItem":{"question":{"questionId":"q","textQuestion":{}}}}]}`, wantBranch: forms.GetFormBranchInvalidResponse, wantKind: sdkgo.FailureProtocol, wantCalls: 1},
		{name: "item with two kinds", formID: "f1", status: http.StatusOK, body: `{"formId":"f1","items":[{"itemId":"a","textItem":{},"pageBreakItem":{}}]}`, wantBranch: forms.GetFormBranchInvalidResponse, wantKind: sdkgo.FailureProtocol, wantCalls: 1},
		{name: "item without ID", formID: "f1", status: http.StatusOK, body: `{"formId":"f1","items":[{"textItem":{}}]}`, wantBranch: forms.GetFormBranchInvalidResponse, wantKind: sdkgo.FailureProtocol, wantCalls: 1},
		{name: "path traversal ID", formID: "../responses", wantBranch: forms.GetFormBranchDefect, wantKind: sdkgo.FailureValidation},
		{name: "edit link instead of ID", formID: "https://docs.google.com/forms/d/f1/edit", wantBranch: forms.GetFormBranchDefect, wantKind: sdkgo.FailureValidation},
		{name: "blank ID", formID: "", wantBranch: forms.GetFormBranchDefect, wantKind: sdkgo.FailureValidation},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeForms(t, func(response http.ResponseWriter, _ *http.Request) {
				writeJSON(t, response, test.status, test.body)
			})
			result, err := sdkgo.RunQuery(newDexContext("get-form-"+test.name), newFormsClient(t, fake.URL).GetForm(), formsConnection, forms.GetFormInput{FormID: test.formID})
			require.NoError(t, err)
			require.Equal(t, test.wantBranch, result.Branch)
			require.Equal(t, test.wantKind, result.Failure.Kind)
			require.NotContains(t, result.Failure.Message, "SENTINEL")
			require.Len(t, fake.recorded(), test.wantCalls)
		})
	}
}

func TestGetFormRejectsAnOversizedFormAsInvalidResponse(t *testing.T) {
	fake := newFakeForms(t, func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(t, response, http.StatusOK, intakeFormResource)
	})
	client := newFormsClient(t, fake.URL, forms.Config{MaxResponseBytes: 64})
	result, err := sdkgo.RunQuery(newDexContext("get-oversized"), client.GetForm(), formsConnection, forms.GetFormInput{FormID: "1FAIpQLintake"})
	require.NoError(t, err)
	require.Equal(t, forms.GetFormBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
}

func TestGetFormRefreshesOnceAfterUnauthorizedAndNeverLoops(t *testing.T) {
	for _, test := range []struct {
		name          string
		isReplacement bool
		wantBranch    sdkgo.BranchID
		wantKind      sdkgo.FailureKind
	}{
		{name: "replacement accepted", isReplacement: true, wantBranch: forms.GetFormBranchFound},
		{name: "replacement rejected", wantBranch: forms.GetFormBranchProviderRejected, wantKind: sdkgo.FailureAuthentication},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeForms(t, func(response http.ResponseWriter, request *http.Request) {
				if test.isReplacement && request.Header.Get("Authorization") == "Bearer replacement-token" {
					writeJSON(t, response, http.StatusOK, `{"formId":"f1","items":[]}`)
					return
				}
				writeJSON(t, response, http.StatusUnauthorized, googleError(401, "UNAUTHENTICATED", "ACCESS_TOKEN_EXPIRED"))
			})
			provider := &rejectionRefreshingCredentialProvider{}
			client, err := forms.New(forms.Config{Endpoint: fake.URL}, provider)
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(newDexContext("get-refresh"), client.GetForm(), formsConnection, forms.GetFormInput{FormID: "f1"})
			require.NoError(t, err)
			require.Equal(t, test.wantBranch, result.Branch)
			if test.wantKind != "" {
				require.Equal(t, test.wantKind, result.Failure.Kind)
			}
			require.Len(t, fake.recorded(), 2)
			require.Equal(t, 1, provider.forcedRefreshes)
		})
	}
}

func TestGetFormWithoutCredentialsUsesDefectWithoutARequest(t *testing.T) {
	fake := newFakeForms(t, func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(t, response, http.StatusOK, `{"formId":"f1","items":[]}`)
	})
	client, err := forms.New(forms.Config{Endpoint: fake.URL}, sdkgo.StaticCredentialProvider[forms.Credentials]{})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newDexContext("get-no-credentials"), client.GetForm(), formsConnection, forms.GetFormInput{FormID: "f1"})
	require.NoError(t, err)
	require.Equal(t, forms.GetFormBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Empty(t, fake.recorded())
}
