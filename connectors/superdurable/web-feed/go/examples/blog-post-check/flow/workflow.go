// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package blogpostcheck checks one blog for recent posts as a Dex Flow. It reads the feed the blog
// advertises, or, for a blog without a feed, its sitemap and the head metadata of each recent post page,
// and optionally reads the readable text of the first reported post.
package blogpostcheck

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/superdurable/web-feed/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// ConnectionName is the static Dex Web connection of the web-feed connector.
	ConnectionName = "web-feed-public"
	// SourceFeed reports posts read from a feed.
	SourceFeed = "feed"
	// SourceSitemap reports posts read from a sitemap and the post pages.
	SourceSitemap = "sitemap"

	discoverFeedsStepType = "DiscoverBlogFeeds"
	readFeedStepType      = "ReadBlogFeed"
	readSitemapStepType   = "ReadBlogSitemap"
	readPostPageStepType  = "ReadPostPage"
	readPostTextStepType  = "ReadFirstPostText"
	// firstPostTextCharacters bounds the reported post text, which the report keeps as an excerpt.
	firstPostTextCharacters = 2000
	defaultMaxPosts         = 5
	maxPostsAllowed         = 10
	// maxSitemapReads bounds the sitemap and child sitemap reads of one check.
	maxSitemapReads = 4
)

var (
	checkStateAttribute = dex.DefineAttribute[checkState]("blog-post-check-state")
	reportAttribute     = dex.DefineAttribute[CheckReport]("blog-post-check-report")
)

// CheckRequest selects the blog to check. Give siteUrl, feedUrl, or sitemapUrl.
type CheckRequest struct {
	// SiteURL is the blog's home or index page, whose advertised feeds are discovered first.
	SiteURL string `json:"siteUrl,omitempty"`
	// FeedURL skips discovery and reads this feed.
	FeedURL string `json:"feedUrl,omitempty"`
	// SitemapURL is read when no feed is found. Empty uses /sitemap.xml at the page's origin.
	SitemapURL string `json:"sitemapUrl,omitempty"`
	// PathPrefix keeps only sitemap entries under this path, such as /blog/.
	PathPrefix string `json:"pathPrefix,omitempty"`
	// PublishedAfter is an RFC 3339 time; only newer posts are reported. Empty reports the newest posts.
	PublishedAfter string `json:"publishedAfter,omitempty"`
	// MaxPosts bounds the reported posts from 1 through 10. Zero selects 5.
	MaxPosts int `json:"maxPosts,omitempty"`
	// ETag is the feed etag a previous check reported, sent as a conditional GET validator.
	ETag string `json:"etag,omitempty"`
	// LastModified is the feed lastModified a previous check reported, sent as a conditional GET validator.
	LastModified string `json:"lastModified,omitempty"`
	// IncludeFirstPostText reads the readable text of the first reported post into the report.
	IncludeFirstPostText bool `json:"includeFirstPostText,omitempty"`
}

// CheckReport is the Flow output and the Dex Web display value.
type CheckReport struct {
	// Source is feed or sitemap.
	Source string `json:"source"`
	// SourceURL is the feed or first sitemap that was read.
	SourceURL string `json:"sourceUrl"`
	// IsUnchanged reports that the feed answered 304 to the conditional GET.
	IsUnchanged bool `json:"isUnchanged"`
	// ETag is the feed etag to pass to the next check.
	ETag string `json:"etag,omitempty"`
	// LastModified is the feed lastModified to pass to the next check.
	LastModified string `json:"lastModified,omitempty"`
	// Posts holds the newest matching posts.
	Posts []Post `json:"posts"`
	// FirstPostText is the readable text of the first post, at most 2000 code points, when the request
	// set includeFirstPostText. It is untrusted provider text.
	FirstPostText string `json:"firstPostText,omitempty"`
}

// Post is one reported blog post. Title and Summary are untrusted provider text.
type Post struct {
	// URL is the post page.
	URL string `json:"url"`
	// Title is the feed item title, or the page's Open Graph title, JSON-LD headline, or title.
	Title string `json:"title,omitempty"`
	// Summary is the feed item summary, or the page's meta or Open Graph description.
	Summary string `json:"summary,omitempty"`
	// PublishedAt is the post's publication time in UTC, or its sitemap date when the page states none.
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
}

// checkState carries the request and the sitemap work queue between Connector Steps.
type checkState struct {
	Request          CheckRequest           `json:"request"`
	PublishedAfter   *time.Time             `json:"publishedAfter,omitempty"`
	SitemapReadCount int                    `json:"sitemapReadCount"`
	PendingSitemaps  []string               `json:"pendingSitemaps"`
	Candidates       []webfeed.SitemapEntry `json:"candidates"`
	PendingPages     []webfeed.SitemapEntry `json:"pendingPages"`
	Report           CheckReport            `json:"report"`
}

// BlogPostCheck checks one blog and completes with its newest posts.
type BlogPostCheck struct {
	dex.FlowDefaults
	connection webfeed.Connection
}

// NewBlogPostCheck binds the web-feed connection at registration time.
func NewBlogPostCheck(connection webfeed.Connection) *BlogPostCheck {
	return &BlogPostCheck{connection: connection}
}

// GetSteps registers the application Steps and one Connector Step per web-feed operation.
func (flow *BlogPostCheck) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(StartBlogPostCheck{}),
		dex.DefineStep(webfeed.NewDiscoverFeedsStep(webfeed.DiscoverFeedsStepConfig[webfeed.DiscoverFeedsInput]{
			StepType: discoverFeedsStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "source", GroupLabel: "Source", Explanation: "List the feeds the blog page advertises."},
			MapToOperationInput: mapDiscoverFeedsInput,
			Discovered:          sdkgo.GoTo(ChooseBlogSource{}),
		})),
		dex.DefineStep(ChooseBlogSource{}),
		dex.DefineStep(webfeed.NewReadFeedStep(webfeed.ReadFeedStepConfig[webfeed.ReadFeedInput]{
			StepType: readFeedStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "feed", GroupLabel: "Feed", Explanation: "Read the blog feed with a conditional GET."},
			MapToOperationInput: mapReadFeedInput,
			Read:                sdkgo.GoTo(CollectFeedPosts{}),
			// An unchanged feed completes the check without posts and keeps the validators for the next poll.
			NotModified: sdkgo.GoTo(CompleteUnchangedFeed{}),
		})),
		dex.DefineStep(CollectFeedPosts{}),
		dex.DefineStep(CompleteUnchangedFeed{}),
		dex.DefineStep(webfeed.NewReadSitemapStep(webfeed.ReadSitemapStepConfig[webfeed.ReadSitemapInput]{
			StepType: readSitemapStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "sitemap", GroupLabel: "Sitemap", Explanation: "Read one sitemap filtered by path and date."},
			MapToOperationInput: mapReadSitemapInput,
			Read:                sdkgo.GoTo(CollectSitemapEntries{}),
		})),
		dex.DefineStep(CollectSitemapEntries{}),
		dex.DefineStep(webfeed.NewReadPageMetadataStep(webfeed.ReadPageMetadataStepConfig[webfeed.ReadPageMetadataInput]{
			StepType: readPostPageStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "sitemap", GroupLabel: "Sitemap", Explanation: "Read the title and dates of one recent post page."},
			MapToOperationInput: mapReadPageMetadataInput,
			Read:                sdkgo.GoTo(CollectPostPage{}),
			// A post removed after the sitemap listed it is skipped so the check reports the others.
			NotFound: sdkgo.GoTo(CollectPostPage{}),
		})),
		dex.DefineStep(CollectPostPage{}),
		dex.DefineStep(webfeed.NewReadPageStep(webfeed.ReadPageStepConfig[webfeed.ReadPageInput]{
			StepType: readPostTextStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "text", GroupLabel: "Post text", Explanation: "Read the readable text of the first reported post."},
			MapToOperationInput: mapReadPageInput,
			Read:                sdkgo.GoTo(CollectFirstPostText{}),
		})),
		dex.DefineStep(CollectFirstPostText{}),
	}
}

// GetPersistenceSchema registers the check state and the report.
func (*BlogPostCheck) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{checkStateAttribute, reportAttribute}}
}

// GetRPCs exposes the read-only Dex Web summary and display views.
func (flow *BlogPostCheck) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary shows the report once the check completes.
// dex:field attribute-key:blog-post-check-report value-type:json editable:false description:"Recent posts and the feed validators for the next check"
func (*BlogPostCheck) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	report, err := readReport(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"blog-post-check-report": report}}, nil
}

// GetDexDisplay shows the report once the check completes.
// dex:field attribute-key:blog-post-check-report value-type:json editable:false description:"Recent posts and the feed validators for the next check"
func (*BlogPostCheck) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	report, err := readReport(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"blog-post-check-report": report}}, nil
}

func readReport(ctx dex.Context) (CheckReport, error) {
	report, err := reportAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return CheckReport{Posts: []Post{}}, nil
	}
	return report, err
}

// StartBlogPostCheck validates the request and picks the first read.
// dex:group group-id:source group-label:"Source"
// dex:explanation text:"Validate the request and read the feed, the page, or the sitemap it names."
type StartBlogPostCheck struct{ dex.StepDefaults }

// WaitFor starts the check immediately.
func (StartBlogPostCheck) WaitFor(dex.Context, CheckRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

// Execute stores the validated request and chooses the feed, discovery, or the sitemap.
func (StartBlogPostCheck) Execute(ctx dex.Context, request CheckRequest) (*dex.StepDecision, error) {
	state, err := newCheckState(request)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	isSitemapOnly := request.FeedURL == "" && request.SiteURL == ""
	if isSitemapOnly {
		state.SitemapReadCount = 1
	}
	if err := checkStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	switch {
	case request.FeedURL != "":
		return dex.GoTo(sdkgo.StepRef[webfeed.ReadFeedInput](readFeedStepType), state.feedInput(request.FeedURL)), nil
	case request.SiteURL != "":
		return dex.GoTo(sdkgo.StepRef[webfeed.DiscoverFeedsInput](discoverFeedsStepType), webfeed.DiscoverFeedsInput{URL: request.SiteURL}), nil
	}
	return dex.GoTo(sdkgo.StepRef[webfeed.ReadSitemapInput](readSitemapStepType), state.sitemapInput(request.SitemapURL)), nil
}

// ChooseBlogSource reads the first advertised feed, or the sitemap when the page advertises none.
// dex:group group-id:source group-label:"Source"
// dex:explanation text:"Choose the first advertised feed, or the sitemap when the page has no feed."
type ChooseBlogSource struct {
	dex.StepDefaultsNoWaitFor[webfeed.DiscoverFeedsResult]
}

// Execute picks the next read from the discovered feeds.
func (ChooseBlogSource) Execute(ctx dex.Context, result webfeed.DiscoverFeedsResult) (*dex.StepDecision, error) {
	state, err := checkStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if len(result.Value.Feeds) > 0 {
		return dex.GoTo(sdkgo.StepRef[webfeed.ReadFeedInput](readFeedStepType), state.feedInput(result.Value.Feeds[0].URL)), nil
	}
	sitemapURL := state.Request.SitemapURL
	if sitemapURL == "" {
		sitemapURL = defaultSitemapURL(result.Value.FinalURL)
	}
	state.SitemapReadCount = 1
	if err := checkStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[webfeed.ReadSitemapInput](readSitemapStepType), state.sitemapInput(sitemapURL)), nil
}

// CollectFeedPosts reports the newest feed items after publishedAfter.
// dex:group group-id:feed group-label:"Feed"
// dex:explanation text:"Report the newest feed items and the validators for the next check."
type CollectFeedPosts struct {
	dex.StepDefaultsNoWaitFor[webfeed.ReadFeedResult]
}

// Execute records the report and completes the Flow with it.
func (CollectFeedPosts) Execute(ctx dex.Context, result webfeed.ReadFeedResult) (*dex.StepDecision, error) {
	state, err := checkStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	state.Report = CheckReport{
		Source: SourceFeed, SourceURL: result.Value.FinalURL, ETag: result.Value.ETag, LastModified: result.Value.LastModified,
		Posts: selectFeedPosts(result.Value.Items, state.PublishedAfter, state.Request.MaxPosts),
	}
	if state.shouldReadFirstPostText() {
		if err := checkStateAttribute.Set(ctx, state); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[webfeed.ReadPageInput](readPostTextStepType), state.firstPostTextInput()), nil
	}
	if err := reportAttribute.Set(ctx, state.Report); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(state.Report), nil
}

// CompleteUnchangedFeed reports an unchanged feed.
// dex:group group-id:feed group-label:"Feed"
// dex:explanation text:"Report that the feed is unchanged and keep its validators."
type CompleteUnchangedFeed struct {
	dex.StepDefaultsNoWaitFor[webfeed.ReadFeedResult]
}

// Execute records the unchanged report and completes the Flow with it.
func (CompleteUnchangedFeed) Execute(ctx dex.Context, result webfeed.ReadFeedResult) (*dex.StepDecision, error) {
	report := CheckReport{
		Source: SourceFeed, SourceURL: result.Value.FinalURL, IsUnchanged: true,
		ETag: result.Value.ETag, LastModified: result.Value.LastModified, Posts: []Post{},
	}
	if err := reportAttribute.Set(ctx, report); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(report), nil
}

// CollectSitemapEntries queues child sitemaps of an index and the newest matching post pages.
// dex:group group-id:sitemap group-label:"Sitemap"
// dex:explanation text:"Queue child sitemaps, then the newest matching post pages."
type CollectSitemapEntries struct {
	dex.StepDefaultsNoWaitFor[webfeed.ReadSitemapResult]
}

// Execute reads the next queued sitemap, the first post page, or completes without posts.
func (CollectSitemapEntries) Execute(ctx dex.Context, result webfeed.ReadSitemapResult) (*dex.StepDecision, error) {
	state, err := checkStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if state.Report.SourceURL == "" {
		state.Report = CheckReport{Source: SourceSitemap, SourceURL: result.Value.FinalURL, Posts: []Post{}}
	}
	state.recordSitemap(result.Value)
	if len(state.PendingSitemaps) > 0 {
		next := state.PendingSitemaps[0]
		state.PendingSitemaps = state.PendingSitemaps[1:]
		state.SitemapReadCount++
		if err := checkStateAttribute.Set(ctx, state); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[webfeed.ReadSitemapInput](readSitemapStepType), state.sitemapInput(next)), nil
	}
	state.PendingPages = newestEntries(state.Candidates, state.Request.MaxPosts)
	if err := checkStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	if len(state.PendingPages) == 0 {
		if state.shouldReadFirstPostText() {
			if err := checkStateAttribute.Set(ctx, state); err != nil {
				return nil, err
			}
			return dex.GoTo(sdkgo.StepRef[webfeed.ReadPageInput](readPostTextStepType), state.firstPostTextInput()), nil
		}
		if err := reportAttribute.Set(ctx, state.Report); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(state.Report), nil
	}
	return dex.GoTo(sdkgo.StepRef[webfeed.ReadPageMetadataInput](readPostPageStepType), webfeed.ReadPageMetadataInput{URL: state.PendingPages[0].URL}), nil
}

// CollectPostPage adds one post page to the report.
// dex:group group-id:sitemap group-label:"Sitemap"
// dex:explanation text:"Add the post page's title and dates, then read the next page or complete."
type CollectPostPage struct {
	dex.StepDefaultsNoWaitFor[webfeed.ReadPageMetadataResult]
}

// Execute records the post, skipping a removed page, and continues with the next queued page.
func (CollectPostPage) Execute(ctx dex.Context, result webfeed.ReadPageMetadataResult) (*dex.StepDecision, error) {
	state, err := checkStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if len(state.PendingPages) == 0 {
		return nil, errors.New("blog post check state has no pending post page")
	}
	entry := state.PendingPages[0]
	state.PendingPages = state.PendingPages[1:]
	if result.Branch == webfeed.ReadPageMetadataBranchRead {
		state.Report.Posts = append(state.Report.Posts, postFromPage(entry, result.Value))
	}
	if err := checkStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	if len(state.PendingPages) > 0 {
		return dex.GoTo(sdkgo.StepRef[webfeed.ReadPageMetadataInput](readPostPageStepType), webfeed.ReadPageMetadataInput{URL: state.PendingPages[0].URL}), nil
	}
	if state.shouldReadFirstPostText() {
		if err := checkStateAttribute.Set(ctx, state); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[webfeed.ReadPageInput](readPostTextStepType), state.firstPostTextInput()), nil
	}
	if err := reportAttribute.Set(ctx, state.Report); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(state.Report), nil
}

// CollectFirstPostText adds the first post's readable text to the report.
// dex:group group-id:text group-label:"Post text"
// dex:explanation text:"Add the first post's readable text to the report and complete."
type CollectFirstPostText struct {
	dex.StepDefaultsNoWaitFor[webfeed.ReadPageResult]
}

// Execute records the text and completes the Flow with the report.
func (CollectFirstPostText) Execute(ctx dex.Context, result webfeed.ReadPageResult) (*dex.StepDecision, error) {
	state, err := checkStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	state.Report.FirstPostText = result.Value.Text
	if err := reportAttribute.Set(ctx, state.Report); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(state.Report), nil
}

func newCheckState(request CheckRequest) (checkState, error) {
	if request.SiteURL == "" && request.FeedURL == "" && request.SitemapURL == "" {
		return checkState{}, errors.New("siteUrl, feedUrl, or sitemapUrl is required")
	}
	if request.MaxPosts == 0 {
		request.MaxPosts = defaultMaxPosts
	}
	if request.MaxPosts < 1 || request.MaxPosts > maxPostsAllowed {
		return checkState{}, fmt.Errorf("maxPosts must be from 1 through %d", maxPostsAllowed)
	}
	state := checkState{Request: request, PendingSitemaps: []string{}, Candidates: []webfeed.SitemapEntry{}, PendingPages: []webfeed.SitemapEntry{}}
	if request.PublishedAfter != "" {
		publishedAfter, err := time.Parse(time.RFC3339, request.PublishedAfter)
		if err != nil {
			return checkState{}, errors.New("publishedAfter must be an RFC 3339 time, such as 2026-11-01T00:00:00Z")
		}
		publishedAfter = publishedAfter.UTC()
		state.PublishedAfter = &publishedAfter
	}
	return state, nil
}

// shouldReadFirstPostText reports that the request asked for the first post's text and a post has a URL.
func (state checkState) shouldReadFirstPostText() bool {
	return state.Request.IncludeFirstPostText && len(state.Report.Posts) > 0 && state.Report.Posts[0].URL != ""
}

func (state checkState) firstPostTextInput() webfeed.ReadPageInput {
	return webfeed.ReadPageInput{URL: state.Report.Posts[0].URL, MaxTextCharacters: firstPostTextCharacters}
}

func (state checkState) feedInput(feedURL string) webfeed.ReadFeedInput {
	return webfeed.ReadFeedInput{URL: feedURL, ETag: state.Request.ETag, LastModified: state.Request.LastModified, Limit: 50, MaxSummaryCharacters: 300}
}

func (state checkState) sitemapInput(sitemapURL string) webfeed.ReadSitemapInput {
	return webfeed.ReadSitemapInput{URL: sitemapURL, PathPrefix: state.Request.PathPrefix, LastModifiedAfter: state.PublishedAfter, Limit: state.Request.MaxPosts}
}

// recordSitemap queues an index's newest children within the read budget and keeps a urlset's entries.
func (state *checkState) recordSitemap(sitemap webfeed.Sitemap) {
	if sitemap.Kind == webfeed.SitemapKindIndex {
		for _, child := range sitemap.Sitemaps {
			if state.SitemapReadCount+len(state.PendingSitemaps) >= maxSitemapReads {
				break
			}
			state.PendingSitemaps = append(state.PendingSitemaps, child.URL)
		}
		return
	}
	state.Candidates = append(state.Candidates, sitemap.Entries...)
}

// selectFeedPosts keeps up to maxPosts items, in feed order, dated after publishedAfter when it is set.
func selectFeedPosts(items []webfeed.FeedItem, publishedAfter *time.Time, maxPosts int) []Post {
	posts := []Post{}
	for _, item := range items {
		published := item.PublishedAt
		if published == nil {
			published = item.UpdatedAt
		}
		if publishedAfter != nil && (published == nil || !published.After(*publishedAfter)) {
			continue
		}
		posts = append(posts, Post{URL: item.Link, Title: item.Title, Summary: item.Summary, PublishedAt: published})
		if len(posts) == maxPosts {
			break
		}
	}
	return posts
}

// newestEntries merges the entries of every read sitemap, newest first, without repeated URLs.
func newestEntries(candidates []webfeed.SitemapEntry, maxPosts int) []webfeed.SitemapEntry {
	sorted := slices.Clone(candidates)
	slices.SortStableFunc(sorted, func(left, right webfeed.SitemapEntry) int {
		return compareNewestFirst(entryTime(left), entryTime(right))
	})
	selected := []webfeed.SitemapEntry{}
	seen := map[string]bool{}
	for _, entry := range sorted {
		if seen[entry.URL] {
			continue
		}
		seen[entry.URL] = true
		selected = append(selected, entry)
		if len(selected) == maxPosts {
			break
		}
	}
	return selected
}

func entryTime(entry webfeed.SitemapEntry) *time.Time {
	if entry.LastModifiedAt != nil {
		return entry.LastModifiedAt
	}
	return entry.NewsPublishedAt
}

func compareNewestFirst(left, right *time.Time) int {
	switch {
	case left == nil && right == nil:
		return 0
	case left == nil:
		return 1
	case right == nil:
		return -1
	}
	return right.Compare(*left)
}

// postFromPage prefers the page's own title and publication time over the sitemap's.
func postFromPage(entry webfeed.SitemapEntry, metadata webfeed.PageMetadata) Post {
	post := Post{
		URL:         entry.URL,
		Title:       firstNonEmpty(metadata.OpenGraph.Title, metadata.StructuredData.Headline, metadata.Title, entry.NewsTitle),
		Summary:     firstNonEmpty(metadata.Description, metadata.OpenGraph.Description),
		PublishedAt: metadata.PublishedAt,
	}
	if post.PublishedAt == nil {
		post.PublishedAt = entryTime(entry)
	}
	return post
}

// defaultSitemapURL is /sitemap.xml at the origin of the page that was read.
func defaultSitemapURL(pageURL string) string {
	parsed, err := url.Parse(pageURL)
	if err != nil || parsed.Host == "" {
		return pageURL
	}
	return (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: "/sitemap.xml"}).String()
}

func mapDiscoverFeedsInput(input webfeed.DiscoverFeedsInput) webfeed.DiscoverFeedsInput { return input }

func mapReadFeedInput(input webfeed.ReadFeedInput) webfeed.ReadFeedInput { return input }

func mapReadSitemapInput(input webfeed.ReadSitemapInput) webfeed.ReadSitemapInput { return input }

func mapReadPageMetadataInput(input webfeed.ReadPageMetadataInput) webfeed.ReadPageMetadataInput {
	return input
}

func mapReadPageInput(input webfeed.ReadPageInput) webfeed.ReadPageInput { return input }

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

var _ dex.Flow = (*BlogPostCheck)(nil)
