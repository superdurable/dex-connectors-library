# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListAccountsInput selects one page of accounts visible to the connected user.
@dataclass(frozen=True, slots=True)
class ListAccountsInput:
    # PageToken is Google's opaque cursor; blank selects the first page.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken'})


setattr(ListAccountsInput, '__module__', 'dex_connectors.google_tag_manager.models')


# TagManagerAccount is the stable account path and display name.
@dataclass(frozen=True, slots=True)
class TagManagerAccount:
    # Path is the API resource identity, such as accounts/123456.
    path: str = ''
    # Name is the display name shown in Tag Manager.
    name: str = ''


setattr(TagManagerAccount, '__module__', 'dex_connectors.google_tag_manager.models')


# AccountsPage contains one bounded account page and next cursor.
@dataclass(frozen=True, slots=True)
class AccountsPage:
    # Accounts are the projected account identities.
    accounts: tuple[TagManagerAccount, ...] = ()
    # NextPageToken is blank on the final page.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'accounts', _replace_nil_slices(self.accounts, 1))


AccountsPage.__annotations__['accounts'] = tuple[TagManagerAccount, ...] | None
AccountsPage.__dataclass_fields__['accounts'].type = tuple[TagManagerAccount, ...] | None
setattr(AccountsPage, '__module__', 'dex_connectors.google_tag_manager.models')


# ListContainersInput selects containers in one account.
@dataclass(frozen=True, slots=True)
class ListContainersInput:
    # AccountID is the numeric account ID from listAccounts.
    account_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'accountId'})
    # PageToken is Google's opaque cursor; blank selects the first page.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken'})


setattr(ListContainersInput, '__module__', 'dex_connectors.google_tag_manager.models')


# TagManagerContainer is the stable container path and display identity.
@dataclass(frozen=True, slots=True)
class TagManagerContainer:
    # Path is the API resource identity.
    path: str = ''
    # Name is the display name shown in Tag Manager.
    name: str = ''
    # PublicID is the GTM identifier shown in the Tag Manager UI.
    public_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'publicId'})


setattr(TagManagerContainer, '__module__', 'dex_connectors.google_tag_manager.models')


# ContainersPage contains one bounded container page and next cursor.
@dataclass(frozen=True, slots=True)
class ContainersPage:
    # Containers are the projected container identities.
    containers: tuple[TagManagerContainer, ...] = ()
    # NextPageToken is blank on the final page.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'containers', _replace_nil_slices(self.containers, 1))


ContainersPage.__annotations__['containers'] = tuple[TagManagerContainer, ...] | None
ContainersPage.__dataclass_fields__['containers'].type = tuple[TagManagerContainer, ...] | None
setattr(ContainersPage, '__module__', 'dex_connectors.google_tag_manager.models')


# ListTagsInput selects tags in one workspace.
@dataclass(frozen=True, slots=True)
class ListTagsInput:
    # AccountID is the numeric Tag Manager account ID.
    account_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'accountId'})
    # ContainerID is the numeric Tag Manager container ID.
    container_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'containerId'})
    # WorkspaceID is the numeric Tag Manager workspace ID.
    workspace_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'workspaceId'})
    # PageToken is Google's opaque cursor; blank selects the first page.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken'})


setattr(ListTagsInput, '__module__', 'dex_connectors.google_tag_manager.models')


# TagManagerTag contains a tag's stable path, display name, and type.
@dataclass(frozen=True, slots=True)
class TagManagerTag:
    # Path is the API resource identity.
    path: str = ''
    # Name is the display name shown in Tag Manager.
    name: str = ''
    # Type is Google's tag template type identifier.
    type: str = ''


setattr(TagManagerTag, '__module__', 'dex_connectors.google_tag_manager.models')


# TagsPage contains one bounded tag page and next cursor.
@dataclass(frozen=True, slots=True)
class TagsPage:
    # Tags are the projected tag identities.
    tags: tuple[TagManagerTag, ...] = ()
    # NextPageToken is blank on the final page.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'tags', _replace_nil_slices(self.tags, 1))


TagsPage.__annotations__['tags'] = tuple[TagManagerTag, ...] | None
TagsPage.__dataclass_fields__['tags'].type = tuple[TagManagerTag, ...] | None
setattr(TagsPage, '__module__', 'dex_connectors.google_tag_manager.models')
