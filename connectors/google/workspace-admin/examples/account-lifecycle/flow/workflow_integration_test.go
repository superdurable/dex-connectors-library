//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accountlifecycle

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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	workspaceadmin "github.com/superdurable/dex-connectors-library/connectors/google/workspace-admin"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const integrationAccessToken = "workspace-admin-token"

func onboardingInput() Input {
	return Input{
		Action: AccountActionOnboard, PrimaryEmail: "ada.lovelace@example.com", GivenName: "Ada", FamilyName: "Lovelace",
		OrgUnitPath: "/Engineering", RecoveryEmail: "ada@personal.example", GroupEmail: "engineering@example.com",
	}
}

// Dex dispatches a backup attempt for an async Execute that runs past about seven seconds.
func TestOnboardingSlowAccountInsertBackupAttemptCreatesOneAccountWithRealDex(t *testing.T) {
	provider := newFakeDirectory(t)
	provider.seedGroup("engineering@example.com")
	provider.slowFirstUserInsertDelay = 9 * time.Second
	flow, harness := newAccountLifecycleHarness(t, provider.URL)
	ctx := integrationContext(t)

	flowID := startAccountLifecycle(t, ctx, harness.client, flow, "slow-user-insert", onboardingInput())
	handOff := waitForCompletedHandOff(t, ctx, harness.client, flowID)

	require.Equal(t, HandOffOnboarded, handOff.Status)
	require.Equal(t, 1, provider.userCount(), "every concurrent attempt converges on one account")
	require.Equal(t, 1, provider.memberCount("engineering@example.com"))
	require.LessOrEqual(t, provider.count("userInsert"), 2)
	require.Equal(t, provider.count("userInsert")-1, provider.count("userInsertConflict"), "a backup attempt hits 409 and reads the account back")
	requireNoPasswordInFlow(t, ctx, harness.client, flow, flowID, provider)
	t.Logf("slow user insert: inserts=%d conflicts=%d memberInserts=%d", provider.count("userInsert"), provider.count("userInsertConflict"), provider.count("memberInsert"))
}

func TestOnboardingSlowMembershipInsertBackupAttemptCreatesOneMembershipWithRealDex(t *testing.T) {
	provider := newFakeDirectory(t)
	provider.seedGroup("engineering@example.com")
	provider.slowFirstMemberInsertDelay = 9 * time.Second
	flow, harness := newAccountLifecycleHarness(t, provider.URL)
	ctx := integrationContext(t)

	flowID := startAccountLifecycle(t, ctx, harness.client, flow, "slow-member-insert", onboardingInput())
	handOff := waitForCompletedHandOff(t, ctx, harness.client, flowID)

	require.Equal(t, HandOffOnboarded, handOff.Status)
	require.Equal(t, workspaceadmin.GroupRoleMember, handOff.GroupRole)
	require.Equal(t, 1, provider.userCount())
	require.Equal(t, 1, provider.memberCount("engineering@example.com"), "every concurrent attempt converges on one membership")
	require.LessOrEqual(t, provider.count("memberInsert"), 2)
	requireNoPasswordInFlow(t, ctx, harness.client, flow, flowID, provider)
	t.Logf("slow member insert: memberInserts=%d memberConflicts=%d", provider.count("memberInsert"), provider.count("memberInsertConflict"))
}

func TestOnboardingSurvivesALostInsertResponseAndAccountPropagationWithRealDex(t *testing.T) {
	provider := newFakeDirectory(t)
	provider.seedGroup("engineering@example.com")
	provider.losesFirstUserInsertResponse = true
	provider.memberInsertsBeforePropagation = 2
	flow, harness := newAccountLifecycleHarness(t, provider.URL)
	ctx := integrationContext(t)

	flowID := startAccountLifecycle(t, ctx, harness.client, flow, "lost-response", onboardingInput())
	handOff := waitForCompletedHandOff(t, ctx, harness.client, flowID)

	require.Equal(t, HandOffOnboarded, handOff.Status)
	require.True(t, handOff.WasAlreadyCreated, "the retried Step found the account its first attempt created")
	require.Equal(t, provider.userID("ada.lovelace@example.com"), handOff.UserID)
	require.Equal(t, "/Engineering", handOff.OrgUnitPath)
	require.Equal(t, newAccountSignInHandOff, handOff.NextStep)
	require.Equal(t, 1, provider.userCount())
	require.Equal(t, 2, provider.count("userInsert"))
	require.Equal(t, 3, provider.count("memberInsert"), "two inserts raced account creation and were retried")
	require.Equal(t, 1, provider.memberCount("engineering@example.com"))
	insert := provider.firstUserInsert()
	require.Equal(t, true, insert["changePasswordAtNextLogin"])
	require.Equal(t, "ada@personal.example", insert["recoveryEmail"])
	requireNoPasswordInFlow(t, ctx, harness.client, flow, flowID, provider)
}

func TestOnboardingHandsOffAnAddressThatBelongsToAnotherAccountWithRealDex(t *testing.T) {
	provider := newFakeDirectory(t)
	provider.seedGroup("engineering@example.com")
	existingID := provider.seedUser("ada.lovelace@example.com", true)
	flow, harness := newAccountLifecycleHarness(t, provider.URL)
	ctx := integrationContext(t)

	flowID := startAccountLifecycle(t, ctx, harness.client, flow, "address-taken", onboardingInput())
	handOff := waitForCompletedHandOff(t, ctx, harness.client, flowID)

	require.Equal(t, HandOffAddressTaken, handOff.Status)
	require.Equal(t, existingID, handOff.UserID)
	require.True(t, handOff.IsSuspended)
	require.Equal(t, addressTakenHandOff, handOff.NextStep)
	require.Equal(t, 1, provider.userCount())
	require.Zero(t, provider.count("memberInsert"), "nothing is built for an address this Flow did not create")
}

func TestReinstatingRestoresTheExistingAccountWithoutCreatingOneWithRealDex(t *testing.T) {
	provider := newFakeDirectory(t)
	provider.seedGroup("engineering@example.com")
	existingID := provider.seedUser("ada.lovelace@example.com", true)
	provider.seedMember("engineering@example.com", "ada.lovelace@example.com", "OWNER")
	flow, harness := newAccountLifecycleHarness(t, provider.URL)
	ctx := integrationContext(t)

	flowID := startAccountLifecycle(t, ctx, harness.client, flow, "reinstate", Input{
		Action: AccountActionReinstate, PrimaryEmail: "ada.lovelace@example.com", GroupEmail: "engineering@example.com",
	})
	handOff := waitForCompletedHandOff(t, ctx, harness.client, flowID)

	require.Equal(t, HandOffReinstated, handOff.Status)
	require.Equal(t, existingID, handOff.UserID)
	require.False(t, handOff.IsSuspended)
	require.True(t, handOff.WasAlreadyMember)
	require.Equal(t, workspaceadmin.GroupRoleOwner, handOff.GroupRole, "an existing role is kept")
	require.Equal(t, reinstatedAccountSignInHandOff, handOff.NextStep)
	require.Zero(t, provider.count("userInsert"))
	require.Equal(t, 1, provider.count("userUpdate"))
	require.Equal(t, 1, provider.memberCount("engineering@example.com"))
}

func TestOffboardingSuspendsAndRemovesTheMembershipAfterALostResponseWithRealDex(t *testing.T) {
	provider := newFakeDirectory(t)
	provider.seedGroup("engineering@example.com")
	existingID := provider.seedUser("ada.lovelace@example.com", false)
	provider.seedMember("engineering@example.com", "ada.lovelace@example.com", "MEMBER")
	provider.losesFirstMemberDeleteResponse = true
	flow, harness := newAccountLifecycleHarness(t, provider.URL)
	ctx := integrationContext(t)

	flowID := startAccountLifecycle(t, ctx, harness.client, flow, "offboard", Input{
		Action: AccountActionOffboard, PrimaryEmail: "ada.lovelace@example.com", GroupEmail: "engineering@example.com",
	})
	handOff := waitForCompletedHandOff(t, ctx, harness.client, flowID)

	require.Equal(t, HandOffOffboarded, handOff.Status)
	require.Equal(t, existingID, handOff.UserID)
	require.True(t, handOff.IsSuspended)
	require.True(t, handOff.WasAlreadyRemoved, "the retried removal found its own earlier delete")
	require.Equal(t, offboardedAccountHandOff, handOff.NextStep)
	require.True(t, provider.isSuspended("ada.lovelace@example.com"))
	require.Zero(t, provider.memberCount("engineering@example.com"))
	require.Equal(t, 2, provider.count("memberDelete"))
	require.Equal(t, []string{"/admin/directory/v1/groups/engineering@example.com/members/" + existingID}, provider.memberDeletePaths()[:1],
		"offboarding removes the member by its unique ID")
}

func TestOffboardingAnUnknownAccountFailsOnTheUnwiredNotFoundBranchWithRealDex(t *testing.T) {
	provider := newFakeDirectory(t)
	provider.seedGroup("engineering@example.com")
	flow, harness := newAccountLifecycleHarness(t, provider.URL)
	ctx := integrationContext(t)

	flowID := startAccountLifecycle(t, ctx, harness.client, flow, "offboard-unknown", Input{
		Action: AccountActionOffboard, PrimaryEmail: "nobody@example.com", GroupEmail: "engineering@example.com",
	})
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{})
	require.NoError(t, err)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Equal(t, 1, provider.count("userUpdate"))
	require.Zero(t, provider.count("memberDelete"))
}

func TestAnInvalidRequestFailsBeforeAnyProviderRequestWithRealDex(t *testing.T) {
	provider := newFakeDirectory(t)
	flow, harness := newAccountLifecycleHarness(t, provider.URL)
	ctx := integrationContext(t)
	input := onboardingInput()
	input.FamilyName = ""

	flowID := startAccountLifecycle(t, ctx, harness.client, flow, "invalid", input)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{})
	require.NoError(t, err)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Zero(t, provider.count("any"))
}

func startAccountLifecycle(t *testing.T, ctx context.Context, client *dex.Client, flow *Flow, scenario string, input Input) string {
	t.Helper()
	flowID := "google-workspace-account-lifecycle-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	return flowID
}

func waitForCompletedHandOff(t *testing.T, ctx context.Context, client *dex.Client, flowID string) AccountHandOff {
	t.Helper()
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, "flow %s: %s", flowID, result.ErrorMessage)
	var handOff AccountHandOff
	require.NoError(t, result.DecodeSingleOutput(&handOff))
	return handOff
}

// requireNoPasswordInFlow reads every Attribute through the display RPC and checks that no generated password is present.
func requireNoPasswordInFlow(t *testing.T, ctx context.Context, client *dex.Client, flow *Flow, flowID string, provider *fakeDirectory) {
	t.Helper()
	var display map[string]any
	require.NoError(t, client.InvokeRPC(ctx, flowID, flow.GetDexDisplay, dex.None(nil), &display))
	encoded, err := json.Marshal(display)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "google-workspace-account-hand-off")
	passwords := provider.passwords()
	require.NotEmpty(t, passwords)
	for _, password := range passwords {
		require.Len(t, password, 43)
		require.NotContains(t, string(encoded), password)
	}
}

// fakeDirectory is a stateful Admin SDK Directory API fake keyed by lowercase address.
type fakeDirectory struct {
	*httptest.Server
	mutex                          sync.Mutex
	users                          map[string]map[string]any
	groups                         map[string]map[string]map[string]any
	nextID                         int
	counts                         map[string]int
	userInserts                    []map[string]any
	memberDeleteRequestPaths       []string
	slowFirstUserInsertDelay       time.Duration
	losesFirstUserInsertResponse   bool
	slowFirstMemberInsertDelay     time.Duration
	memberInsertsBeforePropagation int
	losesFirstMemberDeleteResponse bool
}

func newFakeDirectory(t *testing.T) *fakeDirectory {
	t.Helper()
	provider := &fakeDirectory{
		users: map[string]map[string]any{}, groups: map[string]map[string]map[string]any{}, nextID: 200000000000000000, counts: map[string]int{},
	}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeDirectory) serveHTTP(response http.ResponseWriter, request *http.Request) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.counts["any"]++
	if request.Header.Get("Authorization") != "Bearer "+integrationAccessToken {
		writeFakeJSON(response, http.StatusUnauthorized, fakeGoogleError(http.StatusUnauthorized, "authError", "Login Required."))
		return
	}
	var body map[string]any
	contents, err := io.ReadAll(request.Body)
	if err == nil && len(contents) != 0 {
		err = json.Unmarshal(contents, &body)
	}
	if err != nil {
		writeFakeJSON(response, http.StatusBadRequest, fakeGoogleError(http.StatusBadRequest, "badRequest", "Invalid JSON."))
		return
	}
	segments := strings.Split(strings.TrimPrefix(request.URL.Path, "/admin/directory/v1/"), "/")
	switch {
	case request.Method == http.MethodPost && request.URL.Path == "/admin/directory/v1/users":
		provider.insertUser(response, body)
	case len(segments) == 2 && segments[0] == "users" && request.Method == http.MethodGet:
		provider.counts["userGet"]++
		user := provider.findUser(segments[1])
		if user == nil {
			writeFakeJSON(response, http.StatusNotFound, fakeGoogleError(http.StatusNotFound, "notFound", "Resource Not Found: userKey"))
			return
		}
		writeFakeJSON(response, http.StatusOK, user)
	case len(segments) == 2 && segments[0] == "users" && request.Method == http.MethodPut:
		provider.counts["userUpdate"]++
		user := provider.findUser(segments[1])
		if user == nil {
			writeFakeJSON(response, http.StatusNotFound, fakeGoogleError(http.StatusNotFound, "notFound", "Resource Not Found: userKey"))
			return
		}
		user["suspended"] = body["suspended"]
		writeFakeJSON(response, http.StatusOK, user)
	case len(segments) >= 3 && segments[0] == "groups" && segments[2] == "members":
		provider.serveMembers(response, request, segments, body)
	default:
		writeFakeJSON(response, http.StatusNotFound, fakeGoogleError(http.StatusNotFound, "notFound", "Not Found"))
	}
}

func (provider *fakeDirectory) insertUser(response http.ResponseWriter, body map[string]any) {
	provider.counts["userInsert"]++
	provider.userInserts = append(provider.userInserts, body)
	email := strings.ToLower(body["primaryEmail"].(string))
	if _, exists := provider.users[email]; exists {
		provider.counts["userInsertConflict"]++
		writeFakeJSON(response, http.StatusConflict, fakeGoogleError(http.StatusConflict, "duplicate", "Entity already exists."))
		return
	}
	provider.nextID++
	user := map[string]any{
		"kind": "admin#directory#user", "id": strconv.Itoa(provider.nextID), "primaryEmail": body["primaryEmail"], "name": body["name"],
		"orgUnitPath": body["orgUnitPath"], "suspended": false, "changePasswordAtNextLogin": body["changePasswordAtNextLogin"],
		"externalIds": body["externalIds"], "customerId": "C03az79cb", "creationTime": "2026-10-01T09:00:00.000Z",
	}
	provider.users[email] = user
	isFirstInsert := provider.counts["userInsert"] == 1
	if provider.slowFirstUserInsertDelay > 0 && isFirstInsert {
		// Google has stored the account; the response arrives late while other requests proceed.
		provider.mutex.Unlock()
		time.Sleep(provider.slowFirstUserInsertDelay)
		provider.mutex.Lock()
	}
	if provider.losesFirstUserInsertResponse && isFirstInsert {
		writeFakeJSON(response, http.StatusServiceUnavailable, fakeGoogleError(http.StatusServiceUnavailable, "backendError", "Service unavailable. Please try again"))
		return
	}
	writeFakeJSON(response, http.StatusOK, user)
}

func (provider *fakeDirectory) serveMembers(response http.ResponseWriter, request *http.Request, segments []string, body map[string]any) {
	members, groupExists := provider.groups[strings.ToLower(segments[1])]
	if !groupExists {
		writeFakeJSON(response, http.StatusNotFound, fakeGoogleError(http.StatusNotFound, "notFound", "Resource Not Found: groupKey"))
		return
	}
	switch {
	case len(segments) == 3 && request.Method == http.MethodPost:
		provider.counts["memberInsert"]++
		if provider.counts["memberInsert"] <= provider.memberInsertsBeforePropagation {
			writeFakeJSON(response, http.StatusPreconditionFailed, fakeGoogleError(http.StatusPreconditionFailed, "conditionNotMet", "User creation is not complete."))
			return
		}
		email := strings.ToLower(body["email"].(string))
		user := provider.findUser(email)
		if user == nil {
			writeFakeJSON(response, http.StatusNotFound, fakeGoogleError(http.StatusNotFound, "notFound", "Resource Not Found: memberKey"))
			return
		}
		if _, exists := members[email]; exists {
			provider.counts["memberInsertConflict"]++
			writeFakeJSON(response, http.StatusConflict, fakeGoogleError(http.StatusConflict, "duplicate", "Member already exists."))
			return
		}
		member := map[string]any{"kind": "admin#directory#member", "id": user["id"], "email": user["primaryEmail"], "role": body["role"], "type": "USER", "status": "ACTIVE"}
		members[email] = member
		if provider.slowFirstMemberInsertDelay > 0 && provider.counts["memberInsert"] == 1 {
			provider.mutex.Unlock()
			time.Sleep(provider.slowFirstMemberInsertDelay)
			provider.mutex.Lock()
		}
		writeFakeJSON(response, http.StatusOK, member)
	case len(segments) == 3 && request.Method == http.MethodGet:
		provider.counts["groupCheck"]++
		writeFakeJSON(response, http.StatusOK, map[string]any{"kind": "admin#directory#members"})
	case len(segments) == 4 && request.Method == http.MethodGet:
		member, exists := members[provider.memberAddress(segments[3])]
		if !exists {
			writeFakeJSON(response, http.StatusNotFound, fakeGoogleError(http.StatusNotFound, "notFound", "Resource Not Found: memberKey"))
			return
		}
		writeFakeJSON(response, http.StatusOK, member)
	case len(segments) == 4 && request.Method == http.MethodDelete:
		provider.counts["memberDelete"]++
		provider.memberDeleteRequestPaths = append(provider.memberDeleteRequestPaths, request.URL.Path)
		address := provider.memberAddress(segments[3])
		if _, exists := members[address]; !exists {
			writeFakeJSON(response, http.StatusNotFound, fakeGoogleError(http.StatusNotFound, "notFound", "Resource Not Found: memberKey"))
			return
		}
		delete(members, address)
		if provider.losesFirstMemberDeleteResponse && provider.counts["memberDelete"] == 1 {
			writeFakeJSON(response, http.StatusServiceUnavailable, fakeGoogleError(http.StatusServiceUnavailable, "backendError", "Service unavailable. Please try again"))
			return
		}
		response.WriteHeader(http.StatusNoContent)
	default:
		writeFakeJSON(response, http.StatusNotFound, fakeGoogleError(http.StatusNotFound, "notFound", "Not Found"))
	}
}

// memberAddress resolves a member key, which may be an address or a unique user ID, to the stored address.
func (provider *fakeDirectory) memberAddress(memberKey string) string {
	if user := provider.findUser(memberKey); user != nil {
		return strings.ToLower(user["primaryEmail"].(string))
	}
	return strings.ToLower(memberKey)
}

func (provider *fakeDirectory) findUser(userKey string) map[string]any {
	if user, exists := provider.users[strings.ToLower(userKey)]; exists {
		return user
	}
	for _, user := range provider.users {
		if user["id"] == userKey {
			return user
		}
	}
	return nil
}

func (provider *fakeDirectory) seedUser(email string, isSuspended bool) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.nextID++
	userID := strconv.Itoa(provider.nextID)
	provider.users[strings.ToLower(email)] = map[string]any{
		"id": userID, "primaryEmail": email, "name": map[string]any{"givenName": "Ada", "familyName": "Lovelace"},
		"orgUnitPath": "/Alumni", "suspended": isSuspended, "suspensionReason": "ADMIN",
	}
	return userID
}

func (provider *fakeDirectory) seedGroup(email string) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.groups[strings.ToLower(email)] = map[string]map[string]any{}
}

func (provider *fakeDirectory) seedMember(groupEmail string, memberEmail string, role string) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	user := provider.findUser(memberEmail)
	provider.groups[strings.ToLower(groupEmail)][strings.ToLower(memberEmail)] = map[string]any{
		"id": user["id"], "email": memberEmail, "role": role, "type": "USER", "status": "ACTIVE",
	}
}

func (provider *fakeDirectory) count(name string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.counts[name]
}

func (provider *fakeDirectory) userCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.users)
}

func (provider *fakeDirectory) memberCount(groupEmail string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.groups[strings.ToLower(groupEmail)])
}

func (provider *fakeDirectory) userID(email string) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.users[strings.ToLower(email)]["id"].(string)
}

func (provider *fakeDirectory) isSuspended(email string) bool {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.users[strings.ToLower(email)]["suspended"] == true
}

func (provider *fakeDirectory) firstUserInsert() map[string]any {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.userInserts[0]
}

func (provider *fakeDirectory) passwords() []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	passwords := []string{}
	for _, insert := range provider.userInserts {
		passwords = append(passwords, insert["password"].(string))
	}
	return passwords
}

func (provider *fakeDirectory) memberDeletePaths() []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]string(nil), provider.memberDeleteRequestPaths...)
}

func fakeGoogleError(code int, reason string, message string) map[string]any {
	return map[string]any{"error": map[string]any{
		"code": code, "message": message,
		"errors": []any{map[string]any{"domain": "global", "reason": reason, "message": message}},
	}}
}

func writeFakeJSON(response http.ResponseWriter, status int, body any) {
	contents, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if _, err := response.Write(contents); err != nil {
		panic(err)
	}
}

type accountLifecycleHarness struct {
	cache        *blobcache.Cache
	worker       *dex.Worker
	workerResult chan error
	client       *dex.Client
}

func newAccountLifecycleHarness(t *testing.T, endpoint string) (*Flow, *accountLifecycleHarness) {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "google", Name: ConnectionName}
	providerClient, err := workspaceadmin.New(workspaceadmin.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[workspaceadmin.Credentials]{
		reference: {AuthMethodID: workspaceadmin.WorkspaceDomainDelegationAuthMethodID, AccessToken: sdkgo.NewSecretString(integrationAccessToken)},
	})
	require.NoError(t, err)
	connection, err := workspaceadmin.NewConnection(providerClient, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
	workerAddress := net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness := &accountLifecycleHarness{cache: cache}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	harness.worker, err = dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: workerAddress, FlowServiceAddress: serverAddress, WorkerTarget: dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	harness.workerResult = make(chan error, 1)
	go func() { harness.workerResult <- harness.worker.Start() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	return flow, harness
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
