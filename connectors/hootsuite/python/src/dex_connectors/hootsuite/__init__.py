# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Hootsuite operations for Dex Python Flows."""

from .client import Client, Config, Credentials
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import (
    ListScheduledPostsInput,
    ListSocialProfilesInput,
    ScheduledPost,
    ScheduledPostReceipt,
    ScheduledPostsPage,
    SchedulePostInput,
    SocialProfile,
    SocialProfilesPage,
)
from .operations import ListScheduledPostsOperation, ListSocialProfilesOperation, SchedulePostOperation
from .steps import new_list_scheduled_posts_step, new_list_social_profiles_step, new_schedule_post_step

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "Connection",
    "ListScheduledPostsInput",
    "ListScheduledPostsOperation",
    "ListSocialProfilesInput",
    "ListSocialProfilesOperation",
    "SchedulePostInput",
    "SchedulePostOperation",
    "ScheduledPost",
    "ScheduledPostReceipt",
    "ScheduledPostsPage",
    "SocialProfile",
    "SocialProfilesPage",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "new_list_scheduled_posts_step",
    "new_list_social_profiles_step",
    "new_schedule_post_step",
]
