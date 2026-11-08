# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListScheduledPostsInput, ListSocialProfilesInput, SchedulePostInput, ScheduledPost, ScheduledPostReceipt, ScheduledPostsPage, SocialProfile, SocialProfilesPage
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_SCHEDULED_POSTS_DEFINITION, LIST_SOCIAL_PROFILES_DEFINITION, PROVIDER_REJECTED, SCHEDULED, SCHEDULE_POST_DEFINITION, UNCERTAIN

def list_social_profiles_listed(value: SocialProfilesPage, failure: Failure | None = None) -> Case[SocialProfilesPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_social_profiles_listed, '__module__', "dex_connectors.hootsuite.hootsuitemock")

def list_social_profiles_provider_rejected(value: SocialProfilesPage, failure: Failure | None = None) -> Case[SocialProfilesPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_social_profiles_provider_rejected, '__module__', "dex_connectors.hootsuite.hootsuitemock")

def list_social_profiles_invalid_response(value: SocialProfilesPage, failure: Failure | None = None) -> Case[SocialProfilesPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_social_profiles_invalid_response, '__module__', "dex_connectors.hootsuite.hootsuitemock")

def list_social_profiles_defect(value: SocialProfilesPage, failure: Failure | None = None) -> Case[SocialProfilesPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_social_profiles_defect, '__module__', "dex_connectors.hootsuite.hootsuitemock")

def list_social_profiles_retry(failure: Failure, after_seconds: float = 0) -> Case[SocialProfilesPage]:
    return Case.retry(failure, after_seconds)

setattr(list_social_profiles_retry, '__module__', "dex_connectors.hootsuite.hootsuitemock")

def list_social_profiles_listed_default() -> Case[SocialProfilesPage]:
    return Case.branch_result(LISTED, SocialProfilesPage(profiles=(SocialProfile(id='115185509', type='FACEBOOK', username='Example Page'),)))

setattr(list_social_profiles_listed_default, '__module__', "dex_connectors.hootsuite.hootsuitemock")

def list_scheduled_posts_listed(value: ScheduledPostsPage, failure: Failure | None = None) -> Case[ScheduledPostsPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_scheduled_posts_listed, '__module__', "dex_connectors.hootsuite.hootsuitemock")

def list_scheduled_posts_provider_rejected(value: ScheduledPostsPage, failure: Failure | None = None) -> Case[ScheduledPostsPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_scheduled_posts_provider_rejected, '__module__', "dex_connectors.hootsuite.hootsuitemock")

def list_scheduled_posts_invalid_response(value: ScheduledPostsPage, failure: Failure | None = None) -> Case[ScheduledPostsPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_scheduled_posts_invalid_response, '__module__', "dex_connectors.hootsuite.hootsuitemock")

def list_scheduled_posts_defect(value: ScheduledPostsPage, failure: Failure | None = None) -> Case[ScheduledPostsPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_scheduled_posts_defect, '__module__', "dex_connectors.hootsuite.hootsuitemock")

def list_scheduled_posts_retry(failure: Failure, after_seconds: float = 0) -> Case[ScheduledPostsPage]:
    return Case.retry(failure, after_seconds)

setattr(list_scheduled_posts_retry, '__module__', "dex_connectors.hootsuite.hootsuitemock")

def list_scheduled_posts_listed_default() -> Case[ScheduledPostsPage]:
    return Case.branch_result(LISTED, ScheduledPostsPage(posts=(ScheduledPost(id='4138476039', state='SCHEDULED', social_profile_id='115185509', scheduled_send_time='2026-11-15T14:00:00Z'),), next_cursor=''))

setattr(list_scheduled_posts_listed_default, '__module__', "dex_connectors.hootsuite.hootsuitemock")

def schedule_post_scheduled(value: ScheduledPostReceipt, failure: Failure | None = None) -> Case[ScheduledPostReceipt]:
    return Case.branch_result(SCHEDULED, value, failure)

setattr(schedule_post_scheduled, '__module__', "dex_connectors.hootsuite.hootsuitemock")

def schedule_post_provider_rejected(value: ScheduledPostReceipt, failure: Failure | None = None) -> Case[ScheduledPostReceipt]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(schedule_post_provider_rejected, '__module__', "dex_connectors.hootsuite.hootsuitemock")

def schedule_post_uncertain(value: ScheduledPostReceipt, failure: Failure) -> Case[ScheduledPostReceipt]:
    return Case.uncertain(value, failure)

setattr(schedule_post_uncertain, '__module__', "dex_connectors.hootsuite.hootsuitemock")

def schedule_post_defect(value: ScheduledPostReceipt, failure: Failure | None = None) -> Case[ScheduledPostReceipt]:
    return Case.branch_result(DEFECT, value, failure)

setattr(schedule_post_defect, '__module__', "dex_connectors.hootsuite.hootsuitemock")

def schedule_post_retry(failure: Failure, after_seconds: float = 0) -> Case[ScheduledPostReceipt]:
    return Case.retry(failure, after_seconds)

setattr(schedule_post_retry, '__module__', "dex_connectors.hootsuite.hootsuitemock")

def schedule_post_scheduled_default() -> Case[ScheduledPostReceipt]:
    return Case.branch_result(SCHEDULED, ScheduledPostReceipt(id='4138476039', state='SCHEDULED', social_profile_id='115185509'))

setattr(schedule_post_scheduled_default, '__module__', "dex_connectors.hootsuite.hootsuitemock")


class Mock:

    def __init__(self, connection_name: str = "hootsuite") -> None:
        self.list_social_profiles_script: Query[ListSocialProfilesInput, SocialProfilesPage] = Query(LIST_SOCIAL_PROFILES_DEFINITION)
        self.list_social_profiles_script.default(list_social_profiles_listed_default())
        self.list_scheduled_posts_script: Query[ListScheduledPostsInput, ScheduledPostsPage] = Query(LIST_SCHEDULED_POSTS_DEFINITION)
        self.list_scheduled_posts_script.default(list_scheduled_posts_listed_default())
        self.schedule_post_script: Mutation[SchedulePostInput, ScheduledPostReceipt] = Mutation(SCHEDULE_POST_DEFINITION)
        self.schedule_post_script.default(schedule_post_scheduled_default())
        self._connection = new_connection_with_operations(
            list_social_profiles=self.list_social_profiles_script,
            list_scheduled_posts=self.list_scheduled_posts_script,
            schedule_post=self.schedule_post_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.hootsuite.hootsuitemock")

__all__ = [
    "Mock",
    "list_social_profiles_listed",
    "list_social_profiles_provider_rejected",
    "list_social_profiles_invalid_response",
    "list_social_profiles_defect",
    "list_social_profiles_retry",
    "list_social_profiles_listed_default",
    "list_scheduled_posts_listed",
    "list_scheduled_posts_provider_rejected",
    "list_scheduled_posts_invalid_response",
    "list_scheduled_posts_defect",
    "list_scheduled_posts_retry",
    "list_scheduled_posts_listed_default",
    "schedule_post_scheduled",
    "schedule_post_provider_rejected",
    "schedule_post_uncertain",
    "schedule_post_defect",
    "schedule_post_retry",
    "schedule_post_scheduled_default",
]
