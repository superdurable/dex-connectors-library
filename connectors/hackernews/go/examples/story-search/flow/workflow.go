// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package storysearch finds recent Hacker News stories that mention a keyword, reading result pages in turn.
package storysearch

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/hackernews/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// ConnectionName identifies the public, credential-free Hacker News connection.
const ConnectionName = "hacker-news-public"

const (
	defaultWithinHours     = 24
	maxWithinHours         = 720
	defaultMaxPages        = 4
	maxPages               = 20
	storiesPerPageRead     = 50
	searchStoriesStepID    = "SearchStories"
	getUserStepID          = "GetUser"
	searchItemsStepID      = "SearchItems"
	getArticleThreadStepID = "GetArticleThread"
)

// searchReport is the Flow's only state: the resolved request, the window, and the stories read so far.
var searchReport = dex.DefineAttribute[Report]("hacker-news-story-search-report")

// Request starts one search. Only Query is required.
type Request struct {
	// Query is the keyword query, such as "postgres" or "rust compiler". It must not be blank.
	Query string `json:"query"`
	// WithinHours keeps stories submitted in the last WithinHours hours, from 1 through 720. Zero selects 24.
	WithinHours int `json:"withinHours,omitempty"`
	// Tags narrows the search to show_hn, ask_hn, or front_page stories. Empty searches all stories.
	Tags []hackernews.StoryTag `json:"tags,omitempty"`
	// MinPoints keeps stories with at least this many points. Zero sets no minimum.
	MinPoints int `json:"minPoints,omitempty"`
	// SortBy is date or relevance. Empty selects date, which reads the newest stories first.
	SortBy hackernews.StorySearchSort `json:"sortBy,omitempty"`
	// MaxPages bounds the result pages read, from 1 through 20, at 50 stories per page. Zero selects 4.
	MaxPages int `json:"maxPages,omitempty"`
	// LookupFirstAuthor reads the first story author's public profile after all pages.
	LookupFirstAuthor bool `json:"lookupFirstAuthor,omitempty"`
	// ItemType searches story, comment, poll, or pollopt items instead of story results when set.
	ItemType string `json:"itemType,omitempty"`
	// ReadFirstThread includes the first matching story's nested public comments.
	ReadFirstThread bool `json:"readFirstThread,omitempty"`
}

// Report is the search result, available through Dex Web and as the completion result.
type Report struct {
	// Request is the start request with defaults applied.
	Request Request `json:"request"`
	// CreatedAfter is the fixed start of the search window, chosen when the Flow started.
	CreatedAfter time.Time `json:"createdAfter"`
	// Stories contains each matching story once, in the order the pages returned them.
	Stories []hackernews.StorySearchHit `json:"stories"`
	// TotalHits is the latest page's count of matching stories, which can be an estimate.
	TotalHits int `json:"totalHits"`
	// PagesRead counts the result pages read.
	PagesRead int `json:"pagesRead"`
	// HasUnreadPages reports that MaxPages stopped the search before the last result page.
	HasUnreadPages bool `json:"hasUnreadPages"`
	// FirstAuthor is the first story author's public profile when requested and available.
	FirstAuthor *hackernews.User `json:"firstAuthor,omitempty"`
	// Items contains mixed search results when ItemType is set.
	Items []hackernews.ItemSearchHit `json:"items,omitempty"`
	// FirstThread is the first story's article and public comment tree when requested.
	FirstThread *hackernews.ArticleThread `json:"firstThread,omitempty"`
}

// StorySearchFlow searches Hacker News once and completes with the matching stories.
type StorySearchFlow struct {
	dex.FlowDefaults
	connection hackernews.Connection
}

// NewStorySearchFlow binds the public Hacker News connection, which is never persisted.
func NewStorySearchFlow(connection hackernews.Connection) *StorySearchFlow {
	return &StorySearchFlow{connection: connection}
}

// GetSteps registers request validation, the generated search Step, and page collection.
func (flow *StorySearchFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startStorySearch{}),
		dex.DefineStep(hackernews.NewSearchStoriesStep(hackernews.SearchStoriesStepConfig[hackernews.SearchStoriesInput]{
			StepType: searchStoriesStepID, ConnectionName: ConnectionName, Connection: flow.connection,
			MapToOperationInput: func(input hackernews.SearchStoriesInput) hackernews.SearchStoriesInput { return input },
			Annotations:         sdkgo.StepAnnotations{GroupID: "search", GroupLabel: "Search", Explanation: "Read one page of matching stories from Hacker News Search."},
			Searched:            sdkgo.GoTo(recordStoryPage{}),
		})),
		dex.DefineStep(recordStoryPage{}),
		dex.DefineStep(hackernews.NewSearchItemsStep(hackernews.SearchItemsStepConfig[hackernews.SearchItemsInput]{
			StepType: searchItemsStepID, ConnectionName: ConnectionName, Connection: flow.connection,
			MapToOperationInput: func(input hackernews.SearchItemsInput) hackernews.SearchItemsInput { return input },
			Annotations:         sdkgo.StepAnnotations{GroupID: "search", GroupLabel: "Search", Explanation: "Read one page of public Hacker News items by type."},
			Searched:            sdkgo.GoTo(recordItemPage{}),
		})),
		dex.DefineStep(recordItemPage{}),
		dex.DefineStep(hackernews.NewGetArticleThreadStep(hackernews.GetArticleThreadStepConfig[hackernews.GetArticleThreadInput]{
			StepType: getArticleThreadStepID, ConnectionName: ConnectionName, Connection: flow.connection,
			MapToOperationInput: func(input hackernews.GetArticleThreadInput) hackernews.GetArticleThreadInput { return input },
			Annotations:         sdkgo.StepAnnotations{GroupID: "search", GroupLabel: "Search", Explanation: "Read the first story's nested public comments."},
			Found:               sdkgo.GoTo(recordFirstThread{}),
		})),
		dex.DefineStep(recordFirstThread{}),
		dex.DefineStep(hackernews.NewGetUserStep(hackernews.GetUserStepConfig[hackernews.GetUserInput]{
			StepType: getUserStepID, ConnectionName: ConnectionName, Connection: flow.connection,
			MapToOperationInput: func(input hackernews.GetUserInput) hackernews.GetUserInput { return input },
			Annotations:         sdkgo.StepAnnotations{GroupID: "search", GroupLabel: "Search", Explanation: "Read the first matching story author's public profile."},
			Found:               sdkgo.GoTo(recordFirstAuthor{}),
		})),
		dex.DefineStep(recordFirstAuthor{}),
	}
}

// GetPersistenceSchema registers the search report.
func (*StorySearchFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{searchReport}}
}

// GetRPCs exposes the report in the Dex Web run list and details.
func (flow *StorySearchFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the report; before the first page it returns an empty Report.
// dex:field attribute-key:hacker-news-story-search-report value-type:json editable:false description:"Search request, window, and matching stories"
func (*StorySearchFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	report, err := inspectReport(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"hacker-news-story-search-report": report}}, nil
}

// GetDexDisplay returns the report without making provider calls.
// dex:field attribute-key:hacker-news-story-search-report value-type:json editable:false description:"Matching stories with source and discussion links"
func (*StorySearchFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	report, err := inspectReport(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"hacker-news-story-search-report": report}}, nil
}

func inspectReport(ctx dex.Context) (Report, error) {
	report, err := searchReport.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &missing) {
		return Report{}, err
	}
	return report, nil
}

// dex:group group-id:search group-label:"Search"
// dex:explanation text:"Validate the request and fix the search window before reading the first page."
type startStorySearch struct {
	dex.StepDefaultsNoWaitFor[Request]
}

func (startStorySearch) Execute(ctx dex.Context, request Request) (*dex.StepDecision, error) {
	request, err := resolveRequest(request)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	report := Report{
		Request: request, CreatedAfter: time.Now().UTC().Add(-time.Duration(request.WithinHours) * time.Hour).Truncate(time.Second),
		Stories: []hackernews.StorySearchHit{},
	}
	if err := searchReport.Set(ctx, report); err != nil {
		return nil, err
	}
	if request.ItemType != "" {
		return dex.GoTo(sdkgo.StepRef[hackernews.SearchItemsInput](searchItemsStepID), itemSearchInput(report, 0)), nil
	}
	return dex.GoTo(sdkgo.StepRef[hackernews.SearchStoriesInput](searchStoriesStepID), searchInput(report, 0)), nil
}

// dex:group group-id:search group-label:"Search"
// dex:explanation text:"Keep public items and follow their pages until the budget is reached."
type recordItemPage struct {
	dex.StepDefaultsNoWaitFor[hackernews.SearchItemsResult]
}

func (recordItemPage) Execute(ctx dex.Context, result hackernews.SearchItemsResult) (*dex.StepDecision, error) {
	report, err := searchReport.Get(ctx)
	if err != nil {
		return nil, err
	}
	report.Items = append(report.Items, result.Value.Items...)
	report.TotalHits = result.Value.TotalHits
	report.PagesRead++
	report.HasUnreadPages = result.Value.NextPage != 0 && report.PagesRead >= report.Request.MaxPages
	if err := searchReport.Set(ctx, report); err != nil {
		return nil, err
	}
	if result.Value.NextPage != 0 && !report.HasUnreadPages {
		return dex.GoTo(sdkgo.StepRef[hackernews.SearchItemsInput](searchItemsStepID), itemSearchInput(report, result.Value.NextPage)), nil
	}
	return dex.GracefulComplete(report), nil
}

// dex:group group-id:search group-label:"Search"
// dex:explanation text:"Keep each new story once and read the next page until the results or the page budget run out."
type recordStoryPage struct {
	dex.StepDefaultsNoWaitFor[hackernews.SearchStoriesResult]
}

func (recordStoryPage) Execute(ctx dex.Context, result hackernews.SearchStoriesResult) (*dex.StepDecision, error) {
	report, err := searchReport.Get(ctx)
	if err != nil {
		return nil, err
	}
	report.Stories = appendNewStories(report.Stories, result.Value.Stories)
	report.TotalHits = result.Value.TotalHits
	report.PagesRead++
	report.HasUnreadPages = result.Value.NextPage != 0 && report.PagesRead >= report.Request.MaxPages
	if err := searchReport.Set(ctx, report); err != nil {
		return nil, err
	}
	if result.Value.NextPage != 0 && !report.HasUnreadPages {
		return dex.GoTo(sdkgo.StepRef[hackernews.SearchStoriesInput](searchStoriesStepID), searchInput(report, result.Value.NextPage)), nil
	}
	if report.Request.ReadFirstThread && len(report.Stories) > 0 {
		return dex.GoTo(sdkgo.StepRef[hackernews.GetArticleThreadInput](getArticleThreadStepID), hackernews.GetArticleThreadInput{ID: report.Stories[0].ID}), nil
	}
	if report.Request.LookupFirstAuthor && len(report.Stories) > 0 && report.Stories[0].Author != "" {
		return dex.GoTo(sdkgo.StepRef[hackernews.GetUserInput](getUserStepID), hackernews.GetUserInput{ID: report.Stories[0].Author}), nil
	}
	return dex.GracefulComplete(report), nil
}

// dex:group group-id:search group-label:"Search"
// dex:explanation text:"Store the first matching story's nested public comment tree."
type recordFirstThread struct {
	dex.StepDefaultsNoWaitFor[hackernews.GetArticleThreadResult]
}

func (recordFirstThread) Execute(ctx dex.Context, result hackernews.GetArticleThreadResult) (*dex.StepDecision, error) {
	report, err := searchReport.Get(ctx)
	if err != nil {
		return nil, err
	}
	report.FirstThread = &result.Value
	if err := searchReport.Set(ctx, report); err != nil {
		return nil, err
	}
	if report.Request.LookupFirstAuthor && len(report.Stories) > 0 && report.Stories[0].Author != "" {
		return dex.GoTo(sdkgo.StepRef[hackernews.GetUserInput](getUserStepID), hackernews.GetUserInput{ID: report.Stories[0].Author}), nil
	}
	return dex.GracefulComplete(report), nil
}

// dex:group group-id:search group-label:"Search"
// dex:explanation text:"Store the requested author's public profile with the matching stories."
type recordFirstAuthor struct {
	dex.StepDefaultsNoWaitFor[hackernews.GetUserResult]
}

func (recordFirstAuthor) Execute(ctx dex.Context, result hackernews.GetUserResult) (*dex.StepDecision, error) {
	report, err := searchReport.Get(ctx)
	if err != nil {
		return nil, err
	}
	report.FirstAuthor = &result.Value
	if err := searchReport.Set(ctx, report); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(report), nil
}

func resolveRequest(request Request) (Request, error) {
	if strings.TrimSpace(request.Query) == "" {
		return Request{}, errors.New("query is required")
	}
	if request.WithinHours == 0 {
		request.WithinHours = defaultWithinHours
	}
	if request.MaxPages == 0 {
		request.MaxPages = defaultMaxPages
	}
	if request.SortBy == "" {
		request.SortBy = hackernews.StorySearchSortDate
	}
	if request.ItemType != "" && request.ItemType != "story" && request.ItemType != "comment" && request.ItemType != "poll" && request.ItemType != "pollopt" {
		return Request{}, errors.New("itemType must be story, comment, poll, or pollopt")
	}
	if request.ItemType != "" && request.LookupFirstAuthor {
		return Request{}, errors.New("lookupFirstAuthor is available only for story search")
	}
	if request.ItemType != "" && request.ReadFirstThread {
		return Request{}, errors.New("readFirstThread is available only for story search")
	}
	if request.WithinHours < 1 || request.WithinHours > maxWithinHours || request.MaxPages < 1 || request.MaxPages > maxPages {
		return Request{}, errors.New("withinHours must be 1..720 and maxPages must be 1..20")
	}
	return request, nil
}

func itemSearchInput(report Report, page int) hackernews.SearchItemsInput {
	return hackernews.SearchItemsInput{Query: report.Request.Query, Type: report.Request.ItemType, SortBy: report.Request.SortBy, CreatedAfter: report.CreatedAfter, Page: page, HitsPerPage: storiesPerPageRead}
}

func searchInput(report Report, page int) hackernews.SearchStoriesInput {
	return hackernews.SearchStoriesInput{
		Query: report.Request.Query, SortBy: report.Request.SortBy, Tags: report.Request.Tags, CreatedAfter: report.CreatedAfter,
		MinPoints: report.Request.MinPoints, Page: page, HitsPerPage: storiesPerPageRead,
	}
}

// appendNewStories skips stories already kept; a newest-first page can repeat one when newer stories arrive.
func appendNewStories(kept []hackernews.StorySearchHit, page []hackernews.StorySearchHit) []hackernews.StorySearchHit {
	for _, story := range page {
		isKept := slices.ContainsFunc(kept, func(existing hackernews.StorySearchHit) bool { return existing.ID == story.ID })
		if !isKept {
			kept = append(kept, story)
		}
	}
	return kept
}
