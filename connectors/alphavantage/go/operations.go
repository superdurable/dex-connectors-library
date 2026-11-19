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
	"strings"

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

// SymbolSearchInput supplies company or ticker keywords to search.
type SymbolSearchInput struct {
	// Keywords is a nonblank company name or partial ticker.
	Keywords string `json:"keywords"`
}

// SymbolMatch identifies a symbol returned by Alpha Vantage.
type SymbolMatch struct {
	// Symbol is the provider ticker.
	Symbol string `json:"symbol"`
	// Name is the company or fund name.
	Name string `json:"name"`
	// Region names the market region.
	Region string `json:"region"`
}

// SymbolSearchOutput contains the provider's best matching symbols.
type SymbolSearchOutput struct {
	// Matches are ordered by provider relevance.
	Matches []SymbolMatch `json:"matches"`
}

// CurrencyPairInput identifies a fiat or crypto currency pair.
type CurrencyPairInput struct {
	// FromCurrency is the three-letter source currency code.
	FromCurrency string `json:"fromCurrency"`
	// ToCurrency is the three-letter destination currency code.
	ToCurrency string `json:"toCurrency"`
}

// CurrencyExchangeRateOutput contains one provider exchange rate.
type CurrencyExchangeRateOutput struct {
	// FromCurrency is the source code.
	FromCurrency string `json:"fromCurrency"`
	// ToCurrency is the destination code.
	ToCurrency string `json:"toCurrency"`
	// Rate is the decimal exchange rate string.
	Rate string `json:"rate"`
	// LastRefreshed is the provider timestamp in its stated time zone.
	LastRefreshed string `json:"lastRefreshed"`
}

// MarketStatusInput optionally filters global trading venues by region.
type MarketStatusInput struct {
	// Region is an exact provider region name; blank returns all venues.
	Region string `json:"region,omitempty"`
}

// MarketMoversInput optionally limits each returned ticker list.
type MarketMoversInput struct {
	// Limit is the number of tickers per list, 1 to 20; zero returns all provider entries.
	Limit int `json:"limit,omitempty"`
}

// TradingMarket identifies a venue's current open or closed state.
type TradingMarket struct {
	// MarketType is the venue asset class.
	MarketType string `json:"marketType"`
	// Region is the venue's geographic market.
	Region string `json:"region"`
	// PrimaryExchanges lists the venue exchanges.
	PrimaryExchanges string `json:"primaryExchanges"`
	// CurrentStatus is the provider's open or closed status.
	CurrentStatus string `json:"currentStatus"`
}

// MarketStatusOutput contains current global trading venue states.
type MarketStatusOutput struct {
	// Markets lists the provider venues.
	Markets []TradingMarket `json:"markets"`
}

// MarketMover contains a US ticker's most recent price.
type MarketMover struct {
	// Ticker is the stock ticker.
	Ticker string `json:"ticker"`
	// Price is its decimal price string.
	Price string `json:"price"`
}

// MarketMoversOutput contains the provider's top US ticker lists.
type MarketMoversOutput struct {
	// LastUpdated is the provider's update timestamp.
	LastUpdated string `json:"lastUpdated"`
	// TopGainers lists rising tickers.
	TopGainers []MarketMover `json:"topGainers"`
	// TopLosers lists falling tickers.
	TopLosers []MarketMover `json:"topLosers"`
	// MostActivelyTraded lists high-volume tickers.
	MostActivelyTraded []MarketMover `json:"mostActivelyTraded"`
}

// GetGlobalQuoteOperation retrieves one global quote.
type GetGlobalQuoteOperation struct{ client *Client }

// GetTimeSeriesDailyOperation retrieves compact raw daily prices.
type GetTimeSeriesDailyOperation struct{ client *Client }

// SearchSymbolsOperation finds ticker symbols by keywords.
type SearchSymbolsOperation struct{ client *Client }

// GetCurrencyExchangeRateOperation retrieves one currency pair rate.
type GetCurrencyExchangeRateOperation struct{ client *Client }

// GetMarketStatusOperation retrieves global trading venue states.
type GetMarketStatusOperation struct{ client *Client }

// GetTopGainersLosersOperation retrieves leading US market movers.
type GetTopGainersLosersOperation struct{ client *Client }

// GetGlobalQuote binds the query to this client.
func (client *Client) GetGlobalQuote() GetGlobalQuoteOperation {
	return GetGlobalQuoteOperation{client}
}

// GetTimeSeriesDaily binds the query to this client.
func (client *Client) GetTimeSeriesDaily() GetTimeSeriesDailyOperation {
	return GetTimeSeriesDailyOperation{client}
}

// SearchSymbols binds symbol search to this client.
func (client *Client) SearchSymbols() SearchSymbolsOperation { return SearchSymbolsOperation{client} }

// GetCurrencyExchangeRate binds currency rate lookup to this client.
func (client *Client) GetCurrencyExchangeRate() GetCurrencyExchangeRateOperation {
	return GetCurrencyExchangeRateOperation{client}
}

// GetMarketStatus binds global market status to this client.
func (client *Client) GetMarketStatus() GetMarketStatusOperation {
	return GetMarketStatusOperation{client}
}

// GetTopGainersLosers binds US market mover lookup to this client.
func (client *Client) GetTopGainersLosers() GetTopGainersLosersOperation {
	return GetTopGainersLosersOperation{client}
}

// Definition returns the generated quote contract.
func (GetGlobalQuoteOperation) Definition() sdkgo.QueryDefinition { return GetGlobalQuoteDefinition }

// Definition returns the generated daily-series contract.
func (GetTimeSeriesDailyOperation) Definition() sdkgo.QueryDefinition {
	return GetTimeSeriesDailyDefinition
}

// Definition returns the generated symbol search contract.
func (SearchSymbolsOperation) Definition() sdkgo.QueryDefinition { return SearchSymbolsDefinition }

// Definition returns the generated currency exchange rate contract.
func (GetCurrencyExchangeRateOperation) Definition() sdkgo.QueryDefinition {
	return GetCurrencyExchangeRateDefinition
}

// Definition returns the generated global market status contract.
func (GetMarketStatusOperation) Definition() sdkgo.QueryDefinition { return GetMarketStatusDefinition }

// Definition returns the generated US market movers contract.
func (GetTopGainersLosersOperation) Definition() sdkgo.QueryDefinition {
	return GetTopGainersLosersDefinition
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

// Invoke searches for provider symbols matching a company or ticker phrase.
func (operation SearchSymbolsOperation) Invoke(call sdkgo.Call, input SymbolSearchInput) sdkgo.QueryAttempt[SymbolSearchOutput] {
	branches := queryBranches{found: SearchSymbolsBranchFound, rejected: SearchSymbolsBranchProviderRejected, invalid: SearchSymbolsBranchInvalidResponse, defect: SearchSymbolsBranchDefect}
	if strings.TrimSpace(input.Keywords) == "" || len(input.Keywords) > 100 {
		return queryBranch("searchSymbols", branches.defect, SymbolSearchOutput{}, sdkgo.FailureValidation, "keywords must contain 1 to 100 characters")
	}
	query := url.Values{"function": {"SYMBOL_SEARCH"}, "keywords": {input.Keywords}}
	return invokeJSONQuery(call, operation.client, "searchSymbols", branches, http.MethodGet, "/query", query, nil, decodeSymbolSearch)
}

// Invoke reads the latest exchange rate for a currency pair.
func (operation GetCurrencyExchangeRateOperation) Invoke(call sdkgo.Call, input CurrencyPairInput) sdkgo.QueryAttempt[CurrencyExchangeRateOutput] {
	branches := queryBranches{found: GetCurrencyExchangeRateBranchFound, notFound: GetCurrencyExchangeRateBranchNotFound, rejected: GetCurrencyExchangeRateBranchProviderRejected, invalid: GetCurrencyExchangeRateBranchInvalidResponse, defect: GetCurrencyExchangeRateBranchDefect}
	if !currencyPattern.MatchString(input.FromCurrency) || !currencyPattern.MatchString(input.ToCurrency) {
		return queryBranch("getCurrencyExchangeRate", branches.defect, CurrencyExchangeRateOutput{}, sdkgo.FailureValidation, "currency codes must be three uppercase letters")
	}
	query := url.Values{"function": {"CURRENCY_EXCHANGE_RATE"}, "from_currency": {input.FromCurrency}, "to_currency": {input.ToCurrency}}
	return invokeJSONQuery(call, operation.client, "getCurrencyExchangeRate", branches, http.MethodGet, "/query", query, nil, decodeCurrencyExchangeRate)
}

// Invoke reads the current status of major global trading venues.
func (operation GetMarketStatusOperation) Invoke(call sdkgo.Call, input MarketStatusInput) sdkgo.QueryAttempt[MarketStatusOutput] {
	branches := queryBranches{found: GetMarketStatusBranchFound, rejected: GetMarketStatusBranchProviderRejected, invalid: GetMarketStatusBranchInvalidResponse, defect: GetMarketStatusBranchDefect}
	if len(input.Region) > 100 || strings.ContainsAny(input.Region, "\r\n") {
		return queryBranch("getMarketStatus", branches.defect, MarketStatusOutput{}, sdkgo.FailureValidation, "region is invalid")
	}
	query := url.Values{"function": {"MARKET_STATUS"}}
	return invokeJSONQuery(call, operation.client, "getMarketStatus", branches, http.MethodGet, "/query", query, nil, func(body []byte) (MarketStatusOutput, error) { return decodeMarketStatus(body, input.Region) })
}

// Invoke reads the current US top gainers, losers, and most active tickers.
func (operation GetTopGainersLosersOperation) Invoke(call sdkgo.Call, input MarketMoversInput) sdkgo.QueryAttempt[MarketMoversOutput] {
	branches := queryBranches{found: GetTopGainersLosersBranchFound, rejected: GetTopGainersLosersBranchProviderRejected, invalid: GetTopGainersLosersBranchInvalidResponse, defect: GetTopGainersLosersBranchDefect}
	if input.Limit < 0 || input.Limit > 20 {
		return queryBranch("getTopGainersLosers", branches.defect, MarketMoversOutput{}, sdkgo.FailureValidation, "limit must be 1 to 20 or zero")
	}
	query := url.Values{"function": {"TOP_GAINERS_LOSERS"}}
	return invokeJSONQuery(call, operation.client, "getTopGainersLosers", branches, http.MethodGet, "/query", query, nil, func(body []byte) (MarketMoversOutput, error) { return decodeMarketMovers(body, input.Limit) })
}

var symbolPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,24}$`)
var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
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

func decodeSymbolSearch(body []byte) (SymbolSearchOutput, error) {
	var wire struct {
		Matches []struct {
			Symbol string `json:"1. symbol"`
			Name   string `json:"2. name"`
			Region string `json:"4. region"`
		} `json:"bestMatches"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return SymbolSearchOutput{}, err
	}
	if wire.Matches == nil {
		return SymbolSearchOutput{}, fmt.Errorf("symbol matches missing")
	}
	output := SymbolSearchOutput{Matches: make([]SymbolMatch, 0, len(wire.Matches))}
	for _, match := range wire.Matches {
		if match.Symbol == "" || match.Name == "" || match.Region == "" {
			return SymbolSearchOutput{}, fmt.Errorf("symbol match incomplete")
		}
		output.Matches = append(output.Matches, SymbolMatch{Symbol: match.Symbol, Name: match.Name, Region: match.Region})
	}
	return output, nil
}

func decodeCurrencyExchangeRate(body []byte) (CurrencyExchangeRateOutput, error) {
	var wire struct {
		Rate struct {
			FromCurrency  string `json:"1. From_Currency Code"`
			ToCurrency    string `json:"3. To_Currency Code"`
			ExchangeRate  string `json:"5. Exchange Rate"`
			LastRefreshed string `json:"6. Last Refreshed"`
		} `json:"Realtime Currency Exchange Rate"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return CurrencyExchangeRateOutput{}, err
	}
	if wire.Rate.FromCurrency == "" && wire.Rate.ToCurrency == "" && wire.Rate.ExchangeRate == "" {
		return CurrencyExchangeRateOutput{}, errNoData
	}
	if wire.Rate.FromCurrency == "" || wire.Rate.ToCurrency == "" || wire.Rate.ExchangeRate == "" || wire.Rate.LastRefreshed == "" {
		return CurrencyExchangeRateOutput{}, fmt.Errorf("exchange rate incomplete")
	}
	return CurrencyExchangeRateOutput{FromCurrency: wire.Rate.FromCurrency, ToCurrency: wire.Rate.ToCurrency, Rate: wire.Rate.ExchangeRate, LastRefreshed: wire.Rate.LastRefreshed}, nil
}

func decodeMarketStatus(body []byte, region string) (MarketStatusOutput, error) {
	var wire struct {
		Markets []struct {
			MarketType       string `json:"market_type"`
			Region           string `json:"region"`
			PrimaryExchanges string `json:"primary_exchanges"`
			CurrentStatus    string `json:"current_status"`
		} `json:"markets"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return MarketStatusOutput{}, err
	}
	if wire.Markets == nil {
		return MarketStatusOutput{}, fmt.Errorf("markets missing")
	}
	output := MarketStatusOutput{Markets: make([]TradingMarket, 0, len(wire.Markets))}
	for _, market := range wire.Markets {
		if market.MarketType == "" || market.Region == "" || market.PrimaryExchanges == "" || market.CurrentStatus == "" {
			return MarketStatusOutput{}, fmt.Errorf("market status incomplete")
		}
		if region == "" || market.Region == region {
			output.Markets = append(output.Markets, TradingMarket{MarketType: market.MarketType, Region: market.Region, PrimaryExchanges: market.PrimaryExchanges, CurrentStatus: market.CurrentStatus})
		}
	}
	return output, nil
}

func decodeMarketMovers(body []byte, limit int) (MarketMoversOutput, error) {
	var wire struct {
		LastUpdated        string        `json:"last_updated"`
		TopGainers         []MarketMover `json:"top_gainers"`
		TopLosers          []MarketMover `json:"top_losers"`
		MostActivelyTraded []MarketMover `json:"most_actively_traded"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return MarketMoversOutput{}, err
	}
	if wire.LastUpdated == "" || wire.TopGainers == nil || wire.TopLosers == nil || wire.MostActivelyTraded == nil {
		return MarketMoversOutput{}, fmt.Errorf("market mover lists missing")
	}
	output := MarketMoversOutput{LastUpdated: wire.LastUpdated, TopGainers: make([]MarketMover, 0, len(wire.TopGainers)), TopLosers: make([]MarketMover, 0, len(wire.TopLosers)), MostActivelyTraded: make([]MarketMover, 0, len(wire.MostActivelyTraded))}
	for _, list := range [][]MarketMover{wire.TopGainers, wire.TopLosers, wire.MostActivelyTraded} {
		for _, mover := range list {
			if mover.Ticker == "" || mover.Price == "" {
				return MarketMoversOutput{}, fmt.Errorf("market mover incomplete")
			}
		}
	}
	output.TopGainers = append(output.TopGainers, wire.TopGainers...)
	output.TopLosers = append(output.TopLosers, wire.TopLosers...)
	output.MostActivelyTraded = append(output.MostActivelyTraded, wire.MostActivelyTraded...)
	if limit > 0 {
		if len(output.TopGainers) > limit {
			output.TopGainers = output.TopGainers[:limit]
		}
		if len(output.TopLosers) > limit {
			output.TopLosers = output.TopLosers[:limit]
		}
		if len(output.MostActivelyTraded) > limit {
			output.MostActivelyTraded = output.MostActivelyTraded[:limit]
		}
	}
	return output, nil
}
