# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""profileChanged profiles, hashes, event IDs, and events, byte compatible with the Go connector."""

import hashlib
from datetime import UTC, datetime
from enum import StrEnum
from typing import Any

from dex_connectors.sdk import TriggerEvent
from dex_connectors.sdk.projectconfig import encode_go_json_value

from .client import _bounded, _canonical_email, _locale, _safe_url, _valid_subject
from .models import ProfileChangedEvent, ProfileClaims


class ProfileField(StrEnum):
    """One tracked profile claim in ``ProfileChangedEvent.changed_fields``, in Go's constant order.

    Each value is the ProfileClaims JSON field; the comment names the UserInfo claim it comes from.
    """

    NAME = "name"
    """The full name, from the name claim."""
    GIVEN_NAME = "givenName"
    """The first name, from the given_name claim."""
    FAMILY_NAME = "familyName"
    """The last name, from the family_name claim."""
    EMAIL = "email"
    """The primary email address, from the email claim."""
    EMAIL_VERIFIED = "emailVerified"
    """Whether LinkedIn verified the primary email, from the email_verified claim."""
    PICTURE_URL = "pictureUrl"
    """The profile picture, from the picture claim; a re-signed URL of the same picture is no change."""
    LOCALE = "locale"
    """The member's locale, from the locale claim."""


def profile_claims_from_user_info(claims: dict[str, Any]) -> ProfileClaims | None:
    """Bound decoded UserInfo claims as GetAuthenticatedProfile does; None without a valid sub claim.

    Unlike AuthenticatedProfile, the result keeps an unverified or absent email.
    """
    subject = claims["sub"]
    if not _valid_subject(subject):
        return None
    return ProfileClaims(
        subject=subject,
        name=_bounded(claims["name"], 256),
        given_name=_bounded(claims["given_name"], 128),
        family_name=_bounded(claims["family_name"], 128),
        email=_canonical_email(claims["email"]),
        email_verified=claims["email_verified"],
        picture_url=_safe_url(claims["picture"]),
        locale=_locale(claims["locale"]),
    )


def picture_identity(picture_url: str) -> str:
    """Drop the query and fragment, which carry LinkedIn's expiring signature, as Go's url.URL does.

    A bare "?" stays, since Go keeps ForceQuery.
    """
    without_fragment = picture_url.split("#", 1)[0]
    address, separator, query = without_fragment.partition("?")
    return address + "?" if separator and not query else address


def _tracked(profile: ProfileClaims) -> tuple[tuple[str, str | bool], ...]:
    """The claims a profile hash covers, in Go's trackedProfile field order."""
    return (
        ("name", profile.name),
        ("givenName", profile.given_name),
        ("familyName", profile.family_name),
        ("email", profile.email),
        ("emailVerified", profile.email_verified),
        ("picture", picture_identity(profile.picture_url)),
        ("locale", profile.locale),
    )


def profile_hash(profile: ProfileClaims) -> str:
    """Return the first 16 bytes of SHA-256 over Go's JSON of the tracked claims, hex encoded."""
    members = ",".join(
        f'"{name}":' + encode_go_json_value(value).decode() for name, value in _tracked(profile)
    )
    return hashlib.sha256(("{" + members + "}").encode()).hexdigest()[:32]


def changed_profile_fields(previous: ProfileClaims, current: ProfileClaims) -> tuple[str, ...]:
    """List the tracked claims that differ, in ProfileField order."""
    return tuple(
        field.value
        for field, (_, before), (_, after) in zip(
            ProfileField, _tracked(previous), _tracked(current), strict=True
        )
        if before != after
    )


def profile_change_id(subject: str, previous_hash: str, current_hash: str, sequence: int) -> str:
    """Hash the subject, both profile hashes, and the delivered-change count, as Go does.

    The count gives a profile edited back and forth a new ID each time, and a refetch the same one.
    """
    joined = "\x00".join((subject, previous_hash, current_hash, str(sequence)))
    return "linkedin-profile-" + hashlib.sha256(joined.encode()).hexdigest()[:32]


def new_profile_changed_trigger_event(
    previous_hash: str,
    current_hash: str,
    sequence: int,
    previous: ProfileClaims,
    current: ProfileClaims,
    observed_at: datetime,
) -> TriggerEvent[ProfileChangedEvent]:
    """Build the delivered change, observed at observed_at."""
    payload = ProfileChangedEvent(
        subject=current.subject,
        previous=previous,
        current=current,
        changed_fields=changed_profile_fields(previous, current),
    )
    return TriggerEvent(
        profile_change_id(current.subject, previous_hash, current_hash, sequence),
        observed_at.astimezone(UTC),
        payload,
    )
