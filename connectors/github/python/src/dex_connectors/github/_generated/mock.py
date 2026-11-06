# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AuthenticatedProfile, CommitPage, CommitSummary, GetAuthenticatedProfileInput, ListCommitsInput, ListMergedPullRequestsInput, ListPublicRepositoriesInput, ListPullRequestFilesInput, ListReleasesInput, MergedPullRequest, MergedPullRequestPage, PublicRepositories, PullRequestFile, PullRequestFilePage, Release, ReleasePage, Repository
from .definitions import CONNECTOR_ID, AUTHORIZATION_REVOKED, DEFECT, GET_AUTHENTICATED_PROFILE_DEFINITION, INSUFFICIENT_SCOPE, INVALID_RESPONSE, LISTED, LIST_COMMITS_DEFINITION, LIST_MERGED_PULL_REQUESTS_DEFINITION, LIST_PUBLIC_REPOSITORIES_DEFINITION, LIST_PULL_REQUEST_FILES_DEFINITION, LIST_RELEASES_DEFINITION, NOT_FOUND, PROFILE_LOADED, PROVIDER_REJECTED, REPOSITORIES_LOADED, VERIFIED_EMAIL_REQUIRED

def get_authenticated_profile_profile_loaded(value: AuthenticatedProfile, failure: Failure | None = None) -> Case[AuthenticatedProfile]:
    return Case.branch_result(PROFILE_LOADED, value, failure)

setattr(get_authenticated_profile_profile_loaded, '__module__', "dex_connectors.github.githubmock")

def get_authenticated_profile_verified_email_required(value: AuthenticatedProfile, failure: Failure | None = None) -> Case[AuthenticatedProfile]:
    return Case.branch_result(VERIFIED_EMAIL_REQUIRED, value, failure)

setattr(get_authenticated_profile_verified_email_required, '__module__', "dex_connectors.github.githubmock")

def get_authenticated_profile_insufficient_scope(value: AuthenticatedProfile, failure: Failure | None = None) -> Case[AuthenticatedProfile]:
    return Case.branch_result(INSUFFICIENT_SCOPE, value, failure)

setattr(get_authenticated_profile_insufficient_scope, '__module__', "dex_connectors.github.githubmock")

def get_authenticated_profile_authorization_revoked(value: AuthenticatedProfile, failure: Failure | None = None) -> Case[AuthenticatedProfile]:
    return Case.branch_result(AUTHORIZATION_REVOKED, value, failure)

setattr(get_authenticated_profile_authorization_revoked, '__module__', "dex_connectors.github.githubmock")

def get_authenticated_profile_not_found(value: AuthenticatedProfile, failure: Failure | None = None) -> Case[AuthenticatedProfile]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_authenticated_profile_not_found, '__module__', "dex_connectors.github.githubmock")

def get_authenticated_profile_provider_rejected(value: AuthenticatedProfile, failure: Failure | None = None) -> Case[AuthenticatedProfile]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_authenticated_profile_provider_rejected, '__module__', "dex_connectors.github.githubmock")

def get_authenticated_profile_invalid_response(value: AuthenticatedProfile, failure: Failure | None = None) -> Case[AuthenticatedProfile]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_authenticated_profile_invalid_response, '__module__', "dex_connectors.github.githubmock")

def get_authenticated_profile_defect(value: AuthenticatedProfile, failure: Failure | None = None) -> Case[AuthenticatedProfile]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_authenticated_profile_defect, '__module__', "dex_connectors.github.githubmock")

def get_authenticated_profile_retry(failure: Failure, after_seconds: float = 0) -> Case[AuthenticatedProfile]:
    return Case.retry(failure, after_seconds)

setattr(get_authenticated_profile_retry, '__module__', "dex_connectors.github.githubmock")

def get_authenticated_profile_octocat_profile() -> Case[AuthenticatedProfile]:
    return Case.branch_result(PROFILE_LOADED, AuthenticatedProfile(subject='583231', login='octocat', name='The Octocat', verified_email='octocat@example.com', avatar_url='https://avatars.example.com/u/583231', profile_url='https://github.com/octocat', company='@github', blog_url='https://github.blog', location='San Francisco', bio='Mascot', twitter_username='github', public_repos=8, followers=9000, following=9, created_at=datetime.fromisoformat("2011-01-25T18:44:36Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-02-01T10:00:00Z".replace('Z', '+00:00'))))

setattr(get_authenticated_profile_octocat_profile, '__module__', "dex_connectors.github.githubmock")

def get_authenticated_profile_rate_limited() -> Case[AuthenticatedProfile]:
    return Case.retry(Failure(FailureKind.RATE_LIMIT, CONNECTOR_ID, "getAuthenticatedProfile", "GitHub rate limit was reached."), 1)

setattr(get_authenticated_profile_rate_limited, '__module__', "dex_connectors.github.githubmock")

def get_authenticated_profile_verified_email_required_failure() -> Case[AuthenticatedProfile]:
    return Case.branch_result(VERIFIED_EMAIL_REQUIRED, AuthenticatedProfile(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "getAuthenticatedProfile", "A primary verified email was not available."))

setattr(get_authenticated_profile_verified_email_required_failure, '__module__', "dex_connectors.github.githubmock")

def get_authenticated_profile_insufficient_scope_failure() -> Case[AuthenticatedProfile]:
    return Case.branch_result(INSUFFICIENT_SCOPE, AuthenticatedProfile(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getAuthenticatedProfile", "The OAuth grant does not match the required scopes."))

setattr(get_authenticated_profile_insufficient_scope_failure, '__module__', "dex_connectors.github.githubmock")

def get_authenticated_profile_authorization_revoked_failure() -> Case[AuthenticatedProfile]:
    return Case.branch_result(AUTHORIZATION_REVOKED, AuthenticatedProfile(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "getAuthenticatedProfile", "The OAuth grant is invalid or revoked."))

setattr(get_authenticated_profile_authorization_revoked_failure, '__module__', "dex_connectors.github.githubmock")

def get_authenticated_profile_not_found_failure() -> Case[AuthenticatedProfile]:
    return Case.branch_result(NOT_FOUND, AuthenticatedProfile(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getAuthenticatedProfile", "The authenticated account was not found."))

setattr(get_authenticated_profile_not_found_failure, '__module__', "dex_connectors.github.githubmock")

def get_authenticated_profile_provider_rejected_failure() -> Case[AuthenticatedProfile]:
    return Case.branch_result(PROVIDER_REJECTED, AuthenticatedProfile(), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "getAuthenticatedProfile", "GitHub conclusively rejected the profile query."))

setattr(get_authenticated_profile_provider_rejected_failure, '__module__', "dex_connectors.github.githubmock")

def get_authenticated_profile_invalid_response_failure() -> Case[AuthenticatedProfile]:
    return Case.branch_result(INVALID_RESPONSE, AuthenticatedProfile(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "getAuthenticatedProfile", "GitHub returned an invalid or oversized profile response."))

setattr(get_authenticated_profile_invalid_response_failure, '__module__', "dex_connectors.github.githubmock")

def get_authenticated_profile_defect_failure() -> Case[AuthenticatedProfile]:
    return Case.branch_result(DEFECT, AuthenticatedProfile(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "getAuthenticatedProfile", "Local input, connection configuration, or connector definition is invalid."))

setattr(get_authenticated_profile_defect_failure, '__module__', "dex_connectors.github.githubmock")

def list_public_repositories_repositories_loaded(value: PublicRepositories, failure: Failure | None = None) -> Case[PublicRepositories]:
    return Case.branch_result(REPOSITORIES_LOADED, value, failure)

setattr(list_public_repositories_repositories_loaded, '__module__', "dex_connectors.github.githubmock")

def list_public_repositories_insufficient_scope(value: PublicRepositories, failure: Failure | None = None) -> Case[PublicRepositories]:
    return Case.branch_result(INSUFFICIENT_SCOPE, value, failure)

setattr(list_public_repositories_insufficient_scope, '__module__', "dex_connectors.github.githubmock")

def list_public_repositories_authorization_revoked(value: PublicRepositories, failure: Failure | None = None) -> Case[PublicRepositories]:
    return Case.branch_result(AUTHORIZATION_REVOKED, value, failure)

setattr(list_public_repositories_authorization_revoked, '__module__', "dex_connectors.github.githubmock")

def list_public_repositories_not_found(value: PublicRepositories, failure: Failure | None = None) -> Case[PublicRepositories]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_public_repositories_not_found, '__module__', "dex_connectors.github.githubmock")

def list_public_repositories_provider_rejected(value: PublicRepositories, failure: Failure | None = None) -> Case[PublicRepositories]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_public_repositories_provider_rejected, '__module__', "dex_connectors.github.githubmock")

def list_public_repositories_invalid_response(value: PublicRepositories, failure: Failure | None = None) -> Case[PublicRepositories]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_public_repositories_invalid_response, '__module__', "dex_connectors.github.githubmock")

def list_public_repositories_defect(value: PublicRepositories, failure: Failure | None = None) -> Case[PublicRepositories]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_public_repositories_defect, '__module__', "dex_connectors.github.githubmock")

def list_public_repositories_retry(failure: Failure, after_seconds: float = 0) -> Case[PublicRepositories]:
    return Case.retry(failure, after_seconds)

setattr(list_public_repositories_retry, '__module__', "dex_connectors.github.githubmock")

def list_public_repositories_mixed_repositories() -> Case[PublicRepositories]:
    return Case.branch_result(REPOSITORIES_LOADED, PublicRepositories(repositories=(Repository(id=1296269, name='hello-world', full_name='octocat/hello-world', description='My first repository.', profile_url='https://github.com/octocat/hello-world', language='Go', topics=('example', 'dex'), fork=False, archived=False, disabled=False, stargazers_count=80, forks_count=9, open_issues_count=2, pushed_at=datetime.fromisoformat("2026-03-01T12:00:00Z".replace('Z', '+00:00'))), Repository(id=1300192, name='spoon-knife', full_name='octocat/spoon-knife', fork=True, archived=False, disabled=False, stargazers_count=12, forks_count=0, open_issues_count=0, pushed_at=datetime.fromisoformat("2026-02-20T08:30:00Z".replace('Z', '+00:00'))), Repository(id=1400300, name='old-site', full_name='octocat/old-site', fork=False, archived=True, disabled=False, stargazers_count=3, forks_count=1, open_issues_count=0, pushed_at=datetime.fromisoformat("2019-05-04T00:00:00Z".replace('Z', '+00:00'))), Repository(id=1500400, name='suspended', full_name='octocat/suspended', fork=False, archived=False, disabled=True, stargazers_count=0, forks_count=0, open_issues_count=0)), truncated=True))

setattr(list_public_repositories_mixed_repositories, '__module__', "dex_connectors.github.githubmock")

def list_public_repositories_no_repositories() -> Case[PublicRepositories]:
    return Case.branch_result(REPOSITORIES_LOADED, PublicRepositories(repositories=(), truncated=False))

setattr(list_public_repositories_no_repositories, '__module__', "dex_connectors.github.githubmock")

def list_public_repositories_rate_limited() -> Case[PublicRepositories]:
    return Case.retry(Failure(FailureKind.RATE_LIMIT, CONNECTOR_ID, "listPublicRepositories", "GitHub rate limit was reached."), 1)

setattr(list_public_repositories_rate_limited, '__module__', "dex_connectors.github.githubmock")

def list_public_repositories_insufficient_scope_failure() -> Case[PublicRepositories]:
    return Case.branch_result(INSUFFICIENT_SCOPE, PublicRepositories(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "listPublicRepositories", "The OAuth grant does not match the required scopes."))

setattr(list_public_repositories_insufficient_scope_failure, '__module__', "dex_connectors.github.githubmock")

def list_public_repositories_authorization_revoked_failure() -> Case[PublicRepositories]:
    return Case.branch_result(AUTHORIZATION_REVOKED, PublicRepositories(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "listPublicRepositories", "The OAuth grant is invalid or revoked."))

setattr(list_public_repositories_authorization_revoked_failure, '__module__', "dex_connectors.github.githubmock")

def list_public_repositories_not_found_failure() -> Case[PublicRepositories]:
    return Case.branch_result(NOT_FOUND, PublicRepositories(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "listPublicRepositories", "The requested GitHub account was not found."))

setattr(list_public_repositories_not_found_failure, '__module__', "dex_connectors.github.githubmock")

def list_public_repositories_provider_rejected_failure() -> Case[PublicRepositories]:
    return Case.branch_result(PROVIDER_REJECTED, PublicRepositories(), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "listPublicRepositories", "GitHub conclusively rejected the repository query."))

setattr(list_public_repositories_provider_rejected_failure, '__module__', "dex_connectors.github.githubmock")

def list_public_repositories_invalid_response_failure() -> Case[PublicRepositories]:
    return Case.branch_result(INVALID_RESPONSE, PublicRepositories(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "listPublicRepositories", "GitHub returned an invalid or oversized repository response."))

setattr(list_public_repositories_invalid_response_failure, '__module__', "dex_connectors.github.githubmock")

def list_public_repositories_defect_failure() -> Case[PublicRepositories]:
    return Case.branch_result(DEFECT, PublicRepositories(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "listPublicRepositories", "Local input, connection configuration, or connector definition is invalid."))

setattr(list_public_repositories_defect_failure, '__module__', "dex_connectors.github.githubmock")

def list_merged_pull_requests_listed(value: MergedPullRequestPage, failure: Failure | None = None) -> Case[MergedPullRequestPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_merged_pull_requests_listed, '__module__', "dex_connectors.github.githubmock")

def list_merged_pull_requests_insufficient_scope(value: MergedPullRequestPage, failure: Failure | None = None) -> Case[MergedPullRequestPage]:
    return Case.branch_result(INSUFFICIENT_SCOPE, value, failure)

setattr(list_merged_pull_requests_insufficient_scope, '__module__', "dex_connectors.github.githubmock")

def list_merged_pull_requests_authorization_revoked(value: MergedPullRequestPage, failure: Failure | None = None) -> Case[MergedPullRequestPage]:
    return Case.branch_result(AUTHORIZATION_REVOKED, value, failure)

setattr(list_merged_pull_requests_authorization_revoked, '__module__', "dex_connectors.github.githubmock")

def list_merged_pull_requests_not_found(value: MergedPullRequestPage, failure: Failure | None = None) -> Case[MergedPullRequestPage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_merged_pull_requests_not_found, '__module__', "dex_connectors.github.githubmock")

def list_merged_pull_requests_provider_rejected(value: MergedPullRequestPage, failure: Failure | None = None) -> Case[MergedPullRequestPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_merged_pull_requests_provider_rejected, '__module__', "dex_connectors.github.githubmock")

def list_merged_pull_requests_invalid_response(value: MergedPullRequestPage, failure: Failure | None = None) -> Case[MergedPullRequestPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_merged_pull_requests_invalid_response, '__module__', "dex_connectors.github.githubmock")

def list_merged_pull_requests_defect(value: MergedPullRequestPage, failure: Failure | None = None) -> Case[MergedPullRequestPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_merged_pull_requests_defect, '__module__', "dex_connectors.github.githubmock")

def list_merged_pull_requests_retry(failure: Failure, after_seconds: float = 0) -> Case[MergedPullRequestPage]:
    return Case.retry(failure, after_seconds)

setattr(list_merged_pull_requests_retry, '__module__', "dex_connectors.github.githubmock")

def list_merged_pull_requests_three_unordered_pages() -> Case[MergedPullRequestPage]:
    return Case.paged(LISTED, (MergedPullRequestPage(pull_requests=(MergedPullRequest(number=42, title='Add release notes', body_truncated=False, url='https://github.com/octocat/hello-world/pull/42', author_login='octocat', merged_at=datetime.fromisoformat("2026-03-03T10:00:00Z".replace('Z', '+00:00')), labels=('docs',)), MergedPullRequest(number=40, title='Fix flaky test', body='The retry waited too long.', body_truncated=False, author_login='hubot', merged_at=datetime.fromisoformat("2026-03-01T00:00:00Z".replace('Z', '+00:00')))), total_count=5, incomplete_results=False, next_page=2), MergedPullRequestPage(pull_requests=(MergedPullRequest(number=40, title='Fix flaky test', body='The retry waited too long.', body_truncated=False, author_login='hubot', merged_at=datetime.fromisoformat("2026-03-01T00:00:00Z".replace('Z', '+00:00'))), MergedPullRequest(number=45, title='Bump dependencies', body_truncated=False, author_login='dependabot', merged_at=datetime.fromisoformat("2026-03-07T23:59:59Z".replace('Z', '+00:00')), labels=('dependencies',))), total_count=5, incomplete_results=False, next_page=3), MergedPullRequestPage(pull_requests=(MergedPullRequest(number=38, title='Rewrite the README', body='A long body', body_truncated=True, author_login='octocat', merged_at=datetime.fromisoformat("2026-03-05T16:20:00Z".replace('Z', '+00:00'))),), total_count=5, incomplete_results=False, next_page=0)))

setattr(list_merged_pull_requests_three_unordered_pages, '__module__', "dex_connectors.github.githubmock")

def list_merged_pull_requests_no_merges() -> Case[MergedPullRequestPage]:
    return Case.branch_result(LISTED, MergedPullRequestPage(pull_requests=(), total_count=0, incomplete_results=False, next_page=0))

setattr(list_merged_pull_requests_no_merges, '__module__', "dex_connectors.github.githubmock")

def list_merged_pull_requests_incomplete_search() -> Case[MergedPullRequestPage]:
    return Case.branch_result(LISTED, MergedPullRequestPage(pull_requests=(MergedPullRequest(number=42, title='Add release notes', body_truncated=False, merged_at=datetime.fromisoformat("2026-03-03T10:00:00Z".replace('Z', '+00:00'))),), total_count=7, incomplete_results=True, next_page=0))

setattr(list_merged_pull_requests_incomplete_search, '__module__', "dex_connectors.github.githubmock")

def list_merged_pull_requests_rate_limited() -> Case[MergedPullRequestPage]:
    return Case.retry(Failure(FailureKind.RATE_LIMIT, CONNECTOR_ID, "listMergedPullRequests", "GitHub search rate limit was reached."), 30)

setattr(list_merged_pull_requests_rate_limited, '__module__', "dex_connectors.github.githubmock")

def list_merged_pull_requests_insufficient_scope_failure() -> Case[MergedPullRequestPage]:
    return Case.branch_result(INSUFFICIENT_SCOPE, MergedPullRequestPage(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "listMergedPullRequests", "The OAuth grant does not match the required scopes."))

setattr(list_merged_pull_requests_insufficient_scope_failure, '__module__', "dex_connectors.github.githubmock")

def list_merged_pull_requests_authorization_revoked_failure() -> Case[MergedPullRequestPage]:
    return Case.branch_result(AUTHORIZATION_REVOKED, MergedPullRequestPage(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "listMergedPullRequests", "The OAuth grant is invalid or revoked."))

setattr(list_merged_pull_requests_authorization_revoked_failure, '__module__', "dex_connectors.github.githubmock")

def list_merged_pull_requests_not_found_failure() -> Case[MergedPullRequestPage]:
    return Case.branch_result(NOT_FOUND, MergedPullRequestPage(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "listMergedPullRequests", "The requested GitHub resource was not found."))

setattr(list_merged_pull_requests_not_found_failure, '__module__', "dex_connectors.github.githubmock")

def list_merged_pull_requests_provider_rejected_failure() -> Case[MergedPullRequestPage]:
    return Case.branch_result(PROVIDER_REJECTED, MergedPullRequestPage(), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "listMergedPullRequests", "GitHub conclusively rejected the search, including a repository it cannot search."))

setattr(list_merged_pull_requests_provider_rejected_failure, '__module__', "dex_connectors.github.githubmock")

def list_merged_pull_requests_invalid_response_failure() -> Case[MergedPullRequestPage]:
    return Case.branch_result(INVALID_RESPONSE, MergedPullRequestPage(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "listMergedPullRequests", "GitHub returned an invalid or oversized search response."))

setattr(list_merged_pull_requests_invalid_response_failure, '__module__', "dex_connectors.github.githubmock")

def list_merged_pull_requests_defect_failure() -> Case[MergedPullRequestPage]:
    return Case.branch_result(DEFECT, MergedPullRequestPage(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "listMergedPullRequests", "Local input, connection configuration, or connector definition is invalid."))

setattr(list_merged_pull_requests_defect_failure, '__module__', "dex_connectors.github.githubmock")

def list_pull_request_files_listed(value: PullRequestFilePage, failure: Failure | None = None) -> Case[PullRequestFilePage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_pull_request_files_listed, '__module__', "dex_connectors.github.githubmock")

def list_pull_request_files_insufficient_scope(value: PullRequestFilePage, failure: Failure | None = None) -> Case[PullRequestFilePage]:
    return Case.branch_result(INSUFFICIENT_SCOPE, value, failure)

setattr(list_pull_request_files_insufficient_scope, '__module__', "dex_connectors.github.githubmock")

def list_pull_request_files_authorization_revoked(value: PullRequestFilePage, failure: Failure | None = None) -> Case[PullRequestFilePage]:
    return Case.branch_result(AUTHORIZATION_REVOKED, value, failure)

setattr(list_pull_request_files_authorization_revoked, '__module__', "dex_connectors.github.githubmock")

def list_pull_request_files_not_found(value: PullRequestFilePage, failure: Failure | None = None) -> Case[PullRequestFilePage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_pull_request_files_not_found, '__module__', "dex_connectors.github.githubmock")

def list_pull_request_files_provider_rejected(value: PullRequestFilePage, failure: Failure | None = None) -> Case[PullRequestFilePage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_pull_request_files_provider_rejected, '__module__', "dex_connectors.github.githubmock")

def list_pull_request_files_invalid_response(value: PullRequestFilePage, failure: Failure | None = None) -> Case[PullRequestFilePage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_pull_request_files_invalid_response, '__module__', "dex_connectors.github.githubmock")

def list_pull_request_files_defect(value: PullRequestFilePage, failure: Failure | None = None) -> Case[PullRequestFilePage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_pull_request_files_defect, '__module__', "dex_connectors.github.githubmock")

def list_pull_request_files_retry(failure: Failure, after_seconds: float = 0) -> Case[PullRequestFilePage]:
    return Case.retry(failure, after_seconds)

setattr(list_pull_request_files_retry, '__module__', "dex_connectors.github.githubmock")

def list_pull_request_files_three_pages_of_changes() -> Case[PullRequestFilePage]:
    return Case.paged(LISTED, (PullRequestFilePage(files=(PullRequestFile(filename='README.md', status='modified', additions=3, deletions=1, changes=4, patch='@@ -1 +1,3 @@', patch_truncated=False), PullRequestFile(filename='docs/guide.md', previous_filename='docs/old-guide.md', status='renamed', additions=0, deletions=0, changes=0, patch_truncated=False)), next_page=2), PullRequestFilePage(files=(PullRequestFile(filename='logo.png', status='added', additions=0, deletions=0, changes=0, patch_truncated=False), PullRequestFile(filename='main.go', status='modified', additions=400, deletions=120, changes=520, patch='@@ -1,120 +1,400 @@', patch_truncated=True)), next_page=3), PullRequestFilePage(files=(PullRequestFile(filename='legacy.go', status='removed', additions=0, deletions=80, changes=80, patch_truncated=False),), next_page=0)))

setattr(list_pull_request_files_three_pages_of_changes, '__module__', "dex_connectors.github.githubmock")

def list_pull_request_files_rate_limited() -> Case[PullRequestFilePage]:
    return Case.retry(Failure(FailureKind.RATE_LIMIT, CONNECTOR_ID, "listPullRequestFiles", "GitHub rate limit was reached."), 1)

setattr(list_pull_request_files_rate_limited, '__module__', "dex_connectors.github.githubmock")

def list_pull_request_files_insufficient_scope_failure() -> Case[PullRequestFilePage]:
    return Case.branch_result(INSUFFICIENT_SCOPE, PullRequestFilePage(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "listPullRequestFiles", "The OAuth grant does not match the required scopes."))

setattr(list_pull_request_files_insufficient_scope_failure, '__module__', "dex_connectors.github.githubmock")

def list_pull_request_files_authorization_revoked_failure() -> Case[PullRequestFilePage]:
    return Case.branch_result(AUTHORIZATION_REVOKED, PullRequestFilePage(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "listPullRequestFiles", "The OAuth grant is invalid or revoked."))

setattr(list_pull_request_files_authorization_revoked_failure, '__module__', "dex_connectors.github.githubmock")

def list_pull_request_files_not_found_failure() -> Case[PullRequestFilePage]:
    return Case.branch_result(NOT_FOUND, PullRequestFilePage(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "listPullRequestFiles", "The repository or pull request was not found."))

setattr(list_pull_request_files_not_found_failure, '__module__', "dex_connectors.github.githubmock")

def list_pull_request_files_provider_rejected_failure() -> Case[PullRequestFilePage]:
    return Case.branch_result(PROVIDER_REJECTED, PullRequestFilePage(), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "listPullRequestFiles", "GitHub conclusively rejected the changed-file query."))

setattr(list_pull_request_files_provider_rejected_failure, '__module__', "dex_connectors.github.githubmock")

def list_pull_request_files_invalid_response_failure() -> Case[PullRequestFilePage]:
    return Case.branch_result(INVALID_RESPONSE, PullRequestFilePage(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "listPullRequestFiles", "GitHub returned an invalid or oversized changed-file response."))

setattr(list_pull_request_files_invalid_response_failure, '__module__', "dex_connectors.github.githubmock")

def list_pull_request_files_defect_failure() -> Case[PullRequestFilePage]:
    return Case.branch_result(DEFECT, PullRequestFilePage(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "listPullRequestFiles", "Local input, connection configuration, or connector definition is invalid."))

setattr(list_pull_request_files_defect_failure, '__module__', "dex_connectors.github.githubmock")

def list_releases_listed(value: ReleasePage, failure: Failure | None = None) -> Case[ReleasePage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_releases_listed, '__module__', "dex_connectors.github.githubmock")

def list_releases_insufficient_scope(value: ReleasePage, failure: Failure | None = None) -> Case[ReleasePage]:
    return Case.branch_result(INSUFFICIENT_SCOPE, value, failure)

setattr(list_releases_insufficient_scope, '__module__', "dex_connectors.github.githubmock")

def list_releases_authorization_revoked(value: ReleasePage, failure: Failure | None = None) -> Case[ReleasePage]:
    return Case.branch_result(AUTHORIZATION_REVOKED, value, failure)

setattr(list_releases_authorization_revoked, '__module__', "dex_connectors.github.githubmock")

def list_releases_not_found(value: ReleasePage, failure: Failure | None = None) -> Case[ReleasePage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_releases_not_found, '__module__', "dex_connectors.github.githubmock")

def list_releases_provider_rejected(value: ReleasePage, failure: Failure | None = None) -> Case[ReleasePage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_releases_provider_rejected, '__module__', "dex_connectors.github.githubmock")

def list_releases_invalid_response(value: ReleasePage, failure: Failure | None = None) -> Case[ReleasePage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_releases_invalid_response, '__module__', "dex_connectors.github.githubmock")

def list_releases_defect(value: ReleasePage, failure: Failure | None = None) -> Case[ReleasePage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_releases_defect, '__module__', "dex_connectors.github.githubmock")

def list_releases_retry(failure: Failure, after_seconds: float = 0) -> Case[ReleasePage]:
    return Case.retry(failure, after_seconds)

setattr(list_releases_retry, '__module__', "dex_connectors.github.githubmock")

def list_releases_three_unordered_pages() -> Case[ReleasePage]:
    return Case.paged(LISTED, (ReleasePage(releases=(Release(id=300, tag_name='v1.3.0', name='v1.3.0', url='https://github.com/octocat/hello-world/releases/tag/v1.3.0', body='Faster startup.', body_truncated=False, is_draft=False, is_prerelease=False, created_at=datetime.fromisoformat("2026-03-05T09:00:00Z".replace('Z', '+00:00')), published_at=datetime.fromisoformat("2026-03-05T09:30:00Z".replace('Z', '+00:00'))), Release(id=310, tag_name='v1.4.0-rc.1', body_truncated=False, is_draft=False, is_prerelease=True, created_at=datetime.fromisoformat("2026-03-06T09:00:00Z".replace('Z', '+00:00')), published_at=datetime.fromisoformat("2026-03-06T09:05:00Z".replace('Z', '+00:00')))), next_page=2), ReleasePage(releases=(Release(id=310, tag_name='v1.4.0-rc.1', body_truncated=False, is_draft=False, is_prerelease=True, created_at=datetime.fromisoformat("2026-03-06T09:00:00Z".replace('Z', '+00:00')), published_at=datetime.fromisoformat("2026-03-06T09:05:00Z".replace('Z', '+00:00'))), Release(id=320, tag_name='v1.4.0', body_truncated=False, is_draft=True, is_prerelease=False, created_at=datetime.fromisoformat("2026-03-07T09:00:00Z".replace('Z', '+00:00')))), next_page=3), ReleasePage(releases=(Release(id=200, tag_name='v1.2.0', name='v1.2.0', body='A long list of changes', body_truncated=True, is_draft=False, is_prerelease=False, created_at=datetime.fromisoformat("2025-11-01T09:00:00Z".replace('Z', '+00:00')), published_at=datetime.fromisoformat("2025-11-02T09:00:00Z".replace('Z', '+00:00'))), Release(id=290, tag_name='v1.2.9', body_truncated=False, is_draft=False, is_prerelease=False, created_at=datetime.fromisoformat("2026-03-01T00:00:00Z".replace('Z', '+00:00')), published_at=datetime.fromisoformat("2026-03-01T00:00:00Z".replace('Z', '+00:00')))), next_page=0)))

setattr(list_releases_three_unordered_pages, '__module__', "dex_connectors.github.githubmock")

def list_releases_no_releases() -> Case[ReleasePage]:
    return Case.branch_result(LISTED, ReleasePage(releases=(), next_page=0))

setattr(list_releases_no_releases, '__module__', "dex_connectors.github.githubmock")

def list_releases_rate_limited() -> Case[ReleasePage]:
    return Case.retry(Failure(FailureKind.RATE_LIMIT, CONNECTOR_ID, "listReleases", "GitHub rate limit was reached."), 1)

setattr(list_releases_rate_limited, '__module__', "dex_connectors.github.githubmock")

def list_releases_insufficient_scope_failure() -> Case[ReleasePage]:
    return Case.branch_result(INSUFFICIENT_SCOPE, ReleasePage(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "listReleases", "The OAuth grant does not match the required scopes."))

setattr(list_releases_insufficient_scope_failure, '__module__', "dex_connectors.github.githubmock")

def list_releases_authorization_revoked_failure() -> Case[ReleasePage]:
    return Case.branch_result(AUTHORIZATION_REVOKED, ReleasePage(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "listReleases", "The OAuth grant is invalid or revoked."))

setattr(list_releases_authorization_revoked_failure, '__module__', "dex_connectors.github.githubmock")

def list_releases_not_found_failure() -> Case[ReleasePage]:
    return Case.branch_result(NOT_FOUND, ReleasePage(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "listReleases", "The repository was not found or is inaccessible."))

setattr(list_releases_not_found_failure, '__module__', "dex_connectors.github.githubmock")

def list_releases_provider_rejected_failure() -> Case[ReleasePage]:
    return Case.branch_result(PROVIDER_REJECTED, ReleasePage(), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "listReleases", "GitHub conclusively rejected the releases query."))

setattr(list_releases_provider_rejected_failure, '__module__', "dex_connectors.github.githubmock")

def list_releases_invalid_response_failure() -> Case[ReleasePage]:
    return Case.branch_result(INVALID_RESPONSE, ReleasePage(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "listReleases", "GitHub returned an invalid or oversized releases response."))

setattr(list_releases_invalid_response_failure, '__module__', "dex_connectors.github.githubmock")

def list_releases_defect_failure() -> Case[ReleasePage]:
    return Case.branch_result(DEFECT, ReleasePage(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "listReleases", "Local input, connection configuration, or connector definition is invalid."))

setattr(list_releases_defect_failure, '__module__', "dex_connectors.github.githubmock")

def list_commits_listed(value: CommitPage, failure: Failure | None = None) -> Case[CommitPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_commits_listed, '__module__', "dex_connectors.github.githubmock")

def list_commits_insufficient_scope(value: CommitPage, failure: Failure | None = None) -> Case[CommitPage]:
    return Case.branch_result(INSUFFICIENT_SCOPE, value, failure)

setattr(list_commits_insufficient_scope, '__module__', "dex_connectors.github.githubmock")

def list_commits_authorization_revoked(value: CommitPage, failure: Failure | None = None) -> Case[CommitPage]:
    return Case.branch_result(AUTHORIZATION_REVOKED, value, failure)

setattr(list_commits_authorization_revoked, '__module__', "dex_connectors.github.githubmock")

def list_commits_not_found(value: CommitPage, failure: Failure | None = None) -> Case[CommitPage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_commits_not_found, '__module__', "dex_connectors.github.githubmock")

def list_commits_provider_rejected(value: CommitPage, failure: Failure | None = None) -> Case[CommitPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_commits_provider_rejected, '__module__', "dex_connectors.github.githubmock")

def list_commits_invalid_response(value: CommitPage, failure: Failure | None = None) -> Case[CommitPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_commits_invalid_response, '__module__', "dex_connectors.github.githubmock")

def list_commits_defect(value: CommitPage, failure: Failure | None = None) -> Case[CommitPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_commits_defect, '__module__', "dex_connectors.github.githubmock")

def list_commits_retry(failure: Failure, after_seconds: float = 0) -> Case[CommitPage]:
    return Case.retry(failure, after_seconds)

setattr(list_commits_retry, '__module__', "dex_connectors.github.githubmock")

def list_commits_three_pages_newest_first() -> Case[CommitPage]:
    return Case.paged(LISTED, (CommitPage(commits=(CommitSummary(sha='6dcb09b5b57875f334f61aebed695e2e4193db5e', message='Add release notes', message_truncated=False, author_login='octocat', author_name='The Octocat', authored_at=datetime.fromisoformat("2026-03-06T10:00:00Z".replace('Z', '+00:00')), committed_at=datetime.fromisoformat("2026-03-06T10:01:00Z".replace('Z', '+00:00')), url='https://github.com/octocat/hello-world/commit/6dcb09b5b57875f334f61aebed695e2e4193db5e'), CommitSummary(sha='7638417db6d59f3c431d3e1f261cc637155684cd', message='Fix flaky test', message_truncated=False, author_login='hubot', author_name='Hubot', authored_at=datetime.fromisoformat("2026-03-05T08:00:00Z".replace('Z', '+00:00')), committed_at=datetime.fromisoformat("2026-03-05T08:00:00Z".replace('Z', '+00:00')))), next_page=2), CommitPage(commits=(CommitSummary(sha='7638417db6d59f3c431d3e1f261cc637155684cd', message='Fix flaky test', message_truncated=False, author_login='hubot', author_name='Hubot', authored_at=datetime.fromisoformat("2026-03-05T08:00:00Z".replace('Z', '+00:00')), committed_at=datetime.fromisoformat("2026-03-05T08:00:00Z".replace('Z', '+00:00'))), CommitSummary(sha='3a0f86fb8db8eea7ccbb9a95f325ddbedfb25e15', message='A very long commit message', message_truncated=True, author_name='Build Bot', authored_at=datetime.fromisoformat("2026-03-04T23:59:59Z".replace('Z', '+00:00')), committed_at=datetime.fromisoformat("2026-03-04T23:59:59Z".replace('Z', '+00:00')))), next_page=3), CommitPage(commits=(CommitSummary(sha='762941318ee16e59dabbacb1b4049eec22f0d303', message='Initial commit', message_truncated=False, author_login='octocat', author_name='The Octocat', authored_at=datetime.fromisoformat("2026-03-01T00:00:00Z".replace('Z', '+00:00')), committed_at=datetime.fromisoformat("2026-03-01T00:00:00Z".replace('Z', '+00:00'))),), next_page=0)))

setattr(list_commits_three_pages_newest_first, '__module__', "dex_connectors.github.githubmock")

def list_commits_empty_repository() -> Case[CommitPage]:
    return Case.branch_result(LISTED, CommitPage(commits=(), next_page=0))

setattr(list_commits_empty_repository, '__module__', "dex_connectors.github.githubmock")

def list_commits_rate_limited() -> Case[CommitPage]:
    return Case.retry(Failure(FailureKind.RATE_LIMIT, CONNECTOR_ID, "listCommits", "GitHub rate limit was reached."), 1)

setattr(list_commits_rate_limited, '__module__', "dex_connectors.github.githubmock")

def list_commits_insufficient_scope_failure() -> Case[CommitPage]:
    return Case.branch_result(INSUFFICIENT_SCOPE, CommitPage(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "listCommits", "The OAuth grant does not match the required scopes."))

setattr(list_commits_insufficient_scope_failure, '__module__', "dex_connectors.github.githubmock")

def list_commits_authorization_revoked_failure() -> Case[CommitPage]:
    return Case.branch_result(AUTHORIZATION_REVOKED, CommitPage(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "listCommits", "The OAuth grant is invalid or revoked."))

setattr(list_commits_authorization_revoked_failure, '__module__', "dex_connectors.github.githubmock")

def list_commits_not_found_failure() -> Case[CommitPage]:
    return Case.branch_result(NOT_FOUND, CommitPage(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "listCommits", "The repository or starting ref was not found."))

setattr(list_commits_not_found_failure, '__module__', "dex_connectors.github.githubmock")

def list_commits_provider_rejected_failure() -> Case[CommitPage]:
    return Case.branch_result(PROVIDER_REJECTED, CommitPage(), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "listCommits", "GitHub conclusively rejected the commit query."))

setattr(list_commits_provider_rejected_failure, '__module__', "dex_connectors.github.githubmock")

def list_commits_invalid_response_failure() -> Case[CommitPage]:
    return Case.branch_result(INVALID_RESPONSE, CommitPage(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "listCommits", "GitHub returned an invalid or oversized commit response."))

setattr(list_commits_invalid_response_failure, '__module__', "dex_connectors.github.githubmock")

def list_commits_defect_failure() -> Case[CommitPage]:
    return Case.branch_result(DEFECT, CommitPage(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "listCommits", "Local input, connection configuration, or connector definition is invalid."))

setattr(list_commits_defect_failure, '__module__', "dex_connectors.github.githubmock")


class Mock:

    def __init__(self, connection_name: str = "github") -> None:
        self.get_authenticated_profile_script: Query[GetAuthenticatedProfileInput, AuthenticatedProfile] = Query(GET_AUTHENTICATED_PROFILE_DEFINITION)
        self.get_authenticated_profile_script.default(get_authenticated_profile_octocat_profile())
        self.list_public_repositories_script: Query[ListPublicRepositoriesInput, PublicRepositories] = Query(LIST_PUBLIC_REPOSITORIES_DEFINITION)
        self.list_public_repositories_script.default(list_public_repositories_mixed_repositories())
        self.list_merged_pull_requests_script: Query[ListMergedPullRequestsInput, MergedPullRequestPage] = Query(LIST_MERGED_PULL_REQUESTS_DEFINITION)
        self.list_merged_pull_requests_script.default(list_merged_pull_requests_three_unordered_pages())
        self.list_pull_request_files_script: Query[ListPullRequestFilesInput, PullRequestFilePage] = Query(LIST_PULL_REQUEST_FILES_DEFINITION)
        self.list_pull_request_files_script.default(list_pull_request_files_three_pages_of_changes())
        self.list_releases_script: Query[ListReleasesInput, ReleasePage] = Query(LIST_RELEASES_DEFINITION)
        self.list_releases_script.default(list_releases_three_unordered_pages())
        self.list_commits_script: Query[ListCommitsInput, CommitPage] = Query(LIST_COMMITS_DEFINITION)
        self.list_commits_script.default(list_commits_three_pages_newest_first())
        self._connection = new_connection_with_operations(
            get_authenticated_profile=self.get_authenticated_profile_script,
            list_public_repositories=self.list_public_repositories_script,
            list_merged_pull_requests=self.list_merged_pull_requests_script,
            list_pull_request_files=self.list_pull_request_files_script,
            list_releases=self.list_releases_script,
            list_commits=self.list_commits_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.github.githubmock")

__all__ = [
    "Mock",
    "get_authenticated_profile_profile_loaded",
    "get_authenticated_profile_verified_email_required",
    "get_authenticated_profile_insufficient_scope",
    "get_authenticated_profile_authorization_revoked",
    "get_authenticated_profile_not_found",
    "get_authenticated_profile_provider_rejected",
    "get_authenticated_profile_invalid_response",
    "get_authenticated_profile_defect",
    "get_authenticated_profile_retry",
    "get_authenticated_profile_octocat_profile",
    "get_authenticated_profile_rate_limited",
    "get_authenticated_profile_verified_email_required_failure",
    "get_authenticated_profile_insufficient_scope_failure",
    "get_authenticated_profile_authorization_revoked_failure",
    "get_authenticated_profile_not_found_failure",
    "get_authenticated_profile_provider_rejected_failure",
    "get_authenticated_profile_invalid_response_failure",
    "get_authenticated_profile_defect_failure",
    "list_public_repositories_repositories_loaded",
    "list_public_repositories_insufficient_scope",
    "list_public_repositories_authorization_revoked",
    "list_public_repositories_not_found",
    "list_public_repositories_provider_rejected",
    "list_public_repositories_invalid_response",
    "list_public_repositories_defect",
    "list_public_repositories_retry",
    "list_public_repositories_mixed_repositories",
    "list_public_repositories_no_repositories",
    "list_public_repositories_rate_limited",
    "list_public_repositories_insufficient_scope_failure",
    "list_public_repositories_authorization_revoked_failure",
    "list_public_repositories_not_found_failure",
    "list_public_repositories_provider_rejected_failure",
    "list_public_repositories_invalid_response_failure",
    "list_public_repositories_defect_failure",
    "list_merged_pull_requests_listed",
    "list_merged_pull_requests_insufficient_scope",
    "list_merged_pull_requests_authorization_revoked",
    "list_merged_pull_requests_not_found",
    "list_merged_pull_requests_provider_rejected",
    "list_merged_pull_requests_invalid_response",
    "list_merged_pull_requests_defect",
    "list_merged_pull_requests_retry",
    "list_merged_pull_requests_three_unordered_pages",
    "list_merged_pull_requests_no_merges",
    "list_merged_pull_requests_incomplete_search",
    "list_merged_pull_requests_rate_limited",
    "list_merged_pull_requests_insufficient_scope_failure",
    "list_merged_pull_requests_authorization_revoked_failure",
    "list_merged_pull_requests_not_found_failure",
    "list_merged_pull_requests_provider_rejected_failure",
    "list_merged_pull_requests_invalid_response_failure",
    "list_merged_pull_requests_defect_failure",
    "list_pull_request_files_listed",
    "list_pull_request_files_insufficient_scope",
    "list_pull_request_files_authorization_revoked",
    "list_pull_request_files_not_found",
    "list_pull_request_files_provider_rejected",
    "list_pull_request_files_invalid_response",
    "list_pull_request_files_defect",
    "list_pull_request_files_retry",
    "list_pull_request_files_three_pages_of_changes",
    "list_pull_request_files_rate_limited",
    "list_pull_request_files_insufficient_scope_failure",
    "list_pull_request_files_authorization_revoked_failure",
    "list_pull_request_files_not_found_failure",
    "list_pull_request_files_provider_rejected_failure",
    "list_pull_request_files_invalid_response_failure",
    "list_pull_request_files_defect_failure",
    "list_releases_listed",
    "list_releases_insufficient_scope",
    "list_releases_authorization_revoked",
    "list_releases_not_found",
    "list_releases_provider_rejected",
    "list_releases_invalid_response",
    "list_releases_defect",
    "list_releases_retry",
    "list_releases_three_unordered_pages",
    "list_releases_no_releases",
    "list_releases_rate_limited",
    "list_releases_insufficient_scope_failure",
    "list_releases_authorization_revoked_failure",
    "list_releases_not_found_failure",
    "list_releases_provider_rejected_failure",
    "list_releases_invalid_response_failure",
    "list_releases_defect_failure",
    "list_commits_listed",
    "list_commits_insufficient_scope",
    "list_commits_authorization_revoked",
    "list_commits_not_found",
    "list_commits_provider_rejected",
    "list_commits_invalid_response",
    "list_commits_defect",
    "list_commits_retry",
    "list_commits_three_pages_newest_first",
    "list_commits_empty_repository",
    "list_commits_rate_limited",
    "list_commits_insufficient_scope_failure",
    "list_commits_authorization_revoked_failure",
    "list_commits_not_found_failure",
    "list_commits_provider_rejected_failure",
    "list_commits_invalid_response_failure",
    "list_commits_defect_failure",
]
