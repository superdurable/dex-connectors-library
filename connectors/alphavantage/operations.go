// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package alphavantage

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// StockInput selects one equity symbol such as IBM.
type StockInput struct {
	// Symbol is the provider equity ticker; dots and hyphens support global symbols.
	Symbol string `json:"symbol"`
}

// GlobalQuoteOutput contains the latest provider quote for one equity.
type GlobalQuoteOutput struct {
	// Symbol is the provider's ticker.
	Symbol string `json:"symbol"`
	// Price is the decimal price string from Alpha Vantage.
	Price string `json:"price"`
	// LatestTradingDay is the quote's YYYY-MM-DD trading date.
	LatestTradingDay string `json:"latestTradingDay"`
}

// DailyPrice is one raw daily OHLCV record.
type DailyPrice struct {
	// Date is the YYYY-MM-DD trading date.
	Date string `json:"date"`
	// Open is the raw opening price decimal string.
	Open string `json:"open"`
	// High is the raw daily high decimal string.
	High string `json:"high"`
	// Low is the raw daily low decimal string.
	Low string `json:"low"`
	// Close is the raw closing price decimal string.
	Close string `json:"close"`
	// Volume is the traded share count string.
	Volume string `json:"volume"`
}

// DailySeriesOutput contains up to the provider's compact 100 daily points.
type DailySeriesOutput struct {
	// Symbol is the requested equity ticker.
	Symbol string `json:"symbol"`
	// Days are newest first.
	Days []DailyPrice `json:"days"`
}

// GetGlobalQuoteOperation retrieves one global quote.
type GetGlobalQuoteOperation struct{ client *Client }

// GetTimeSeriesDailyOperation retrieves compact raw daily prices.
type GetTimeSeriesDailyOperation struct{ client *Client }

// GetGlobalQuote binds the query to this client.
func (client *Client) GetGlobalQuote() GetGlobalQuoteOperation {
	return GetGlobalQuoteOperation{client}
}

// GetTimeSeriesDaily binds the query to this client.
func (client *Client) GetTimeSeriesDaily() GetTimeSeriesDailyOperation {
	return GetTimeSeriesDailyOperation{client}
}

// Definition returns the generated quote contract.
func (GetGlobalQuoteOperation) Definition() sdkgo.QueryDefinition { return GetGlobalQuoteDefinition }

// Definition returns the generated daily-series contract.
func (GetTimeSeriesDailyOperation) Definition() sdkgo.QueryDefinition {
	return GetTimeSeriesDailyDefinition
}

// Invoke requests the provider's latest global quote.
func (operation GetGlobalQuoteOperation) Invoke(call sdkgo.Call, input StockInput) sdkgo.QueryAttempt[GlobalQuoteOutput] {
	branches := queryBranches{found: GetGlobalQuoteBranchFound, notFound: GetGlobalQuoteBranchNotFound, rejected: GetGlobalQuoteBranchProviderRejected, invalid: GetGlobalQuoteBranchInvalidResponse, defect: GetGlobalQuoteBranchDefect}
	if !symbolPattern.MatchString(input.Symbol) {
		return queryBranch("getGlobalQuote", branches.defect, GlobalQuoteOutput{}, sdkgo.FailureValidation, "symbol is invalid")
	}
	query := url.Values{"function": {"GLOBAL_QUOTE"}, "symbol": {input.Symbol}}
	return invokeJSONQuery(call, operation.client, "getGlobalQuote", branches, http.MethodGet, "/query", query, nil, decodeGlobalQuote)
}

// Invoke requests the provider's compact daily time series.
func (operation GetTimeSeriesDailyOperation) Invoke(call sdkgo.Call, input StockInput) sdkgo.QueryAttempt[DailySeriesOutput] {
	branches := queryBranches{found: GetTimeSeriesDailyBranchFound, notFound: GetTimeSeriesDailyBranchNotFound, rejected: GetTimeSeriesDailyBranchProviderRejected, invalid: GetTimeSeriesDailyBranchInvalidResponse, defect: GetTimeSeriesDailyBranchDefect}
	if !symbolPattern.MatchString(input.Symbol) {
		return queryBranch("getTimeSeriesDaily", branches.defect, DailySeriesOutput{}, sdkgo.FailureValidation, "symbol is invalid")
	}
	query := url.Values{"function": {"TIME_SERIES_DAILY"}, "symbol": {input.Symbol}, "outputsize": {"compact"}}
	return invokeJSONQuery(call, operation.client, "getTimeSeriesDaily", branches, http.MethodGet, "/query", query, nil, func(body []byte) (DailySeriesOutput, error) { return decodeDailySeries(body, input.Symbol) })
}

var symbolPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,24}$`)
var errNoData = errors.New("provider returned no data")

func decodeGlobalQuote(body []byte) (GlobalQuoteOutput, error) {
	var wire struct {
		Quote struct {
			Symbol           string `json:"01. symbol"`
			Price            string `json:"05. price"`
			LatestTradingDay string `json:"07. latest trading day"`
		} `json:"Global Quote"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return GlobalQuoteOutput{}, err
	}
	if wire.Quote.Symbol == "" && wire.Quote.Price == "" && wire.Quote.LatestTradingDay == "" {
		return GlobalQuoteOutput{}, errNoData
	}
	if wire.Quote.Symbol == "" || wire.Quote.Price == "" || wire.Quote.LatestTradingDay == "" {
		return GlobalQuoteOutput{}, fmt.Errorf("quote fields missing")
	}
	return GlobalQuoteOutput{Symbol: wire.Quote.Symbol, Price: wire.Quote.Price, LatestTradingDay: wire.Quote.LatestTradingDay}, nil
}

func decodeDailySeries(body []byte, symbol string) (DailySeriesOutput, error) {
	var wire struct {
		Series map[string]struct {
			Open   string `json:"1. open"`
			High   string `json:"2. high"`
			Low    string `json:"3. low"`
			Close  string `json:"4. close"`
			Volume string `json:"5. volume"`
		} `json:"Time Series (Daily)"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return DailySeriesOutput{}, err
	}
	if len(wire.Series) == 0 {
		return DailySeriesOutput{}, errNoData
	}
	output := DailySeriesOutput{Symbol: symbol, Days: make([]DailyPrice, 0, len(wire.Series))}
	for date, day := range wire.Series {
		if day.Open == "" || day.High == "" || day.Low == "" || day.Close == "" || day.Volume == "" {
			return DailySeriesOutput{}, fmt.Errorf("daily point incomplete")
		}
		output.Days = append(output.Days, DailyPrice{Date: date, Open: day.Open, High: day.High, Low: day.Low, Close: day.Close, Volume: day.Volume})
	}
	sort.Slice(output.Days, func(i, j int) bool { return output.Days[i].Date > output.Days[j].Date })
	return output, nil
}
