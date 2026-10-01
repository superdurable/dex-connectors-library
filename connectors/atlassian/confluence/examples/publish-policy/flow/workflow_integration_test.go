//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package publishpolicy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationCloudID     = "11223344-a1b2-4b33-8c44-def123456789"
	integrationAccessToken = "integration-access-token"
	integrationSpaceID     = "98306"
	integrationHomepageID  = "65537"
	shortRequestTimeout    = 500 * time.Millisecond
	// slowRequestTimeout outlasts the fake's nine-second responses and Dex's roughly seven-second async local phase.
	slowRequestTimeout = 20 * time.Second
	slowProviderDelay  = 9 * time.Second

	markerSlowCreate     = "[slow-create]"
	markerCreateTimeout  = "[create-timeout]"
	markerRateLimit      = "[rate-limit]"
	markerReject         = "[reject]"
	markerSlowUpdate     = "[slow-update]"
	markerConcurrentEdit = "[concurrent-edit]"
	markerSlowComment    = "[slow-comment]"
	markerCommentTimeout = "[comment-timeout]"

	existingPolicyTitle = "Travel policy"
	integrationBody     = "# Remote work\n\nStaff may work **remotely** two days a week.\n\n- Ask your manager\n- Log the days"
)

var (
	sitePrefix        = "/ex/confluence/" + integrationCloudID + "/wiki"
	pagePathPattern   = regexp.MustCompile(`^` + regexp.QuoteMeta(sitePrefix) + `/api/v2/pages/([0-9]+)(/footer-comments|/versions/[0-9]+)?$`)
	versionNumberPath = regexp.MustCompile(`/versions/([0-9]+)$`)
)

func TestNewPolicyIsPublishedReadBackAndCommentedOnceWithRealDex(t *testing.T) {
	provider, flow, harness := newConfluenceIntegrationHarness(t, integrationCloudID, shortRequestTimeout)
	ctx := integrationContext(t)

	publication := runPublicationFlow(t, ctx, harness, flow, "new", publicationInput("Remote work policy"))
	require.Equal(t, PhasePublished, publication.Phase)
	require.False(t, publication.IsExistingPageUpdated)
	require.Equal(t, 1, publication.VersionNumber)
	require.Equal(t, "# Remote work\n\nStaff may work **remotely** two days a week.\n\n- Ask your manager\n- Log the days", publication.PublishedText)
	require.NotEmpty(t, publication.CommentID)
	require.Equal(t, []RelatedPolicy{{PageID: "600001", Title: "Remote work policy (2019 draft)", VersionNumber: 2}}, publication.RelatedPolicies)
	require.Equal(t, 1, provider.createCount("Remote work policy"))
	require.Equal(t, 1, provider.commentCount(publication.PageID))
	require.Equal(t, []string{`type = page AND space in ("OPS") AND title ~ "\"Remote work policy\"" ORDER BY lastmodified DESC`}, provider.searchQueries())
	require.Equal(t, "https://ops.atlassian.net/wiki/spaces/OPS/pages/"+publication.PageID, publication.WebURL)
}

func TestExistingPolicyIsUpdatedToItsNextVersionWithRealDex(t *testing.T) {
	provider, flow, harness := newConfluenceIntegrationHarness(t, integrationCloudID, shortRequestTimeout)
	ctx := integrationContext(t)

	input := publicationInput(existingPolicyTitle)
	input.ChangeSummary = "Annual review"
	publication := runPublicationFlow(t, ctx, harness, flow, "existing", input)
	require.Equal(t, PhasePublished, publication.Phase)
	require.True(t, publication.IsExistingPageUpdated)
	require.Equal(t, 4, publication.VersionNumber)
	require.Equal(t, 1, provider.createCount(existingPolicyTitle), "the create found the title taken and created nothing")
	require.Equal(t, 1, provider.updateCount(existingPolicyTitle))
	require.Regexp(t, `^Annual review \[dex:[0-9a-f]{16}\]$`, provider.versionMessage(existingPolicyTitle, 4))
	require.Equal(t, 1, provider.pageCount(existingPolicyTitle))
}

// TestSlowCreateIsSentOnceUnderSyncDurabilityWithRealDex guards sync durability: an async backup attempt
// would send a create that outlasts Dex's roughly seven-second local phase while the first is in flight.
func TestSlowCreateIsSentOnceUnderSyncDurabilityWithRealDex(t *testing.T) {
	provider, flow, harness := newConfluenceIntegrationHarness(t, integrationCloudID, slowRequestTimeout)
	ctx := integrationContext(t)

	title := markerSlowCreate + " Generator testing policy"
	publication := runPublicationFlow(t, ctx, harness, flow, "slow-create", publicationInput(title))
	require.Equal(t, PhasePublished, publication.Phase)
	require.False(t, publication.IsExistingPageUpdated, "the Step reports its own page as created, not as a title conflict")
	require.Equal(t, 1, provider.createCount(title), "one Step execution sends one create")
	require.Equal(t, 1, provider.pageCount(title))
}

// TestSlowUpdateBackupAttemptConvergesOnItsVersionWithRealDex proves updatePage is safe under async
// durability: a backup attempt's repeated update is refused and finds the version the first attempt saved.
func TestSlowUpdateBackupAttemptConvergesOnItsVersionWithRealDex(t *testing.T) {
	provider, flow, harness := newConfluenceIntegrationHarness(t, integrationCloudID, slowRequestTimeout)
	ctx := integrationContext(t)
	title := markerSlowUpdate + " Expense policy"
	provider.seedPage(title, 2)

	publication := runPublicationFlow(t, ctx, harness, flow, "slow-update", publicationInput(title))
	require.Equal(t, PhasePublished, publication.Phase)
	require.Equal(t, 3, publication.VersionNumber)
	require.Equal(t, 3, provider.currentVersion(title), "the page changed once")
	t.Logf("slow update: update requests=%d, confirmed by read-back=%t", provider.updateCount(title), publication.IsConfirmedByReadBack)
}

func TestCreateTimeoutIsConfirmedByTitleWithoutASecondPageWithRealDex(t *testing.T) {
	provider, flow, harness := newConfluenceIntegrationHarness(t, integrationCloudID, shortRequestTimeout)
	ctx := integrationContext(t)

	title := markerCreateTimeout + " Lift inspection policy"
	publication := runPublicationFlow(t, ctx, harness, flow, "create-timeout", publicationInput(title))
	require.Equal(t, PhasePublished, publication.Phase)
	require.True(t, publication.IsConfirmedByReadBack)
	require.False(t, publication.IsExistingPageUpdated)
	require.Equal(t, 1, provider.createCount(title))
	require.Equal(t, 1, provider.pageCount(title))
}

func TestRateLimitedCreateIsRetriedAfterRetryAfterWithRealDex(t *testing.T) {
	provider, flow, harness := newConfluenceIntegrationHarness(t, integrationCloudID, shortRequestTimeout)
	ctx := integrationContext(t)

	title := markerRateLimit + " Door access policy"
	publication := runPublicationFlow(t, ctx, harness, flow, "rate-limited", publicationInput(title))
	require.Equal(t, PhasePublished, publication.Phase)
	attempts := provider.createTimesFor(title)
	require.Len(t, attempts, 2)
	require.GreaterOrEqual(t, attempts[1].Sub(attempts[0]), time.Second, "the retry waits for Retry-After")
	require.Equal(t, 1, provider.pageCount(title))
}

func TestRejectedCreateCompletesAsRejectedWithRealDex(t *testing.T) {
	provider, flow, harness := newConfluenceIntegrationHarness(t, integrationCloudID, shortRequestTimeout)
	ctx := integrationContext(t)

	title := markerReject + " Badge policy"
	publication := runPublicationFlow(t, ctx, harness, flow, "rejected", publicationInput(title))
	require.Equal(t, PhaseRejected, publication.Phase)
	require.Equal(t, sdkgo.FailureValidation, publication.RejectionKind)
	require.Empty(t, publication.PageID)
	require.Zero(t, provider.pageCount(title))
}

func TestConcurrentEditIsParkedAndPublishedOverOnlyAfterReviewWithRealDex(t *testing.T) {
	provider, flow, harness := newConfluenceIntegrationHarness(t, integrationCloudID, shortRequestTimeout)
	ctx := integrationContext(t)
	title := markerConcurrentEdit + " Visitor policy"
	provider.seedPage(title, 5)
	flowID := startPublicationFlow(t, ctx, harness, flow, "concurrent-edit", publicationInput(title))

	parked := waitForPhase(t, ctx, harness, flow, flowID, PhaseNeedsConflictReview)
	require.Equal(t, 6, parked.ConflictingVersionNumber, "another editor saved version 6 first")
	require.Equal(t, 6, provider.currentVersion(title), "the other editor's version was not overwritten")

	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.PublishOverLatestVersion, nil, nil))
	publication := waitForFlowOutput(t, ctx, harness, flowID)
	require.Equal(t, PhasePublished, publication.Phase)
	require.Equal(t, 7, publication.VersionNumber)
	require.Equal(t, 7, provider.currentVersion(title))
}

// TestSlowCommentIsSentOnceUnderSyncDurabilityWithRealDex guards sync durability for addComment: a backup
// attempt cannot see the local attempt's checkpoint and would add a second comment.
func TestSlowCommentIsSentOnceUnderSyncDurabilityWithRealDex(t *testing.T) {
	provider, flow, harness := newConfluenceIntegrationHarness(t, integrationCloudID, slowRequestTimeout)
	ctx := integrationContext(t)

	input := publicationInput("Parking policy")
	input.PublicationComment = markerSlowComment + " Published for review."
	publication := runPublicationFlow(t, ctx, harness, flow, "slow-comment", input)
	require.Equal(t, PhasePublished, publication.Phase)
	require.NotEmpty(t, publication.CommentID)
	require.Equal(t, 1, provider.commentCount(publication.PageID))
}

func TestCommentTimeoutIsConfirmedByReadBackWithoutAResendWithRealDex(t *testing.T) {
	provider, flow, harness := newConfluenceIntegrationHarness(t, integrationCloudID, shortRequestTimeout)
	ctx := integrationContext(t)

	input := publicationInput("Fire drill policy")
	input.PublicationComment = markerCommentTimeout + " Published for review."
	publication := runPublicationFlow(t, ctx, harness, flow, "comment-timeout", input)
	require.Equal(t, PhasePublished, publication.Phase)
	require.False(t, publication.IsCommentOutcomeUnknown)
	require.NotEmpty(t, publication.CommentID, "the next attempt found the stored comment")
	require.Equal(t, 1, provider.commentCount(publication.PageID), "an unconfirmed comment is never re-sent")
}

func TestBlankCloudIDUsesTheOnlyGrantedSiteWithRealDex(t *testing.T) {
	provider, flow, harness := newConfluenceIntegrationHarness(t, "", shortRequestTimeout)
	ctx := integrationContext(t)

	publication := runPublicationFlow(t, ctx, harness, flow, "blank-site", publicationInput("Loading dock policy"))
	require.Equal(t, PhasePublished, publication.Phase)
	require.Equal(t, 1, provider.accessibleResourcesCount(), "the Worker's client caches the resolved site")
}

func publicationInput(title string) Input {
	return Input{Title: title, Body: integrationBody, SpaceKey: "OPS", PublicationComment: "Published for the **Q4** review."}
}

func runPublicationFlow(t *testing.T, ctx context.Context, harness *confluenceIntegrationHarness, flow *Flow, scenario string, input Input) PolicyPublication {
	t.Helper()
	flowID := startPublicationFlow(t, ctx, harness, flow, scenario, input)
	return waitForFlowOutput(t, ctx, harness, flowID)
}

func startPublicationFlow(t *testing.T, ctx context.Context, harness *confluenceIntegrationHarness, flow *Flow, scenario string, input Input) string {
	t.Helper()
	flowID := "confluence-policy-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	return flowID
}

func waitForFlowOutput(t *testing.T, ctx context.Context, harness *confluenceIntegrationHarness, flowID string) PolicyPublication {
	t.Helper()
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s", flowID)
	var publication PolicyPublication
	require.NoError(t, result.DecodeSingleOutput(&publication))
	return publication
}

func waitForPhase(t *testing.T, ctx context.Context, harness *confluenceIntegrationHarness, flow *Flow, flowID string, phase string) PolicyPublication {
	t.Helper()
	var publication PolicyPublication
	require.Eventually(t, func() bool {
		publication = PolicyPublication{}
		return harness.client.InvokeRPC(ctx, flowID, flow.GetPolicyPublication, nil, &publication) == nil && publication.Phase == phase
	}, 30*time.Second, 100*time.Millisecond, "Flow %s last publication %+v", flowID, publication)
	return publication
}

func integrationContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// fakeConfluence keeps one page per title, accepts each version once, and stores comments; markers select behaviors.
type fakeConfluence struct {
	*httptest.Server
	t                 *testing.T
	mutex             sync.Mutex
	nextID            int
	pages             map[string]*fakePage
	createTimes       map[string][]time.Time
	updates           map[string]int
	comments          map[string][]fakeComment
	searches          []string
	resourcesRequests int
}

type fakePage struct {
	id        string
	title     string
	parentID  string
	createdAt time.Time
	versions  []fakeVersion
}

type fakeVersion struct {
	number    int
	message   string
	storage   string
	createdAt time.Time
}

type fakeComment struct {
	id        string
	storage   string
	createdAt time.Time
}

func newFakeConfluence(t *testing.T) *fakeConfluence {
	t.Helper()
	provider := &fakeConfluence{
		t: t, nextID: 600000, pages: map[string]*fakePage{}, createTimes: map[string][]time.Time{},
		updates: map[string]int{}, comments: map[string][]fakeComment{},
	}
	provider.seedPage("Remote work policy (2019 draft)", 2)
	provider.seedPage(existingPolicyTitle, 3)
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeConfluence) seedPage(title string, versionNumber int) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	page := provider.storePageLocked(title, integrationHomepageID, "<p>Earlier policy text</p>", time.Now().Add(-48*time.Hour))
	for number := 2; number <= versionNumber; number++ {
		page.versions = append(page.versions, fakeVersion{number: number, message: "Edited in the browser", storage: "<p>Earlier policy text</p>", createdAt: time.Now().Add(-time.Hour)})
	}
}

func (provider *fakeConfluence) serveHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Authorization") != "Bearer "+integrationAccessToken {
		provider.writeJSON(response, http.StatusUnauthorized, `{"code":401,"message":"SENTINEL Unauthorized"}`)
		return
	}
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/oauth/token/accessible-resources":
		provider.mutex.Lock()
		provider.resourcesRequests++
		provider.mutex.Unlock()
		provider.writeJSON(response, http.StatusOK, `[{"id":"`+integrationCloudID+`","name":"Ops","url":"https://ops.atlassian.net","scopes":["search:confluence","read:page:confluence"]}]`)
	case request.Method == http.MethodGet && request.URL.Path == sitePrefix+"/rest/api/content/search":
		provider.searchPages(response, request)
	case request.Method == http.MethodGet && request.URL.Path == sitePrefix+"/api/v2/spaces":
		provider.writeJSON(response, http.StatusOK, `{"results":[{"id":"`+integrationSpaceID+`","key":"OPS","name":"Operations"}]}`)
	case request.Method == http.MethodGet && request.URL.Path == sitePrefix+"/api/v2/pages":
		provider.lookupPage(response, request)
	case request.Method == http.MethodPost && request.URL.Path == sitePrefix+"/api/v2/pages":
		provider.createPage(response, request)
	case request.Method == http.MethodPost && request.URL.Path == sitePrefix+"/api/v2/footer-comments":
		provider.createComment(response, request)
	default:
		match := pagePathPattern.FindStringSubmatch(request.URL.Path)
		if match == nil {
			provider.writeJSON(response, http.StatusNotFound, `{"errors":[{"title":"SENTINEL no route"}]}`)
			return
		}
		provider.servePage(response, request, match[1], match[2])
	}
}

func (provider *fakeConfluence) searchPages(response http.ResponseWriter, request *http.Request) {
	cql := request.URL.Query().Get("cql")
	phrase := ""
	if match := regexp.MustCompile(`title ~ "\\"(.*)\\""`).FindStringSubmatch(cql); match != nil {
		phrase = strings.ToLower(match[1])
	}
	provider.mutex.Lock()
	provider.searches = append(provider.searches, cql)
	var results []string
	for _, page := range provider.pages {
		// Search is eventually consistent: pages created by this test run are not indexed yet.
		if page.createdAt.Before(time.Now().Add(-time.Hour)) && strings.Contains(strings.ToLower(page.title), phrase) && page.title != existingPolicyTitle {
			version := page.versions[len(page.versions)-1]
			results = append(results, fmt.Sprintf(`{"id":%q,"type":"page","status":"current","title":%q,"space":{"id":%s,"key":"OPS"},"version":{"number":%d,"when":%q},"_links":{"webui":"/spaces/OPS/pages/%s"}}`,
				page.id, page.title, integrationSpaceID, version.number, version.createdAt.UTC().Format(time.RFC3339), page.id))
		}
	}
	provider.mutex.Unlock()
	provider.writeJSON(response, http.StatusOK, `{"results":[`+strings.Join(results, ",")+`],"_links":{"base":"https://ops.atlassian.net/wiki"}}`)
}

func (provider *fakeConfluence) lookupPage(response http.ResponseWriter, request *http.Request) {
	require.Equal(provider.t, integrationSpaceID, request.URL.Query().Get("space-id"))
	provider.mutex.Lock()
	page, isFound := provider.pages[request.URL.Query().Get("title")]
	body := `{"results":[],"_links":{"base":"https://ops.atlassian.net/wiki"}}`
	if isFound {
		body = `{"results":[` + provider.pageJSONLocked(page) + `],"_links":{"base":"https://ops.atlassian.net/wiki"}}`
	}
	provider.mutex.Unlock()
	provider.writeJSON(response, http.StatusOK, body)
}

func (provider *fakeConfluence) createPage(response http.ResponseWriter, request *http.Request) {
	var body struct {
		SpaceID  string `json:"spaceId"`
		Title    string `json:"title"`
		ParentID string `json:"parentId"`
		Body     struct {
			Value string `json:"value"`
		} `json:"body"`
	}
	require.NoError(provider.t, json.NewDecoder(request.Body).Decode(&body))
	require.Equal(provider.t, integrationSpaceID, body.SpaceID)
	provider.mutex.Lock()
	provider.createTimes[body.Title] = append(provider.createTimes[body.Title], time.Now())
	attempt := len(provider.createTimes[body.Title])
	_, isTaken := provider.pages[body.Title]
	provider.mutex.Unlock()
	switch {
	case isTaken:
		provider.writeJSON(response, http.StatusBadRequest, `{"errors":[{"status":400,"code":"BAD_REQUEST","title":"SENTINEL A page with this title already exists"}]}`)
		return
	case strings.Contains(body.Title, markerReject):
		provider.writeJSON(response, http.StatusBadRequest, `{"errors":[{"status":400,"title":"SENTINEL invalid storage format"}]}`)
		return
	case strings.Contains(body.Title, markerRateLimit) && attempt == 1:
		response.Header().Set("Retry-After", "1")
		provider.writeJSON(response, http.StatusTooManyRequests, `{"message":"SENTINEL rate limited"}`)
		return
	}
	parentID := body.ParentID
	if parentID == "" {
		parentID = integrationHomepageID
	}
	provider.mutex.Lock()
	page := provider.storePageLocked(body.Title, parentID, body.Body.Value, time.Now())
	created := provider.pageJSONLocked(page)
	provider.mutex.Unlock()
	switch {
	case strings.Contains(body.Title, markerCreateTimeout):
		// Confluence stored the page, but the response never arrives before the connector gives up.
		<-request.Context().Done()
	case strings.Contains(body.Title, markerSlowCreate):
		time.Sleep(slowProviderDelay)
		provider.writeJSON(response, http.StatusOK, created)
	default:
		provider.writeJSON(response, http.StatusOK, created)
	}
}

func (provider *fakeConfluence) servePage(response http.ResponseWriter, request *http.Request, pageID string, subresource string) {
	provider.mutex.Lock()
	page := provider.pageByIDLocked(pageID)
	provider.mutex.Unlock()
	if page == nil {
		provider.writeJSON(response, http.StatusNotFound, `{"errors":[{"status":404,"title":"SENTINEL Not Found"}]}`)
		return
	}
	switch {
	case request.Method == http.MethodGet && subresource == "":
		provider.mutex.Lock()
		body := provider.pageJSONLocked(page)
		provider.mutex.Unlock()
		provider.writeJSON(response, http.StatusOK, body)
	case request.Method == http.MethodGet && subresource == "/footer-comments":
		provider.listComments(response, pageID)
	case request.Method == http.MethodGet && strings.HasPrefix(subresource, "/versions/"):
		number, err := strconv.Atoi(versionNumberPath.FindStringSubmatch(subresource)[1])
		require.NoError(provider.t, err)
		provider.mutex.Lock()
		var version *fakeVersion
		for index := range page.versions {
			if page.versions[index].number == number {
				version = &page.versions[index]
			}
		}
		provider.mutex.Unlock()
		if version == nil {
			provider.writeJSON(response, http.StatusNotFound, `{}`)
			return
		}
		provider.writeJSON(response, http.StatusOK, fmt.Sprintf(`{"number":%d,"message":%q,"createdAt":%q}`, version.number, version.message, version.createdAt.UTC().Format(time.RFC3339)))
	case request.Method == http.MethodPut && subresource == "":
		provider.updatePage(response, request, page)
	default:
		provider.writeJSON(response, http.StatusMethodNotAllowed, `{}`)
	}
}

func (provider *fakeConfluence) updatePage(response http.ResponseWriter, request *http.Request, page *fakePage) {
	var body struct {
		Title string `json:"title"`
		Body  struct {
			Value string `json:"value"`
		} `json:"body"`
		Version struct {
			Number  int    `json:"number"`
			Message string `json:"message"`
		} `json:"version"`
	}
	require.NoError(provider.t, json.NewDecoder(request.Body).Decode(&body))
	provider.mutex.Lock()
	provider.updates[page.title]++
	if strings.Contains(page.title, markerConcurrentEdit) && provider.updates[page.title] == 1 {
		// Another editor saves the next version just before this update arrives.
		current := page.versions[len(page.versions)-1]
		page.versions = append(page.versions, fakeVersion{number: current.number + 1, message: "Edited in the browser", storage: current.storage, createdAt: time.Now()})
	}
	current := page.versions[len(page.versions)-1]
	isApplied := body.Version.Number == current.number+1
	if isApplied {
		page.versions = append(page.versions, fakeVersion{number: body.Version.Number, message: body.Version.Message, storage: body.Body.Value, createdAt: time.Now()})
	}
	updated := provider.pageJSONLocked(page)
	provider.mutex.Unlock()
	if !isApplied {
		provider.writeJSON(response, http.StatusConflict, `{"errors":[{"status":409,"title":"SENTINEL Version must be incremented"}]}`)
		return
	}
	if strings.Contains(page.title, markerSlowUpdate) {
		// Confluence saved the version, but the response arrives after Dex's async local phase ends.
		time.Sleep(slowProviderDelay)
	}
	provider.writeJSON(response, http.StatusOK, updated)
}

func (provider *fakeConfluence) createComment(response http.ResponseWriter, request *http.Request) {
	var body struct {
		PageID string `json:"pageId"`
		Body   struct {
			Value string `json:"value"`
		} `json:"body"`
	}
	contents, err := io.ReadAll(request.Body)
	require.NoError(provider.t, err)
	require.NoError(provider.t, json.Unmarshal(contents, &body))
	provider.mutex.Lock()
	provider.nextID++
	comment := fakeComment{id: strconv.Itoa(provider.nextID), storage: body.Body.Value, createdAt: time.Now()}
	provider.comments[body.PageID] = append(provider.comments[body.PageID], comment)
	provider.mutex.Unlock()
	switch {
	case strings.Contains(body.Body.Value, markerCommentTimeout):
		// Confluence stored the comment, but the response never arrives before the connector gives up.
		<-request.Context().Done()
	case strings.Contains(body.Body.Value, markerSlowComment):
		time.Sleep(slowProviderDelay)
		provider.writeJSON(response, http.StatusCreated, provider.commentJSON(body.PageID, comment))
	default:
		provider.writeJSON(response, http.StatusCreated, provider.commentJSON(body.PageID, comment))
	}
}

func (provider *fakeConfluence) listComments(response http.ResponseWriter, pageID string) {
	provider.mutex.Lock()
	var results []string
	for index := len(provider.comments[pageID]) - 1; index >= 0; index-- {
		results = append(results, provider.commentJSON(pageID, provider.comments[pageID][index]))
	}
	provider.mutex.Unlock()
	provider.writeJSON(response, http.StatusOK, `{"results":[`+strings.Join(results, ",")+`]}`)
}

func (provider *fakeConfluence) commentJSON(pageID string, comment fakeComment) string {
	encoded, err := json.Marshal(map[string]any{
		"id": comment.id, "status": "current", "pageId": pageID,
		"version": map[string]any{"number": 1, "createdAt": comment.createdAt.UTC().Format("2006-01-02T15:04:05.000Z")},
		"body":    map[string]any{"storage": map[string]any{"representation": "storage", "value": comment.storage}},
	})
	require.NoError(provider.t, err)
	return string(encoded)
}

// storePageLocked adds a page at version 1; the caller holds the mutex.
func (provider *fakeConfluence) storePageLocked(title string, parentID string, storage string, createdAt time.Time) *fakePage {
	provider.nextID++
	page := &fakePage{
		id: strconv.Itoa(provider.nextID), title: title, parentID: parentID, createdAt: createdAt,
		versions: []fakeVersion{{number: 1, storage: storage, createdAt: createdAt}},
	}
	provider.pages[title] = page
	return page
}

// pageJSONLocked renders the current version; the caller holds the mutex.
func (provider *fakeConfluence) pageJSONLocked(page *fakePage) string {
	version := page.versions[len(page.versions)-1]
	encoded, err := json.Marshal(map[string]any{
		"id": page.id, "status": "current", "title": page.title, "spaceId": integrationSpaceID, "parentId": page.parentID,
		"authorId": "5b10ac8d82e05b22cc7d4ef5", "createdAt": page.createdAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		"version": map[string]any{"number": version.number, "message": version.message, "createdAt": version.createdAt.UTC().Format("2006-01-02T15:04:05.000Z")},
		"body":    map[string]any{"storage": map[string]any{"representation": "storage", "value": version.storage}},
		"_links":  map[string]any{"webui": "/spaces/OPS/pages/" + page.id, "base": "https://ops.atlassian.net/wiki"},
	})
	require.NoError(provider.t, err)
	return string(encoded)
}

func (provider *fakeConfluence) pageByIDLocked(pageID string) *fakePage {
	for _, page := range provider.pages {
		if page.id == pageID {
			return page
		}
	}
	return nil
}

func (provider *fakeConfluence) writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if _, err := response.Write([]byte(body)); err != nil {
		provider.t.Logf("fake Confluence response write failed: %v", err)
	}
}

func (provider *fakeConfluence) createCount(title string) int {
	return len(provider.createTimesFor(title))
}

func (provider *fakeConfluence) createTimesFor(title string) []time.Time {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]time.Time(nil), provider.createTimes[title]...)
}

func (provider *fakeConfluence) pageCount(title string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	if _, isFound := provider.pages[title]; isFound {
		return 1
	}
	return 0
}

func (provider *fakeConfluence) currentVersion(title string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	versions := provider.pages[title].versions
	return versions[len(versions)-1].number
}

func (provider *fakeConfluence) versionMessage(title string, number int) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	for _, version := range provider.pages[title].versions {
		if version.number == number {
			return version.message
		}
	}
	return ""
}

func (provider *fakeConfluence) updateCount(title string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.updates[title]
}

func (provider *fakeConfluence) commentCount(pageID string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.comments[pageID])
}

func (provider *fakeConfluence) searchQueries() []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]string(nil), provider.searches...)
}

func (provider *fakeConfluence) accessibleResourcesCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.resourcesRequests
}

type confluenceIntegrationHarness struct {
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newConfluenceIntegrationHarness(t *testing.T, cloudID string, requestTimeout time.Duration) (*fakeConfluence, *Flow, *confluenceIntegrationHarness) {
	t.Helper()
	provider := newFakeConfluence(t)
	reference := sdkgo.ConnectionRef{Provider: "atlassian", Name: ConnectionName}
	providerClient, err := confluence.New(
		confluence.Config{CloudID: cloudID, Endpoint: provider.URL},
		sdkgo.StaticCredentialProvider[confluence.Credentials]{reference: {
			OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
			AccessToken: sdkgo.NewSecretString(integrationAccessToken), RefreshToken: sdkgo.NewSecretString("refresh-token"),
		}},
		confluence.WithHTTPClient(&http.Client{Timeout: requestTimeout}),
	)
	require.NoError(t, err)
	connection, err := confluence.NewConnection(providerClient, reference)
	require.NoError(t, err)
	flow := NewFlow(connection, SpaceSelection{})
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	workerAddress := net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness := &confluenceIntegrationHarness{
		registry: registry, cache: cache, workerAddress: workerAddress,
		serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801"),
	}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: workerAddress, FlowServiceAddress: harness.serverAddress, WorkerTarget: dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	harness.worker = worker
	harness.workerResult = make(chan error, 1)
	go func() { harness.workerResult <- worker.Start() }()
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(stopCtx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	return provider, flow, harness
}

func availableIntegrationPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}

func environmentOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
