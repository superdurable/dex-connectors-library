// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package companynews demonstrates the NewsAPI searchArticles Query in a Flow
// started from Dex Web: it collects recent headlines that name one company.
package companynews

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/connectors/newsapi"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity that Dex Web Start Flow sends from the Flow Definition.
	FlowType = "NewsAPICompanyNews"
	// ConnectionName is the static Dex Web connection that holds the NewsAPI key.
	ConnectionName = "newsapi"

	searchCompanyArticlesStepType = "SearchCompanyArticles"
	maxCompanyCharacters          = 100
	// maxDays matches the Developer plan, which searches about one month back.
	maxDays            = 30
	defaultDays        = 10
	defaultMaxArticles = 20
	defaultLanguage    = "en"
)

var (
	companyNewsRequestAttribute = dex.DefineAttribute[CompanyNewsRequest]("newsapi-company-news-request")
	companyNewsOutcomeAttribute = dex.DefineAttribute[CompanyNewsOutcome]("newsapi-company-news-outcome")
)

// CompanyNewsRequest is the typed start input entered in Dex Web Start Flow.
type CompanyNewsRequest struct {
	// Company is matched against article titles, such as Acme Corporation.
	Company string `json:"company"`
	// Days is how far back to search, from 1 through 30. Zero selects 10.
	Days int `json:"days"`
	// MaxArticles bounds the page, from 1 through 100. Zero selects 20.
	MaxArticles int `json:"maxArticles"`
	// Language is a NewsAPI language code. Empty selects en.
	Language string `json:"language"`
	// From is the oldest publication date the search uses. RecordCompanyNewsRequest sets it once.
	From string `json:"from,omitempty"`
}

// Status is the CompanyNewsOutcome route that completed the Flow.
type Status string

const (
	// StatusFound means NewsAPI returned a page of matching articles, possibly empty.
	StatusFound Status = "found"
	// StatusRejected means NewsAPI conclusively rejected the search, such as an exhausted key.
	StatusRejected Status = "rejected"
)

// CompanyNewsOutcome is the Flow completion output.
type CompanyNewsOutcome struct {
	Status         Status            `json:"status"`
	Company        string            `json:"company"`
	From           string            `json:"from"`
	TotalResults   int               `json:"totalResults"`
	Articles       []newsapi.Article `json:"articles"`
	FailureKind    string            `json:"failureKind,omitempty"`
	FailureMessage string            `json:"failureMessage,omitempty"`
}

// Flow searches NewsAPI once for recent articles about one company.
type Flow struct {
	dex.FlowDefaults
	connection newsapi.Connection
}

// NewFlow binds the trusted NewsAPI Connection at registration time.
func NewFlow(connection newsapi.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordCompanyNewsRequest{}),
		dex.DefineStep(newsapi.NewSearchArticlesStep(newsapi.SearchArticlesStepConfig[CompanyNewsRequest]{
			StepType: searchCompanyArticlesStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "newsapi", GroupLabel: "NewsAPI",
				Explanation: "Search NewsAPI for the newest articles whose titles name the company.",
			},
			Connection:          flow.connection,
			MapToOperationInput: MapToSearchArticlesInput,
			Searched:            sdkgo.GoTo(companyArticlesFound{}),
			ProviderRejected:    sdkgo.GoTo(companyNewsRejected{}),
		})),
		dex.DefineStep(companyArticlesFound{}),
		dex.DefineStep(companyNewsRejected{}),
	}
}

func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{companyNewsRequestAttribute, companyNewsOutcomeAttribute}}
}

// dex:field attribute-key:newsapi-company-news-request value-type:json editable:false description:"Company news request"
// dex:field attribute-key:newsapi-company-news-outcome value-type:json editable:false description:"Company news outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := companyNewsInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"newsapi-company-news-request": request,
		"newsapi-company-news-outcome": outcome,
	}}, nil
}

// dex:field attribute-key:newsapi-company-news-request value-type:json editable:false description:"Company, search window, page size, and language"
// dex:field attribute-key:newsapi-company-news-outcome value-type:json editable:false description:"Matching articles, or NewsAPI's rejection"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := companyNewsInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"newsapi-company-news-request": request,
		"newsapi-company-news-outcome": outcome,
	}}, nil
}

func companyNewsInspection(ctx dex.Context) (CompanyNewsRequest, CompanyNewsOutcome, error) {
	request, err := optionalAttribute(ctx, companyNewsRequestAttribute)
	if err != nil {
		return CompanyNewsRequest{}, CompanyNewsOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, companyNewsOutcomeAttribute)
	if err != nil {
		return CompanyNewsRequest{}, CompanyNewsOutcome{}, err
	}
	return request, outcome, nil
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

// MapToSearchArticlesInput maps the recorded request to one NewsAPI page of the newest title matches.
func MapToSearchArticlesInput(request CompanyNewsRequest) newsapi.SearchArticlesInput {
	return newsapi.SearchArticlesInput{
		Query: request.Company, SearchIn: []string{"title"}, From: request.From,
		Language: request.Language, SortBy: "publishedAt", PageSize: request.MaxArticles,
	}
}

// PrepareCompanyNewsRequest validates request, applies defaults, and fixes the search window relative to now.
func PrepareCompanyNewsRequest(request CompanyNewsRequest, now time.Time) (CompanyNewsRequest, error) {
	request.Company = strings.TrimSpace(request.Company)
	if request.Company == "" || utf8.RuneCountInString(request.Company) > maxCompanyCharacters {
		return CompanyNewsRequest{}, fmt.Errorf("company must be 1 to %d characters", maxCompanyCharacters)
	}
	if request.Days == 0 {
		request.Days = defaultDays
	}
	if request.Days < 1 || request.Days > maxDays {
		return CompanyNewsRequest{}, fmt.Errorf("days must be from 1 through %d", maxDays)
	}
	if request.MaxArticles == 0 {
		request.MaxArticles = defaultMaxArticles
	}
	if request.MaxArticles < 1 || request.MaxArticles > 100 {
		return CompanyNewsRequest{}, fmt.Errorf("maxArticles must be from 1 through 100")
	}
	if request.Language == "" {
		request.Language = defaultLanguage
	}
	request.From = now.UTC().AddDate(0, 0, -request.Days).Format(time.DateOnly)
	return request, nil
}

// dex:group group-id:company-news group-label:"Company news"
// dex:explanation text:"Validate the company, fix the search window, and persist the request before calling NewsAPI."
type recordCompanyNewsRequest struct {
	dex.StepDefaults
}

func (recordCompanyNewsRequest) GetStepType() string { return "RecordCompanyNewsRequest" }

// WaitFor skips immediately; Dex Web Start Flow invokes the start Step's WaitFor, so StepDefaultsNoWaitFor cannot be used.
func (recordCompanyNewsRequest) WaitFor(dex.Context, CompanyNewsRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordCompanyNewsRequest) Execute(ctx dex.Context, request CompanyNewsRequest) (*dex.StepDecision, error) {
	prepared, err := PrepareCompanyNewsRequest(request, time.Now())
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := companyNewsRequestAttribute.Set(ctx, prepared); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[CompanyNewsRequest](searchCompanyArticlesStepType), prepared), nil
}

// dex:group group-id:company-news group-label:"Company news"
// dex:explanation text:"Record the matching articles and complete the Flow."
type companyArticlesFound struct {
	dex.StepDefaultsNoWaitFor[newsapi.SearchArticlesResult]
}

func (companyArticlesFound) GetStepType() string { return "CompanyArticlesFound" }

func (companyArticlesFound) Execute(ctx dex.Context, result newsapi.SearchArticlesResult) (*dex.StepDecision, error) {
	request, err := companyNewsRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := CompanyNewsOutcome{
		Status: StatusFound, Company: request.Company, From: request.From,
		TotalResults: result.Value.TotalResults, Articles: result.Value.Articles,
	}
	if err := companyNewsOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:company-news group-label:"Company news"
// dex:explanation text:"Record NewsAPI's rejection, such as an exhausted key, and complete without articles."
type companyNewsRejected struct {
	dex.StepDefaultsNoWaitFor[newsapi.SearchArticlesResult]
}

func (companyNewsRejected) GetStepType() string { return "CompanyNewsRejected" }

func (companyNewsRejected) Execute(ctx dex.Context, result newsapi.SearchArticlesResult) (*dex.StepDecision, error) {
	request, err := companyNewsRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := CompanyNewsOutcome{Status: StatusRejected, Company: request.Company, From: request.From, Articles: []newsapi.Article{}}
	if result.Failure != nil {
		outcome.FailureKind, outcome.FailureMessage = string(result.Failure.Kind), result.Failure.Message
	}
	if err := companyNewsOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
