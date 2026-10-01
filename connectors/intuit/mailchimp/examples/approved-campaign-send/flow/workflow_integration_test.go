//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package approvedcampaignsend

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationAPIKey     = "0123456789abcdef0123456789abcdef" + "-us6"
	integrationListID     = "57afe96172"
	integrationCampaignID = "42694e9e57"
	integrationTag        = "spring-launch"
	// danaSubscriberHash is the MD5 hash of dana.diaz@example.com, the lowercased form of Dana.Diaz@Example.com.
	danaSubscriberHash = "cccd26f6b8eb83e52443ce371e249ece"

	defaultRequestTimeout = 5 * time.Second
	// slowResponseDelay outlasts Dex's roughly seven-second async local phase, so an async Step is dispatched again.
	slowResponseDelay  = 9 * time.Second
	slowRequestTimeout = 20 * time.Second
)

func integrationLaunchInput() Input {
	return Input{
		ListID: integrationListID, CampaignID: integrationCampaignID, Tag: integrationTag, StatusIfNew: mailchimp.MemberStatusPending,
		Contacts: []ContactInput{
			{EmailAddress: "ben@example.com", FirstName: "Ben"},
			{EmailAddress: "cara@example.com", FirstName: "Cara"},
			{EmailAddress: "Dana.Diaz@Example.com", FirstName: "Dana", LastName: "Diaz"},
		},
	}
}

func approval() SendDecisionInput {
	note := "Copy and audience checked."
	return SendDecisionInput{DecidedBy: "Grace Hopper", Note: &note}
}

func TestApprovedLaunchEnrollsContactsLeavesUnsubscribedAloneAndSendsOnceWithRealDex(t *testing.T) {
	provider := newFakeMailchimp(t)
	provider.seedMember("ben@example.com", "subscribed", "vip")
	provider.seedMember("cara@example.com", "unsubscribed")
	provider.seedMember("ed@example.com", "subscribed")
	provider.seedMember("fay@example.com", "subscribed")
	harness := newLaunchHarness(t, provider, defaultRequestTimeout)

	flowID := harness.startLaunch(t, "approved", integrationLaunchInput())
	waiting := harness.waitForPhase(t, flowID, PhaseAwaitingApproval)
	require.Zero(t, provider.count("send"), "nothing is sent before a person approves")
	require.Zero(t, provider.count("campaign"), "the campaign is read only by the send Step")
	require.Equal(t, 3, waiting.SubscribedMemberCount, "Ben, Ed, and Fay; Dana is pending and Cara unsubscribed")
	require.Equal(t, []ContactOutcome{
		{EmailAddress: "ben@example.com", SubscriberHash: mailchimp.SubscriberHash("ben@example.com"), Action: ContactEnrolled,
			PreviousStatus: mailchimp.MemberStatusSubscribed, Status: mailchimp.MemberStatusSubscribed},
		{EmailAddress: "cara@example.com", SubscriberHash: mailchimp.SubscriberHash("cara@example.com"), Action: ContactSuppressed,
			PreviousStatus: mailchimp.MemberStatusUnsubscribed, Status: mailchimp.MemberStatusUnsubscribed},
		{EmailAddress: "Dana.Diaz@Example.com", SubscriberHash: danaSubscriberHash, Action: ContactEnrolled,
			Status: mailchimp.MemberStatusPending},
	}, waiting.Contacts)

	require.Equal(t, []string{
		"/3.0/lists/" + integrationListID + "/members/" + mailchimp.SubscriberHash("ben@example.com"),
		"/3.0/lists/" + integrationListID + "/members/" + danaSubscriberHash,
	}, provider.paths("upsert"), "Cara unsubscribed, so she is never written; Dana is addressed by her lowercased hash")
	for _, body := range provider.bodies("upsert") {
		require.Contains(t, body, `"status_if_new":"pending"`)
		require.NotContains(t, body, `"status":`, "the upsert never changes an existing contact's status")
	}
	require.Equal(t, 1, provider.confirmationEmailCount(), "only Dana is new, so Mailchimp sends one confirmation email")

	require.NoError(t, harness.client.InvokeRPC(integrationContext(t), flowID, harness.flow.ApproveCampaignSend, approval(), nil))
	record := harness.waitForFlowOutput(t, flowID)
	require.Equal(t, PhaseSent, record.Phase)
	require.Equal(t, &SendDecision{DecidedBy: "Grace Hopper", Note: "Copy and audience checked."}, record.Approval)
	require.Equal(t, 1, record.SendExecutions)
	require.Equal(t, mailchimp.CampaignStatusSave, record.Campaign.Status, "the campaign as read before the send")
	require.Equal(t, "Spring launch", record.Campaign.Title)
	require.Equal(t, 1, provider.count("send"))
	require.Equal(t, 1, provider.appliedSendCount())

	require.Equal(t, []string{integrationTag, "vip"}, provider.member("ben@example.com").sortedTags())
	require.Empty(t, provider.member("cara@example.com").sortedTags())
	require.Equal(t, []string{integrationTag}, provider.member("dana.diaz@example.com").sortedTags())
	require.Equal(t, "unsubscribed", provider.member("cara@example.com").status)
	require.Equal(t, map[string]any{"FNAME": "Dana", "LNAME": "Diaz"}, provider.member("dana.diaz@example.com").mergeFields)
}

func TestDeclinedLaunchCompletesWithoutSendingWithRealDex(t *testing.T) {
	provider := newFakeMailchimp(t)
	harness := newLaunchHarness(t, provider, defaultRequestTimeout)
	flowID := harness.startLaunch(t, "declined", integrationLaunchInput())
	harness.waitForPhase(t, flowID, PhaseAwaitingApproval)

	reason := "Hold until the pricing page is live."
	require.NoError(t, harness.client.InvokeRPC(integrationContext(t), flowID, harness.flow.DeclineCampaignSend, SendDecisionInput{DecidedBy: "Ada", Note: &reason}, nil))
	record := harness.waitForFlowOutput(t, flowID)
	require.Equal(t, PhaseDeclined, record.Phase)
	require.Equal(t, &SendDecision{DecidedBy: "Ada", Note: reason}, record.Decline)
	require.Nil(t, record.Approval)
	require.Zero(t, provider.count("campaign"))
	require.Zero(t, provider.count("send"))
}

// TestRepeatedApprovalSchedulesOneSendWithRealDex approves twice at once; only the awaitingApproval phase schedules a send.
func TestRepeatedApprovalSchedulesOneSendWithRealDex(t *testing.T) {
	provider := newFakeMailchimp(t)
	harness := newLaunchHarness(t, provider, defaultRequestTimeout)
	flowID := harness.startLaunch(t, "double-approval", integrationLaunchInput())
	harness.waitForPhase(t, flowID, PhaseAwaitingApproval)

	var approvals sync.WaitGroup
	approvalErrors := make([]error, 2)
	for index := range approvalErrors {
		approvals.Add(1)
		go func() {
			defer approvals.Done()
			approvalErrors[index] = harness.client.InvokeRPC(integrationContext(t), flowID, harness.flow.ApproveCampaignSend, approval(), nil)
		}()
	}
	approvals.Wait()
	for _, err := range approvalErrors {
		var lockConflict *dex.RPCLockConflictError
		if !errors.As(err, &lockConflict) {
			require.NoError(t, err)
		}
	}
	record := harness.waitForFlowOutput(t, flowID)
	require.Equal(t, PhaseSent, record.Phase)
	require.Equal(t, 1, record.SendExecutions, "the second approval found the send already scheduled")
	require.Equal(t, 1, provider.count("campaign"))
	require.Equal(t, 1, provider.count("send"))

	err := harness.client.InvokeRPC(integrationContext(t), flowID, harness.flow.ApproveCampaignSend, approval(), nil)
	require.Error(t, err, "a completed Flow accepts no further approval")
	require.Equal(t, 1, provider.count("send"))
}

// TestSlowUpsertAndTagsDispatchedTwiceWriteOneContactWithRealDex holds both writes past Dex's async local phase.
func TestSlowUpsertAndTagsDispatchedTwiceWriteOneContactWithRealDex(t *testing.T) {
	provider := newFakeMailchimp(t)
	provider.delaysFirstUpsert, provider.delaysFirstTags = true, true
	harness := newLaunchHarness(t, provider, slowRequestTimeout)
	input := integrationLaunchInput()
	input.Contacts = []ContactInput{{EmailAddress: "Dana.Diaz@Example.com", FirstName: "Dana"}}

	flowID := harness.startLaunch(t, "slow-upsert", input)
	waiting := harness.waitForPhase(t, flowID, PhaseAwaitingApproval)
	provider.waitForDelayedRequests(t)
	require.GreaterOrEqual(t, provider.count("upsert"), 2, "Dex dispatched the upsert again past its local phase")
	require.GreaterOrEqual(t, provider.count("tags"), 2, "Dex dispatched the tag update again past its local phase")
	require.Equal(t, 1, provider.createdMemberCount(), "a repeated PUT keyed by the address creates no second contact")
	require.Equal(t, 1, provider.confirmationEmailCount(), "the repeated PUT found the pending contact and sent no second confirmation")
	require.Equal(t, "pending", provider.member("dana.diaz@example.com").status)
	require.Equal(t, []string{integrationTag}, provider.member("dana.diaz@example.com").sortedTags())
	require.Len(t, waiting.Contacts, 1)
	t.Logf("slow writes: upserts=%d tags=%d", provider.count("upsert"), provider.count("tags"))

	require.NoError(t, harness.client.InvokeRPC(integrationContext(t), flowID, harness.flow.ApproveCampaignSend, approval(), nil))
	require.Equal(t, PhaseSent, harness.waitForFlowOutput(t, flowID).Phase)
}

// TestSlowSendIsSentOnceWithRealDex proves sync durability never dispatches a second in-flight send.
func TestSlowSendIsSentOnceWithRealDex(t *testing.T) {
	provider := newFakeMailchimp(t)
	provider.delaysFirstSend = true
	harness := newLaunchHarness(t, provider, slowRequestTimeout)
	flowID := harness.startLaunch(t, "slow-send", integrationLaunchInput())
	harness.waitForPhase(t, flowID, PhaseAwaitingApproval)

	approvedAt := time.Now()
	require.NoError(t, harness.client.InvokeRPC(integrationContext(t), flowID, harness.flow.ApproveCampaignSend, approval(), nil))
	record := harness.waitForFlowOutput(t, flowID)
	require.GreaterOrEqual(t, time.Since(approvedAt), slowResponseDelay)
	require.Equal(t, PhaseSent, record.Phase)
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.count("send"), "no second dispatch while the first was in flight")
	require.Equal(t, 1, provider.count("campaign"))
}

func TestLostSendResponseNeedsReviewAndARecheckFindsItSentWithRealDex(t *testing.T) {
	provider := newFakeMailchimp(t)
	provider.losesFirstSendResponse = true
	harness := newLaunchHarness(t, provider, defaultRequestTimeout)
	flowID := harness.startLaunch(t, "lost-send", integrationLaunchInput())
	harness.waitForPhase(t, flowID, PhaseAwaitingApproval)
	require.NoError(t, harness.client.InvokeRPC(integrationContext(t), flowID, harness.flow.ApproveCampaignSend, approval(), nil))

	review := harness.waitForPhase(t, flowID, PhaseNeedsReview)
	require.NotNil(t, review.UncertainSend)
	require.NotEmpty(t, review.UncertainSend.CallID)
	require.Equal(t, sdkgo.FailureTransport, review.UncertainSend.FailureKind)
	require.Equal(t, "Mailchimp request failed before a response arrived", review.UncertainSend.FailureMessage)
	require.Equal(t, 1, provider.count("send"), "an unconfirmed send is never resent")

	require.NoError(t, harness.client.InvokeRPC(integrationContext(t), flowID, harness.flow.RecheckCampaignSend, SendDecisionInput{DecidedBy: "Grace Hopper"}, nil))
	record := harness.waitForFlowOutput(t, flowID)
	require.Equal(t, PhaseAlreadySent, record.Phase, "the recheck read Mailchimp's sending status and sent nothing")
	require.Equal(t, mailchimp.CampaignStatusSending, record.Campaign.Status)
	require.Equal(t, 2, record.SendExecutions)
	require.Equal(t, 1, provider.count("send"))
	require.Equal(t, 1, provider.appliedSendCount())
}

// TestLostWorkerDuringSendReportsAlreadySentWithoutResendingWithRealDex replaces the Worker mid-send.
func TestLostWorkerDuringSendReportsAlreadySentWithoutResendingWithRealDex(t *testing.T) {
	provider := newFakeMailchimp(t)
	provider.holdsFirstSend = make(chan struct{})
	harness := newLaunchHarness(t, provider, slowRequestTimeout)
	flowID := harness.startLaunch(t, "lost-worker", integrationLaunchInput())
	harness.waitForPhase(t, flowID, PhaseAwaitingApproval)
	require.NoError(t, harness.client.InvokeRPC(integrationContext(t), flowID, harness.flow.ApproveCampaignSend, approval(), nil))
	require.Eventually(t, func() bool { return provider.count("send") == 1 }, 30*time.Second, 50*time.Millisecond,
		"the first attempt must reach Mailchimp")
	harness.replaceWorker(t)

	record := harness.waitForFlowOutput(t, flowID)
	close(provider.holdsFirstSend)
	require.Equal(t, PhaseAlreadySent, record.Phase, "the new Worker's attempt found the checkpoint and Mailchimp's sending status")
	require.Equal(t, 1, record.SendExecutions)
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.count("send"), "the attempt on the new Worker did not resend the campaign")
	require.Equal(t, 2, provider.count("campaign"), "one read before the send and one read by the replayed attempt")
}

func TestRateLimitedSendWaitsAndSendsOnceWithRealDex(t *testing.T) {
	provider := newFakeMailchimp(t)
	provider.rateLimitsFirstSend = true
	harness := newLaunchHarness(t, provider, defaultRequestTimeout)
	flowID := harness.startLaunch(t, "rate-limited", integrationLaunchInput())
	harness.waitForPhase(t, flowID, PhaseAwaitingApproval)
	require.NoError(t, harness.client.InvokeRPC(integrationContext(t), flowID, harness.flow.ApproveCampaignSend, approval(), nil))

	record := harness.waitForFlowOutput(t, flowID)
	require.Equal(t, PhaseSent, record.Phase, "a 429 clears the checkpoint, so the retry may send")
	times := provider.requestTimes("send")
	require.Len(t, times, 2)
	require.GreaterOrEqual(t, times[1].Sub(times[0]), time.Second, "the retry waited for Retry-After")
	require.Equal(t, 1, provider.appliedSendCount())
}

func TestCampaignAlreadySentOrForAnotherAudienceIsNeverSentWithRealDex(t *testing.T) {
	for _, test := range []struct {
		name   string
		status string
		listID string
		phase  string
	}{
		{name: "already-sent", status: "sent", listID: integrationListID, phase: PhaseAlreadySent},
		{name: "another-audience", status: "save", listID: "b1c2d3e4f5", phase: PhaseNotSent},
		{name: "scheduled", status: "schedule", listID: integrationListID, phase: PhaseNotSent},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newFakeMailchimp(t)
			provider.campaign.status, provider.campaign.listID = test.status, test.listID
			harness := newLaunchHarness(t, provider, defaultRequestTimeout)
			flowID := harness.startLaunch(t, test.name, integrationLaunchInput())
			harness.waitForPhase(t, flowID, PhaseAwaitingApproval)
			require.NoError(t, harness.client.InvokeRPC(integrationContext(t), flowID, harness.flow.ApproveCampaignSend, approval(), nil))

			record := harness.waitForFlowOutput(t, flowID)
			require.Equal(t, test.phase, record.Phase)
			require.Zero(t, provider.count("send"))
			if test.phase == PhaseNotSent {
				require.True(t, strings.HasPrefix(record.NotSentReason, "notSendable: "), record.NotSentReason)
			}
		})
	}
}

func TestRejectedContactIsRecordedAndTheLaunchContinuesWithRealDex(t *testing.T) {
	provider := newFakeMailchimp(t)
	provider.rejectsUpsertFor = "ben@example.com"
	harness := newLaunchHarness(t, provider, defaultRequestTimeout)
	flowID := harness.startLaunch(t, "rejected-contact", integrationLaunchInput())

	waiting := harness.waitForPhase(t, flowID, PhaseAwaitingApproval)
	require.Len(t, waiting.Contacts, 3)
	require.Equal(t, ContactRejected, waiting.Contacts[0].Action)
	require.Equal(t, "Mailchimp rejected the request (HTTP 400) [Member In Compliance State]", waiting.Contacts[0].FailureMessage,
		"only Mailchimp's title, never its detail")
	require.Equal(t, ContactEnrolled, waiting.Contacts[1].Action)
	require.Equal(t, ContactEnrolled, waiting.Contacts[2].Action)
	require.Zero(t, provider.countForPath("tags", mailchimp.SubscriberHash("ben@example.com")), "a rejected contact is not tagged")

	require.NoError(t, harness.client.InvokeRPC(integrationContext(t), flowID, harness.flow.DeclineCampaignSend, SendDecisionInput{DecidedBy: "Ada"}, nil))
	require.Equal(t, PhaseDeclined, harness.waitForFlowOutput(t, flowID).Phase)
}

func TestInvalidLaunchFailsBeforeCallingMailchimpWithRealDex(t *testing.T) {
	provider := newFakeMailchimp(t)
	harness := newLaunchHarness(t, provider, defaultRequestTimeout)
	input := integrationLaunchInput()
	input.StatusIfNew = mailchimp.MemberStatusUnsubscribed
	flowID := harness.startLaunch(t, "invalid", input)

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Zero(t, provider.totalRequests())
}

// fakeMailchimp is a stateful Mailchimp Marketing API fake for one audience and one campaign, without idempotency keys.
type fakeMailchimp struct {
	*httptest.Server
	t     *testing.T
	mutex sync.Mutex
	// delayedRequests tracks handlers still sleeping, so a test can assert their final effect.
	delayedRequests sync.WaitGroup

	members            map[string]*fakeMember
	campaign           *fakeCampaign
	createdMembers     int
	confirmationEmails int
	counts             map[string]int
	requests           map[string][]fakeRecordedRequest

	delaysFirstUpsert      bool
	delaysFirstTags        bool
	delaysFirstSend        bool
	losesFirstSendResponse bool
	holdsFirstSend         chan struct{}
	rateLimitsFirstSend    bool
	rejectsUpsertFor       string
}

type fakeMember struct {
	email       string
	status      string
	mergeFields map[string]any
	tags        map[string]bool
	lastChanged time.Time
}

type fakeCampaign struct {
	id           string
	listID       string
	status       string
	campaignType string
	appliedSends int
}

type fakeRecordedRequest struct {
	at   time.Time
	path string
	body string
}

func newFakeMailchimp(t *testing.T) *fakeMailchimp {
	t.Helper()
	provider := &fakeMailchimp{
		t: t, members: map[string]*fakeMember{}, counts: map[string]int{}, requests: map[string][]fakeRecordedRequest{},
		campaign: &fakeCampaign{id: integrationCampaignID, listID: integrationListID, status: "save", campaignType: "regular"},
	}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeMailchimp) serveHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Authorization") != "Bearer "+integrationAPIKey {
		provider.writeProblem(response, http.StatusUnauthorized, "API Key Invalid")
		return
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		provider.writeProblem(response, http.StatusBadRequest, "JSON Parse Exception")
		return
	}
	path := strings.TrimPrefix(request.URL.Path, "/3.0")
	membersPrefix := "/lists/" + integrationListID + "/members"
	campaignPath := "/campaigns/" + integrationCampaignID
	switch {
	case request.Method == http.MethodGet && path == membersPrefix:
		provider.listMembers(response, request, body)
	case strings.HasPrefix(path, membersPrefix+"/"):
		segments := strings.Split(strings.TrimPrefix(path, membersPrefix+"/"), "/")
		switch {
		case len(segments) == 1 && request.Method == http.MethodGet:
			provider.getMember(response, request, segments[0], body)
		case len(segments) == 1 && request.Method == http.MethodPut:
			provider.upsertMember(response, request, segments[0], body)
		case len(segments) == 2 && segments[1] == "tags" && request.Method == http.MethodPost:
			provider.updateTags(response, request, segments[0], body)
		default:
			provider.writeProblem(response, http.StatusMethodNotAllowed, "Method Not Allowed")
		}
	case request.Method == http.MethodGet && path == campaignPath:
		provider.record("campaign", request, body)
		provider.writeValue(response, http.StatusOK, provider.campaignJSON())
	case request.Method == http.MethodPost && path == campaignPath+"/actions/send":
		provider.sendCampaign(response, request, body)
	default:
		provider.writeProblem(response, http.StatusNotFound, "Resource Not Found")
	}
}

func (provider *fakeMailchimp) getMember(response http.ResponseWriter, request *http.Request, hash string, body []byte) {
	provider.record("get", request, body)
	provider.mutex.Lock()
	member, isFound := provider.members[hash]
	var value map[string]any
	if isFound {
		value = member.json()
	}
	provider.mutex.Unlock()
	if !isFound {
		provider.writeProblem(response, http.StatusNotFound, "Resource Not Found")
		return
	}
	provider.writeValue(response, http.StatusOK, value)
}

func (provider *fakeMailchimp) upsertMember(response http.ResponseWriter, request *http.Request, hash string, body []byte) {
	attempt := provider.record("upsert", request, body)
	var put struct {
		EmailAddress string         `json:"email_address"`
		StatusIfNew  string         `json:"status_if_new"`
		Status       string         `json:"status"`
		MergeFields  map[string]any `json:"merge_fields"`
	}
	if json.Unmarshal(body, &put) != nil || mailchimp.SubscriberHash(put.EmailAddress) != hash || put.StatusIfNew == "" {
		provider.writeProblem(response, http.StatusBadRequest, "Invalid Resource")
		return
	}
	if strings.EqualFold(put.EmailAddress, provider.rejectsUpsertFor) {
		provider.writeProblem(response, http.StatusBadRequest, "Member In Compliance State")
		return
	}
	if provider.delaysFirstUpsert && attempt == 1 {
		provider.sleepWhileDelayed()
	}
	provider.mutex.Lock()
	member, isFound := provider.members[hash]
	if !isFound {
		status := put.StatusIfNew
		if put.Status != "" {
			status = put.Status
		}
		member = &fakeMember{email: put.EmailAddress, status: status, mergeFields: map[string]any{}, tags: map[string]bool{}}
		provider.members[hash] = member
		provider.createdMembers++
		if status == "pending" {
			provider.confirmationEmails++
		}
	} else if put.Status != "" {
		member.status = put.Status
	}
	for tag, value := range put.MergeFields {
		member.mergeFields[tag] = value
	}
	member.lastChanged = time.Now().UTC()
	value := member.json()
	provider.mutex.Unlock()
	provider.writeValue(response, http.StatusOK, value)
}

func (provider *fakeMailchimp) updateTags(response http.ResponseWriter, request *http.Request, hash string, body []byte) {
	attempt := provider.record("tags", request, body)
	var post struct {
		Tags []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"tags"`
	}
	if json.Unmarshal(body, &post) != nil || len(post.Tags) == 0 {
		provider.writeProblem(response, http.StatusBadRequest, "Invalid Resource")
		return
	}
	if provider.delaysFirstTags && attempt == 1 {
		provider.sleepWhileDelayed()
	}
	provider.mutex.Lock()
	member, isFound := provider.members[hash]
	if isFound {
		for _, tag := range post.Tags {
			if tag.Status == "active" {
				member.tags[tag.Name] = true
			} else {
				delete(member.tags, tag.Name)
			}
		}
	}
	provider.mutex.Unlock()
	if !isFound {
		provider.writeProblem(response, http.StatusNotFound, "Resource Not Found")
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (provider *fakeMailchimp) listMembers(response http.ResponseWriter, request *http.Request, body []byte) {
	provider.record("list", request, body)
	query := request.URL.Query()
	count, countErr := strconv.Atoi(query.Get("count"))
	offset, offsetErr := strconv.Atoi(query.Get("offset"))
	if countErr != nil || offsetErr != nil || count < 1 || count > 1000 || query.Get("sort_field") != "last_changed" {
		provider.writeProblem(response, http.StatusBadRequest, "Invalid Resource")
		return
	}
	provider.mutex.Lock()
	var matching []*fakeMember
	for _, member := range provider.members {
		if status := query.Get("status"); status == "" || member.status == status {
			matching = append(matching, member)
		}
	}
	sort.Slice(matching, func(left, right int) bool { return matching[left].lastChanged.Before(matching[right].lastChanged) })
	page := []map[string]any{}
	for index := offset; index < len(matching) && len(page) < count; index++ {
		page = append(page, matching[index].json())
	}
	total := len(matching)
	provider.mutex.Unlock()
	provider.writeValue(response, http.StatusOK, map[string]any{"members": page, "list_id": integrationListID, "total_items": total})
}

func (provider *fakeMailchimp) sendCampaign(response http.ResponseWriter, request *http.Request, body []byte) {
	attempt := provider.record("send", request, body)
	if provider.rateLimitsFirstSend && attempt == 1 {
		response.Header().Set("Retry-After", "1")
		provider.writeProblem(response, http.StatusTooManyRequests, "Too Many Requests")
		return
	}
	provider.mutex.Lock()
	if provider.campaign.status != "save" {
		provider.mutex.Unlock()
		provider.writeProblem(response, http.StatusBadRequest, "Bad Request")
		return
	}
	provider.campaign.status = "sending"
	provider.campaign.appliedSends++
	provider.mutex.Unlock()
	switch {
	case provider.delaysFirstSend && attempt == 1:
		provider.sleepWhileDelayed()
	case provider.holdsFirstSend != nil && attempt == 1:
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		select {
		case <-provider.holdsFirstSend:
		case <-time.After(2 * time.Minute):
		}
	case provider.losesFirstSendResponse && attempt == 1:
		hijacker, isHijacker := response.(http.Hijacker)
		require.True(provider.t, isHijacker)
		connection, _, err := hijacker.Hijack()
		require.NoError(provider.t, err)
		require.NoError(provider.t, connection.Close())
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (provider *fakeMailchimp) sleepWhileDelayed() {
	provider.delayedRequests.Add(1)
	defer provider.delayedRequests.Done()
	time.Sleep(slowResponseDelay)
}

func (provider *fakeMailchimp) campaignJSON() map[string]any {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return map[string]any{
		"id": provider.campaign.id, "web_id": 98765, "type": provider.campaign.campaignType, "status": provider.campaign.status,
		"emails_sent": 0, "create_time": "2026-01-20T10:00:00+00:00", "send_time": "",
		"settings":   map[string]any{"title": "Spring launch", "subject_line": "Our spring launch is here"},
		"recipients": map[string]any{"list_id": provider.campaign.listID, "list_name": "Customers", "recipient_count": 3},
	}
}

// json requires provider.mutex.
func (member *fakeMember) json() map[string]any {
	tags := []map[string]any{}
	for index, name := range member.sortedTags() {
		tags = append(tags, map[string]any{"id": index + 1, "name": name})
	}
	return map[string]any{
		"id": mailchimp.SubscriberHash(member.email), "email_address": member.email, "status": member.status,
		"merge_fields": member.mergeFields, "tags_count": len(tags), "tags": tags, "list_id": integrationListID,
		"timestamp_signup": "", "timestamp_opt": "", "last_changed": member.lastChanged.Format(time.RFC3339),
	}
}

func (member fakeMember) sortedTags() []string {
	var tags []string
	for tag := range member.tags {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags
}

func (provider *fakeMailchimp) seedMember(email string, status string, tags ...string) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	member := &fakeMember{email: email, status: status, mergeFields: map[string]any{}, tags: map[string]bool{},
		lastChanged: time.Date(2026, 1, 10, 9, 0, len(provider.members), 0, time.UTC)}
	for _, tag := range tags {
		member.tags[tag] = true
	}
	provider.members[mailchimp.SubscriberHash(email)] = member
}

func (provider *fakeMailchimp) record(name string, request *http.Request, body []byte) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.counts[name]++
	provider.requests[name] = append(provider.requests[name], fakeRecordedRequest{at: time.Now(), path: request.URL.Path, body: string(body)})
	return provider.counts[name]
}

func (provider *fakeMailchimp) writeProblem(response http.ResponseWriter, status int, title string) {
	provider.writeValue(response, status, map[string]any{
		"type": "https://mailchimp.com/developer/marketing/docs/errors/", "title": title, "status": status,
		"detail": "SENTINEL detail that can repeat a contact's address", "instance": "3b4dcb40-0b6b-4820-bfaa-41267b3826ea",
	})
}

func (provider *fakeMailchimp) writeValue(response http.ResponseWriter, status int, value any) {
	encoded, err := json.Marshal(value)
	require.NoError(provider.t, err)
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("X-Request-Id", "a1efb240-f8d8-40fe-a680-c3a5619a42e9")
	response.WriteHeader(status)
	if _, err := response.Write(encoded); err != nil {
		provider.t.Logf("fake Mailchimp response write failed: %v", err)
	}
}

func (provider *fakeMailchimp) waitForDelayedRequests(t *testing.T) {
	t.Helper()
	finished := make(chan struct{})
	go func() {
		provider.delayedRequests.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * time.Minute):
		t.Fatal("a delayed fake Mailchimp request did not finish")
	}
}

func (provider *fakeMailchimp) count(name string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.counts[name]
}

func (provider *fakeMailchimp) countForPath(name string, fragment string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	total := 0
	for _, request := range provider.requests[name] {
		if strings.Contains(request.path, fragment) {
			total++
		}
	}
	return total
}

func (provider *fakeMailchimp) totalRequests() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	total := 0
	for _, requests := range provider.requests {
		total += len(requests)
	}
	return total
}

func (provider *fakeMailchimp) paths(name string) []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var paths []string
	for _, request := range provider.requests[name] {
		paths = append(paths, request.path)
	}
	return paths
}

func (provider *fakeMailchimp) bodies(name string) []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var bodies []string
	for _, request := range provider.requests[name] {
		bodies = append(bodies, request.body)
	}
	return bodies
}

func (provider *fakeMailchimp) requestTimes(name string) []time.Time {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var times []time.Time
	for _, request := range provider.requests[name] {
		times = append(times, request.at)
	}
	return times
}

func (provider *fakeMailchimp) member(email string) fakeMember {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	member := provider.members[mailchimp.SubscriberHash(email)]
	require.NotNil(provider.t, member, email)
	copied := *member
	return copied
}

func (provider *fakeMailchimp) createdMemberCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.createdMembers
}

func (provider *fakeMailchimp) confirmationEmailCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.confirmationEmails
}

func (provider *fakeMailchimp) appliedSendCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.campaign.appliedSends
}

// launchHarness owns a real Worker and Client against the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
type launchHarness struct {
	flow          *Flow
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newLaunchHarness(t *testing.T, provider *fakeMailchimp, requestTimeout time.Duration) *launchHarness {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "mailchimp", Name: ConnectionName}
	providerClient, err := mailchimp.New(mailchimp.Config{},
		sdkgo.StaticCredentialProvider[mailchimp.Credentials]{reference: {APIKey: sdkgo.NewSecretString(integrationAPIKey)}},
		mailchimp.WithAPIBaseURL(provider.URL+"/3.0"), mailchimp.WithHTTPClient(&http.Client{Timeout: requestTimeout}),
	)
	require.NoError(t, err)
	connection, err := mailchimp.NewConnection(providerClient, reference)
	require.NoError(t, err)
	harness := &launchHarness{flow: NewFlow(connection), serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")}
	harness.registry, err = dex.NewRegistry([]dex.Flow{harness.flow})
	require.NoError(t, err)
	harness.cache, err = blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	harness.workerAddress = net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness.client, err = dex.NewClient(harness.registry, harness.cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress, WorkerTarget: &dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	harness.startWorker(t)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	return harness
}

func (harness *launchHarness) startWorker(t *testing.T) {
	t.Helper()
	worker, err := dex.NewWorker(harness.registry, harness.cache, dex.WorkerOptions{
		BindAddress: harness.workerAddress, FlowServiceAddress: harness.serverAddress, WorkerTarget: dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	harness.worker, harness.workerResult = worker, workerResult
}

// replaceWorker force-stops the Worker without draining its handlers, like a crash, then starts a new one.
func (harness *launchHarness) replaceWorker(t *testing.T) {
	t.Helper()
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	// A forced stop returns the expired context's error; losing the in-flight handler is the point.
	_ = harness.worker.Stop(expired)
	require.NoError(t, <-harness.workerResult)
	harness.startWorker(t)
}

func (harness *launchHarness) startLaunch(t *testing.T, scenario string, input Input) string {
	t.Helper()
	flowID := "mailchimp-campaign-launch-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(integrationContext(t), harness.flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err, "start Flow %s on the Dex Server at %s", flowID, harness.serverAddress)
	return flowID
}

// waitForFlow waits, across server long-poll caps, until the Flow closes.
func (harness *launchHarness) waitForFlow(t *testing.T, flowID string) dex.FlowResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	for {
		result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
		var longPollTimeout *dex.LongPollTimeoutError
		if errors.As(err, &longPollTimeout) {
			continue
		}
		require.NoError(t, err, "Flow %s did not close", flowID)
		return result
	}
}

func (harness *launchHarness) waitForFlowOutput(t *testing.T, flowID string) CampaignLaunch {
	t.Helper()
	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var record CampaignLaunch
	require.NoError(t, result.DecodeSingleOutput(&record))
	return record
}

func (harness *launchHarness) waitForPhase(t *testing.T, flowID string, phase string) CampaignLaunch {
	t.Helper()
	ctx := integrationContext(t)
	var record CampaignLaunch
	require.Eventually(t, func() bool {
		record = CampaignLaunch{}
		return harness.client.InvokeRPC(ctx, flowID, harness.flow.GetCampaignLaunch, nil, &record) == nil && record.Phase == phase
	}, 2*time.Minute, 100*time.Millisecond, "Flow %s last record %+v", flowID, record)
	return record
}

func integrationContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
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
