import {
  Avatar,
  Dropdown,
  DropdownButton,
  DropdownDivider,
  DropdownItem,
  DropdownMenu,
  NavbarItem,
  SidebarItem,
  UserIdentity,
} from '@harlew-dev/ui'
import {
  ArrowRightStartOnRectangleIcon,
  BookOpenIcon,
  ChevronUpIcon,
  Cog6ToothIcon,
} from '@heroicons/react/16/solid'
import type { User } from '../lib/api'

function initials(email: string): string {
  const name = email.split('@')[0] ?? email
  const parts = name.split(/[._-]+/).filter(Boolean)
  return (
    parts
      .slice(0, 2)
      .map((part) => part[0])
      .join('')
      .toUpperCase() || '?'
  )
}

function UserAvatar({ user, className }: { user: User; className: string }) {
  return (
    <Avatar
      initials={initials(user.email)}
      alt=""
      color="zinc"
      className={className}
    />
  )
}

// The account menu: in the sidebar's footer (the account's avatar, email and
// role, opening upwards) and in the phone navbar (the avatar alone).
export function AccountMenu({
  user,
  active,
  signingOut,
  onSignOut,
  placement,
}: {
  user: User | null
  active: boolean
  signingOut: boolean
  onSignOut: () => void
  placement: 'sidebar' | 'navbar'
}) {
  if (!user) return null
  const role = user.instance_admin ? 'Instance admin' : 'Member'
  return (
    <Dropdown>
      {placement === 'sidebar' ? (
        <DropdownButton as={SidebarItem} selected={active}>
          <UserIdentity
            avatar={<UserAvatar user={user} className="size-10" />}
            name={user.email}
            detail={role}
          />
          <ChevronUpIcon data-slot="icon" />
        </DropdownButton>
      ) : (
        <DropdownButton
          as={NavbarItem}
          selected={active}
          aria-label={`Account: ${user.email}`}
        >
          <UserAvatar user={user} className="size-8" />
        </DropdownButton>
      )}
      <DropdownMenu
        anchor={placement === 'sidebar' ? 'top start' : 'bottom end'}
        className={placement === 'sidebar' ? 'min-w-(--anchor-width)' : 'min-w-64'}
      >
        <DropdownItem href="/account" selected={active}>
          <Cog6ToothIcon data-slot="icon" />
          Account settings
        </DropdownItem>
        <DropdownItem href="/docs">
          <BookOpenIcon data-slot="icon" />
          Documentation
        </DropdownItem>
        <DropdownDivider />
        <DropdownItem disabled={signingOut} onClick={onSignOut}>
          <ArrowRightStartOnRectangleIcon data-slot="icon" />
          {signingOut ? 'Signing out…' : 'Sign out'}
        </DropdownItem>
      </DropdownMenu>
    </Dropdown>
  )
}
