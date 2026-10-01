// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package publishpolicy demonstrates every Confluence operation in one Flow started from Dex Web Start
// Flow: find related policies in the space, publish the policy page, update the existing page with that
// title to its next version instead, read the published page back, and add a publication comment. An
// edit someone else saved first is parked for an operator instead of being overwritten.
package publishpolicy

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "ConfluencePolicyPublication"
	// ConnectionName is the static Dex Web connection for Confluence.
	ConnectionName = "confluence-policies"
	// PublishOverLatestVersionPermission is required by the conflict review Action.
	PublishOverLatestVersionPermission = "confluence-policy-publication.publish"

	recordPublicationRequestStepType = "RecordPublicationRequest"
	findRelatedPoliciesStepType      = "FindRelatedPolicies"
	recordRelatedPoliciesStepType    = "RecordRelatedPolicies"
	publishPolicyPageStepType        = "PublishPolicyPage"
	recordPublishedPageStepType      = "RecordPublishedPage"
	recordExistingPageStepType       = "RecordExistingPage"
	recordRejectedCreateStepType     = "RecordRejectedCreate"
	updatePolicyPageStepType         = "UpdatePolicyPage"
	recordUpdatedPageStepType        = "RecordUpdatedPage"
	recordEditConflictStepType       = "RecordEditConflict"
	recordRejectedUpdateStepType     = "RecordRejectedUpdate"
	readBackPolicyPageStepType       = "ReadBackPolicyPage"
	verifyPublishedPolicyStepType    = "VerifyPublishedPolicy"
	addPublicationCommentStepType    = "AddPublicationComment"
	completePublicationStepType      = "CompletePublication"

	maximumRelatedPolicies    = 10
	maximumReadBackCharacters = 4000
)

// Publication phases stored in the confluence-policy-publication-phase Attribute.
const (
	// PhaseSearching means the Flow is listing related policies in the space.
	PhaseSearching = "searching"
	// PhasePublishing means a createPage Step is about to run or running.
	PhasePublishing = "publishing"
	// PhaseUpdating means an updatePage Step is about to run or running.
	PhaseUpdating = "updating"
	// PhaseNeedsConflictReview means someone else saved an edit first; an operator decides.
	PhaseNeedsConflictReview = "needsConflictReview"
	// PhaseVerifying means the Flow is reading the published page back.
	PhaseVerifying = "verifying"
	// PhaseCommenting means the Flow is adding the publication comment.
	PhaseCommenting = "commenting"
	// PhasePublished means the page is published at the recorded version.
	PhasePublished = "published"
	// PhaseRejected means Confluence conclusively refused the publication and nothing was changed.
	PhaseRejected = "rejected"
)

var (
	publicationPhaseAttribute = dex.DefineAttribute[string]("confluence-policy-publication-phase")
	publicationAttribute      = dex.DefineAttribute[PolicyPublication]("confluence-policy-publication")
)

// Input is the policy entered in Dex Web Start Flow.
type Input struct {
	// Title is the one-line page title; an existing page with this title in the space is updated.
	Title string `json:"title"`
	// Body is the complete policy text in Markdown.
	Body string `json:"body"`
	// SpaceKey names the space, such as OPS, when no space was picked in Dex Web.
	SpaceKey string `json:"spaceKey,omitempty"`
	// ParentPageID places a new page below this page; blank uses the space homepage.
	ParentPageID string `json:"parentPageId,omitempty"`
	// ChangeSummary is the version message when an existing page is updated.
	ChangeSummary string `json:"changeSummary,omitempty"`
	// PublicationComment is added to the page as a footer comment; blank adds none.
	PublicationComment string `json:"publicationComment,omitempty"`
}

// SpaceSelection is the value the space picker saves for the PublishPolicyPage Step.
type SpaceSelection struct {
	// SpaceID is the picked space's numeric ID.
	SpaceID string `json:"spaceId,omitempty"`
	// SpaceKey is the picked space's key; blank uses the Start Flow spaceKey.
	SpaceKey string `json:"spaceKey,omitempty"`
	// SpaceName is the picked space's display name.
	SpaceName string `json:"spaceName,omitempty"`
}

// PublicationRequest is the validated request every later Step reads.
type PublicationRequest struct {
	// SpaceID is the picked space ID, or empty when the space is named by key.
	SpaceID string `json:"spaceId,omitempty"`
	// SpaceKey is the space key used for search and, without SpaceID, for the create.
	SpaceKey string `json:"spaceKey"`
	// ParentPageID places a new page below this page.
	ParentPageID string `json:"parentPageId,omitempty"`
	// Title is the trimmed page title.
	Title string `json:"title"`
	// Body is the Markdown policy text.
	Body string `json:"body"`
	// ChangeSummary is the version message for an update.
	ChangeSummary string `json:"changeSummary,omitempty"`
	// PublicationComment is the footer comment; blank adds none.
	PublicationComment string `json:"publicationComment,omitempty"`
}

// PageUpdateRequest is one update for the updatePage Step.
type PageUpdateRequest struct {
	// PageID is the page to update.
	PageID string `json:"pageId"`
	// NextVersionNumber is the version the update creates.
	NextVersionNumber int `json:"nextVersionNumber"`
	// Title is the page title after the update.
	Title string `json:"title"`
	// Body is the Markdown policy text that replaces the page body.
	Body string `json:"body"`
	// ChangeSummary is the version message.
	ChangeSummary string `json:"changeSummary,omitempty"`
}

// PageReference identifies the page the read-back Step reads.
type PageReference struct {
	// PageID is the published page.
	PageID string `json:"pageId"`
}

// CommentRequest is one footer comment for the addComment Step.
type CommentRequest struct {
	// PageID is the page to comment on.
	PageID string `json:"pageId"`
	// Body is the Markdown comment.
	Body string `json:"body"`
}

// RelatedPolicy is one page search found in the space.
type RelatedPolicy struct {
	// PageID is the page's ID.
	PageID string `json:"pageId"`
	// Title is the page's title.
	Title string `json:"title"`
	// VersionNumber is the page's version when search read it.
	VersionNumber int `json:"versionNumber"`
}

// PolicyPublication is the Flow's durable record of one publication.
type PolicyPublication struct {
	// Request is the validated request.
	Request PublicationRequest `json:"request"`
	// Phase mirrors the confluence-policy-publication-phase Attribute.
	Phase string `json:"phase"`
	// RelatedPolicies lists up to 10 pages search found for the title in the space.
	RelatedPolicies []RelatedPolicy `json:"relatedPolicies,omitempty"`
	// PageID is the published page.
	PageID string `json:"pageId,omitempty"`
	// VersionNumber is the published version.
	VersionNumber int `json:"versionNumber,omitempty"`
	// WebURL is the published page's address.
	WebURL string `json:"webUrl,omitempty"`
	// IsExistingPageUpdated reports that a page with the title existed and was updated.
	IsExistingPageUpdated bool `json:"isExistingPageUpdated,omitempty"`
	// IsConfirmedByReadBack reports that Confluence did not confirm a write and the connector found it.
	IsConfirmedByReadBack bool `json:"isConfirmedByReadBack,omitempty"`
	// ConflictingVersionNumber is the version another editor saved before the update.
	ConflictingVersionNumber int `json:"conflictingVersionNumber,omitempty"`
	// PublishedText is the start of the read-back page as Markdown.
	PublishedText string `json:"publishedText,omitempty"`
	// IsPublishedTextTruncated reports that PublishedText stopped at 4000 characters.
	IsPublishedTextTruncated bool `json:"isPublishedTextTruncated,omitempty"`
	// CommentID is the publication comment's ID once Confluence confirmed it.
	CommentID string `json:"commentId,omitempty"`
	// IsCommentOutcomeUnknown reports a comment Confluence may or may not have stored; it is never re-sent.
	IsCommentOutcomeUnknown bool `json:"isCommentOutcomeUnknown,omitempty"`
	// RejectionKind is the safe failure category when Confluence refused the publication.
	RejectionKind sdkgo.FailureKind `json:"rejectionKind,omitempty"`
}

// Flow publishes one Confluence policy page.
type Flow struct {
	dex.FlowDefaults
	connection confluence.Connection
	selection  SpaceSelection
}

// NewFlow binds the Confluence Connection and the picked space at registration time.
// A blank selection uses each Start Flow input's spaceKey.
func NewFlow(connection confluence.Connection, selection SpaceSelection) *Flow {
	selection.SpaceID, selection.SpaceKey = strings.TrimSpace(selection.SpaceID), strings.TrimSpace(selection.SpaceKey)
	return &Flow{connection: connection, selection: selection}
}

// SpaceSelectionConfigurationRef identifies the space picker value saved in Dex Web.
func SpaceSelectionConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: confluence.ConnectorID, ConnectionName: ConnectionName, OperationID: "createPage",
		FlowType: FlowType, StepType: publishPolicyPageStepType,
	}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Confluence connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordPublicationRequest{selection: flow.selection}),
		dex.DefineStep(confluence.NewSearchPagesStep(confluence.SearchPagesStepConfig[PublicationRequest]{
			StepType: findRelatedPoliciesStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "confluence", GroupLabel: "Confluence",
				Explanation: "Search the space for policy pages whose title contains the requested title.",
			},
			Connection: flow.connection, MapToOperationInput: MapToSearchPagesInput,
			Searched: sdkgo.GoTo(recordRelatedPolicies{}),
		})),
		dex.DefineStep(recordRelatedPolicies{}),
		dex.DefineStep(confluence.NewCreatePageStep(confluence.CreatePageStepConfig[PublicationRequest]{
			StepType: publishPolicyPageStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "confluence", GroupLabel: "Confluence",
				Explanation: "Publish the page once; a page that already has the title is updated instead.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "policySpace", UnitID: confluence.UIUnitSpacePicker, Label: "Policy space",
				Description: "Choose the Confluence space that holds the in-force policies; the related-policy search and the publication both use it, and a page there with the same title is updated to its next version. Leave it unsaved to use each Start Flow input's spaceKey. Restart the Worker after saving.",
				Bindings: []sdkgo.ConnectorUIBinding{
					{Port: confluence.UISpacePickerPortSpaceID, JSONPointer: "/spaceId"},
					{Port: confluence.UISpacePickerPortSpaceKey, JSONPointer: "/spaceKey"},
					{Port: confluence.UISpacePickerPortSpaceName, JSONPointer: "/spaceName"},
				},
			}}},
			Connection: flow.connection, MapToOperationInput: MapToCreatePageInput,
			Created:          sdkgo.GoTo(recordPublishedPage{}),
			TitleConflict:    sdkgo.GoTo(recordExistingPage{}),
			NotFound:         sdkgo.GoTo(recordRejectedCreate{}),
			ProviderRejected: sdkgo.GoTo(recordRejectedCreate{}),
		})),
		dex.DefineStep(recordPublishedPage{}),
		dex.DefineStep(recordExistingPage{}),
		dex.DefineStep(recordRejectedCreate{}),
		dex.DefineStep(confluence.NewUpdatePageStep(confluence.UpdatePageStepConfig[PageUpdateRequest]{
			StepType: updatePolicyPageStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "confluence", GroupLabel: "Confluence",
				Explanation: "Replace the page body as its next version; another editor's newer version is never overwritten.",
			},
			Connection: flow.connection, MapToOperationInput: MapToUpdatePageInput,
			Updated:          sdkgo.GoTo(recordUpdatedPage{}),
			VersionConflict:  sdkgo.GoTo(recordEditConflict{}),
			NotFound:         sdkgo.GoTo(recordRejectedUpdate{}),
			ProviderRejected: sdkgo.GoTo(recordRejectedUpdate{}),
		})),
		dex.DefineStep(recordUpdatedPage{}),
		dex.DefineStep(recordEditConflict{}),
		dex.DefineStep(recordRejectedUpdate{}),
		dex.DefineStep(confluence.NewGetPageStep(confluence.GetPageStepConfig[PageReference]{
			StepType: readBackPolicyPageStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "confluence", GroupLabel: "Confluence",
				Explanation: "Read the published page back as Markdown with its version number.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetPageInput,
			Found: sdkgo.GoTo(verifyPublishedPolicy{}),
		})),
		dex.DefineStep(verifyPublishedPolicy{}),
		dex.DefineStep(confluence.NewAddCommentStep(confluence.AddCommentStepConfig[CommentRequest]{
			StepType: addPublicationCommentStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "confluence", GroupLabel: "Confluence",
				Explanation: "Add the publication comment once; an unknown outcome is recorded, never re-sent.",
			},
			Connection: flow.connection, MapToOperationInput: MapToAddCommentInput,
			Added:     sdkgo.GoTo(completePublication{}),
			Uncertain: sdkgo.GoTo(completePublication{}),
		})),
		dex.DefineStep(completePublication{}),
	}
}

// GetRPCs returns the conflict review Action, the publication read RPC, and the Dex Web views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.PublishOverLatestVersion, &dex.RPCOptions{
			Action: dex.DefineAction(
				"Publish over latest version",
				dex.WhenAttributeMatches(publicationPhaseAttribute, dex.AttributeMatchEqual(PhaseNeedsConflictReview)),
				dex.ActionRequiresPermission(PublishOverLatestVersionPermission),
			),
			LockAttributes: []dex.AttributeLock{dex.LockAttribute(publicationPhaseAttribute), dex.LockAttribute(publicationAttribute)},
		}),
		dex.DefineRPC(flow.GetPolicyPublication, nil),
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the phase and publication Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{publicationPhaseAttribute, publicationAttribute}}
}

// MapToSearchPagesInput searches the space's policy pages for the title as a phrase.
func MapToSearchPagesInput(request PublicationRequest) confluence.SearchPagesInput {
	return confluence.SearchPagesInput{
		Filter:   confluence.PageSearchFilter{SpaceKeys: []string{request.SpaceKey}, TitlePhrase: request.Title},
		PageSize: maximumRelatedPolicies,
	}
}

// MapToCreatePageInput publishes the Markdown body in the picked space, or the space named by key.
func MapToCreatePageInput(request PublicationRequest) confluence.CreatePageInput {
	input := confluence.CreatePageInput{
		SpaceID: request.SpaceID, ParentPageID: request.ParentPageID, Title: request.Title, Body: request.Body,
		BodyFormat: confluence.TextFormatMarkdown,
	}
	if input.SpaceID == "" {
		input.SpaceKey = request.SpaceKey
	}
	return input
}

// MapToUpdatePageInput replaces the page body with the policy as the next version.
func MapToUpdatePageInput(update PageUpdateRequest) confluence.UpdatePageInput {
	return confluence.UpdatePageInput{
		PageID: update.PageID, Title: update.Title, Body: update.Body, BodyFormat: confluence.TextFormatMarkdown,
		NextVersionNumber: update.NextVersionNumber, VersionMessage: update.ChangeSummary,
	}
}

// NewPageUpdateRequest updates pageID from currentVersionNumber to the next version with the request's policy.
func NewPageUpdateRequest(request PublicationRequest, pageID string, currentVersionNumber int) PageUpdateRequest {
	return PageUpdateRequest{
		PageID: pageID, NextVersionNumber: currentVersionNumber + 1, Title: request.Title, Body: request.Body, ChangeSummary: request.ChangeSummary,
	}
}

// MapToGetPageInput reads the page back as bounded Markdown.
func MapToGetPageInput(reference PageReference) confluence.GetPageInput {
	return confluence.GetPageInput{PageID: reference.PageID, BodyFormat: confluence.TextFormatMarkdown, MaxBodyCharacters: maximumReadBackCharacters}
}

// MapToAddCommentInput maps the publication comment to one footer comment.
func MapToAddCommentInput(request CommentRequest) confluence.AddCommentInput {
	return confluence.AddCommentInput{PageID: request.PageID, Body: request.Body, BodyFormat: confluence.TextFormatMarkdown}
}

// PublishOverLatestVersion updates the page over the version another editor saved, after an operator
// reviewed that edit. The update is a new Step execution with a new connector call ID.
func (*Flow) PublishOverLatestVersion(ctx dex.Context, _ dex.None) (*dex.RPCResult[dex.None], error) {
	publication, err := publicationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if publication.Phase != PhaseNeedsConflictReview {
		return &dex.RPCResult[dex.None]{}, nil
	}
	update := NewPageUpdateRequest(publication.Request, publication.PageID, publication.ConflictingVersionNumber)
	publication.Phase = PhaseUpdating
	if err := publicationPhaseAttribute.Set(ctx, publication.Phase); err != nil {
		return nil, err
	}
	if err := publicationAttribute.Set(ctx, publication); err != nil {
		return nil, err
	}
	return &dex.RPCResult[dex.None]{
		NextSteps: []dex.StepMovement{dex.MovementOf(sdkgo.StepRef[PageUpdateRequest](updatePolicyPageStepType), update)},
	}, nil
}

// GetPolicyPublication returns the current publication record.
func (*Flow) GetPolicyPublication(ctx dex.Context, _ dex.None) (*dex.RPCResult[PolicyPublication], error) {
	publication, err := publicationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[PolicyPublication]{Output: publication}, nil
}

// GetDexSummary returns the phase and publication record for Dex Web lists.
//
// dex:field attribute-key:confluence-policy-publication-phase value-type:string editable:false description:"Publication phase"
// dex:field attribute-key:confluence-policy-publication value-type:json editable:false description:"Space, title, page, version, and comment"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	phase, err := optionalAttribute(ctx, publicationPhaseAttribute)
	if err != nil {
		return nil, err
	}
	publication, err := optionalAttribute(ctx, publicationAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"confluence-policy-publication-phase": phase,
		"confluence-policy-publication":       publication,
	}}, nil
}

// GetDexDisplay returns the phase and publication record for the Dex Web run view.
//
// dex:field attribute-key:confluence-policy-publication-phase value-type:string editable:false description:"Publication phase" ui-slot:status
// dex:field attribute-key:confluence-policy-publication value-type:json editable:false description:"Request, related policies, published version, read-back text, comment, and any edit conflict awaiting review"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	phase, err := optionalAttribute(ctx, publicationPhaseAttribute)
	if err != nil {
		return nil, err
	}
	publication, err := optionalAttribute(ctx, publicationAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"confluence-policy-publication-phase": phase,
		"confluence-policy-publication":       publication,
	}}, nil
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Validate the policy and record it with the picked space before calling Confluence."
type recordPublicationRequest struct {
	dex.StepDefaults
	selection SpaceSelection
}

func (recordPublicationRequest) GetStepType() string { return recordPublicationRequestStepType }

func (recordPublicationRequest) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(publicationPhaseAttribute), dex.LockAttribute(publicationAttribute)}}
}

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordPublicationRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (step recordPublicationRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	request, err := BuildPublicationRequest(step.selection, input)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	publication := PolicyPublication{Request: request, Phase: PhaseSearching}
	if err := publicationPhaseAttribute.Set(ctx, publication.Phase); err != nil {
		return nil, err
	}
	if err := publicationAttribute.Set(ctx, publication); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[PublicationRequest](findRelatedPoliciesStepType), request), nil
}

// BuildPublicationRequest validates Start Flow input. A picked space wins over the input's spaceKey.
func BuildPublicationRequest(selection SpaceSelection, input Input) (PublicationRequest, error) {
	request := PublicationRequest{
		SpaceID: selection.SpaceID, SpaceKey: selection.SpaceKey, ParentPageID: strings.TrimSpace(input.ParentPageID),
		Title: strings.TrimSpace(input.Title), Body: input.Body, ChangeSummary: strings.TrimSpace(input.ChangeSummary),
		PublicationComment: strings.TrimSpace(input.PublicationComment),
	}
	if request.SpaceKey == "" {
		request.SpaceID, request.SpaceKey = "", strings.TrimSpace(input.SpaceKey)
	}
	switch {
	case request.SpaceKey == "":
		return PublicationRequest{}, errors.New("pick a space on the PublishPolicyPage Step or set spaceKey")
	case request.Title == "":
		return PublicationRequest{}, errors.New("title is required")
	case strings.ContainsAny(request.Title, "\r\n"):
		return PublicationRequest{}, errors.New("title must be one line")
	case strings.TrimSpace(request.Body) == "":
		return PublicationRequest{}, errors.New("body is required")
	}
	return request, nil
}

// dex:group group-id:publication group-label:"Publication"
// dex:explanation text:"Record up to ten related policy pages, then publish the page."
type recordRelatedPolicies struct {
	dex.StepDefaultsNoWaitFor[confluence.SearchPagesResult]
}

func (recordRelatedPolicies) GetStepType() string { return recordRelatedPoliciesStepType }

func (recordRelatedPolicies) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(publicationPhaseAttribute), dex.LockAttribute(publicationAttribute)}}
}

func (recordRelatedPolicies) Execute(ctx dex.Context, result confluence.SearchPagesResult) (*dex.StepDecision, error) {
	publication, err := publicationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	publication.RelatedPolicies = nil
	for _, page := range result.Value.Pages {
		publication.RelatedPolicies = append(publication.RelatedPolicies, RelatedPolicy{PageID: page.ID, Title: page.Title, VersionNumber: page.VersionNumber})
	}
	publication.Phase = PhasePublishing
	if err := publicationPhaseAttribute.Set(ctx, publication.Phase); err != nil {
		return nil, err
	}
	if err := publicationAttribute.Set(ctx, publication); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[PublicationRequest](publishPolicyPageStepType), publication.Request), nil
}

// dex:group group-id:publication group-label:"Publication"
// dex:explanation text:"Record the new page and read it back."
type recordPublishedPage struct {
	dex.StepDefaultsNoWaitFor[confluence.CreatePageResult]
}

func (recordPublishedPage) GetStepType() string { return recordPublishedPageStepType }

func (recordPublishedPage) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(publicationPhaseAttribute), dex.LockAttribute(publicationAttribute)}}
}

func (recordPublishedPage) Execute(ctx dex.Context, result confluence.CreatePageResult) (*dex.StepDecision, error) {
	publication, err := publicationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	page := result.Value
	publication.PageID, publication.VersionNumber, publication.WebURL = page.PageID, page.VersionNumber, page.WebURL
	publication.IsConfirmedByReadBack = page.IsConfirmedByTitleLookup
	publication.Phase = PhaseVerifying
	if err := publicationPhaseAttribute.Set(ctx, publication.Phase); err != nil {
		return nil, err
	}
	if err := publicationAttribute.Set(ctx, publication); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[PageReference](readBackPolicyPageStepType), PageReference{PageID: page.PageID}), nil
}

// dex:group group-id:publication group-label:"Publication"
// dex:explanation text:"Update the page that already has the title to its next version."
type recordExistingPage struct {
	dex.StepDefaultsNoWaitFor[confluence.CreatePageResult]
}

func (recordExistingPage) GetStepType() string { return recordExistingPageStepType }

func (recordExistingPage) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(publicationPhaseAttribute), dex.LockAttribute(publicationAttribute)}}
}

func (recordExistingPage) Execute(ctx dex.Context, result confluence.CreatePageResult) (*dex.StepDecision, error) {
	publication, err := publicationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	publication.PageID, publication.IsExistingPageUpdated = result.Value.PageID, true
	publication.Phase = PhaseUpdating
	if err := publicationPhaseAttribute.Set(ctx, publication.Phase); err != nil {
		return nil, err
	}
	if err := publicationAttribute.Set(ctx, publication); err != nil {
		return nil, err
	}
	update := NewPageUpdateRequest(publication.Request, result.Value.PageID, result.Value.VersionNumber)
	return dex.GoTo(sdkgo.StepRef[PageUpdateRequest](updatePolicyPageStepType), update), nil
}

// dex:group group-id:publication group-label:"Publication"
// dex:explanation text:"Complete as rejected when Confluence refused the page or cannot find the space."
type recordRejectedCreate struct {
	dex.StepDefaultsNoWaitFor[confluence.CreatePageResult]
}

func (recordRejectedCreate) GetStepType() string { return recordRejectedCreateStepType }

func (recordRejectedCreate) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(publicationPhaseAttribute), dex.LockAttribute(publicationAttribute)}}
}

func (recordRejectedCreate) Execute(ctx dex.Context, result confluence.CreatePageResult) (*dex.StepDecision, error) {
	publication, err := publicationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	publication.Phase = PhaseRejected
	if result.Failure != nil {
		publication.RejectionKind = result.Failure.Kind
	}
	if err := publicationPhaseAttribute.Set(ctx, publication.Phase); err != nil {
		return nil, err
	}
	if err := publicationAttribute.Set(ctx, publication); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(publication), nil
}

// dex:group group-id:publication group-label:"Publication"
// dex:explanation text:"Record the updated version and read the page back."
type recordUpdatedPage struct {
	dex.StepDefaultsNoWaitFor[confluence.UpdatePageResult]
}

func (recordUpdatedPage) GetStepType() string { return recordUpdatedPageStepType }

func (recordUpdatedPage) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(publicationPhaseAttribute), dex.LockAttribute(publicationAttribute)}}
}

func (recordUpdatedPage) Execute(ctx dex.Context, result confluence.UpdatePageResult) (*dex.StepDecision, error) {
	publication, err := publicationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	publication.VersionNumber, publication.WebURL = result.Value.VersionNumber, result.Value.WebURL
	publication.IsConfirmedByReadBack = result.Value.IsConfirmedByReadBack
	publication.ConflictingVersionNumber = 0
	publication.Phase = PhaseVerifying
	if err := publicationPhaseAttribute.Set(ctx, publication.Phase); err != nil {
		return nil, err
	}
	if err := publicationAttribute.Set(ctx, publication); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[PageReference](readBackPolicyPageStepType), PageReference{PageID: publication.PageID}), nil
}

// dex:group group-id:review group-label:"Conflict review"
// dex:explanation text:"Park the publication for an operator when another editor saved a newer version first."
type recordEditConflict struct {
	dex.StepDefaultsNoWaitFor[confluence.UpdatePageResult]
}

func (recordEditConflict) GetStepType() string { return recordEditConflictStepType }

func (recordEditConflict) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(publicationPhaseAttribute), dex.LockAttribute(publicationAttribute)}}
}

func (recordEditConflict) Execute(ctx dex.Context, result confluence.UpdatePageResult) (*dex.StepDecision, error) {
	publication, err := publicationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	publication.ConflictingVersionNumber = result.Value.VersionNumber
	publication.Phase = PhaseNeedsConflictReview
	if err := publicationPhaseAttribute.Set(ctx, publication.Phase); err != nil {
		return nil, err
	}
	if err := publicationAttribute.Set(ctx, publication); err != nil {
		return nil, err
	}
	return dex.DeadEnd(), nil
}

// dex:group group-id:publication group-label:"Publication"
// dex:explanation text:"Complete as rejected when Confluence refused the update or the page is gone."
type recordRejectedUpdate struct {
	dex.StepDefaultsNoWaitFor[confluence.UpdatePageResult]
}

func (recordRejectedUpdate) GetStepType() string { return recordRejectedUpdateStepType }

func (recordRejectedUpdate) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(publicationPhaseAttribute), dex.LockAttribute(publicationAttribute)}}
}

func (recordRejectedUpdate) Execute(ctx dex.Context, result confluence.UpdatePageResult) (*dex.StepDecision, error) {
	publication, err := publicationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	publication.Phase = PhaseRejected
	if result.Failure != nil {
		publication.RejectionKind = result.Failure.Kind
	}
	if err := publicationPhaseAttribute.Set(ctx, publication.Phase); err != nil {
		return nil, err
	}
	if err := publicationAttribute.Set(ctx, publication); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(publication), nil
}

// dex:group group-id:publication group-label:"Publication"
// dex:explanation text:"Check the read-back version and title, then add the comment or complete."
type verifyPublishedPolicy struct {
	dex.StepDefaultsNoWaitFor[confluence.GetPageResult]
}

func (verifyPublishedPolicy) GetStepType() string { return verifyPublishedPolicyStepType }

func (verifyPublishedPolicy) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(publicationPhaseAttribute), dex.LockAttribute(publicationAttribute)}}
}

func (verifyPublishedPolicy) Execute(ctx dex.Context, result confluence.GetPageResult) (*dex.StepDecision, error) {
	publication, err := publicationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	page := result.Value
	if page.Title != publication.Request.Title || page.Version.Number < publication.VersionNumber {
		return dex.ForceFail("the read-back page does not show the published title and version"), nil
	}
	publication.PublishedText, publication.IsPublishedTextTruncated = page.Body, page.IsBodyTruncated
	if publication.Request.PublicationComment == "" {
		publication.Phase = PhasePublished
		if err := publicationPhaseAttribute.Set(ctx, publication.Phase); err != nil {
			return nil, err
		}
		if err := publicationAttribute.Set(ctx, publication); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(publication), nil
	}
	publication.Phase = PhaseCommenting
	if err := publicationPhaseAttribute.Set(ctx, publication.Phase); err != nil {
		return nil, err
	}
	if err := publicationAttribute.Set(ctx, publication); err != nil {
		return nil, err
	}
	comment := CommentRequest{PageID: publication.PageID, Body: publication.Request.PublicationComment}
	return dex.GoTo(sdkgo.StepRef[CommentRequest](addPublicationCommentStepType), comment), nil
}

// dex:group group-id:publication group-label:"Publication"
// dex:explanation text:"Record the comment ID, or that its outcome is unknown, and complete without re-sending."
type completePublication struct {
	dex.StepDefaultsNoWaitFor[confluence.AddCommentResult]
}

func (completePublication) GetStepType() string { return completePublicationStepType }

func (completePublication) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(publicationPhaseAttribute), dex.LockAttribute(publicationAttribute)}}
}

func (completePublication) Execute(ctx dex.Context, result confluence.AddCommentResult) (*dex.StepDecision, error) {
	publication, err := publicationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	publication.CommentID = result.Value.CommentID
	publication.IsCommentOutcomeUnknown = result.Branch == confluence.AddCommentBranchUncertain
	publication.Phase = PhasePublished
	if err := publicationPhaseAttribute.Set(ctx, publication.Phase); err != nil {
		return nil, err
	}
	if err := publicationAttribute.Set(ctx, publication); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(publication), nil
}

func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		var zero T
		return zero, nil
	}
	return value, err
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, dex.None] = (*Flow)(nil).PublishOverLatestVersion
var _ dex.RPC[dex.None, PolicyPublication] = (*Flow)(nil).GetPolicyPublication
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
