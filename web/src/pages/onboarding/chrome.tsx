import {
  Button,
  CenteredLayout,
  Dropdown,
  DropdownButton,
  DropdownDivider,
  DropdownHeader,
  DropdownItem,
  DropdownLabel,
  DropdownMenu,
  Strong,
  Text,
} from '@harlew-dev/ui'
import { ArrowRightStartOnRectangleIcon, EllipsisHorizontalIcon } from '@heroicons/react/16/solid'
import { useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router'
import { BrandLogo } from '../../components/brand-logo'
import { useAuth } from '../../lib/auth'

// Where onboarding says who is signed in and offers a way out, without the
// account's workspace-only settings.
function SignedInMenu() {
  const { user, signOut } = useAuth()
  const navigate = useNavigate()
  const [busy, setBusy] = useState(false)

  async function signOutNow() {
    if (busy) return
    setBusy(true)
    try {
      await signOut()
      navigate('/login', { replace: true })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dropdown>
      <DropdownButton as={Button} plain aria-label="Account">
        <EllipsisHorizontalIcon />
      </DropdownButton>
      <DropdownMenu className="min-w-64" anchor="bottom end">
        <DropdownHeader>
          <Text className="truncate">
            Signed in as <Strong>{user?.email}</Strong>
          </Text>
        </DropdownHeader>
        <DropdownDivider />
        <DropdownItem disabled={busy} onClick={() => void signOutNow()}>
          <ArrowRightStartOnRectangleIcon data-slot="icon" />
          <DropdownLabel>Sign out</DropdownLabel>
        </DropdownItem>
      </DropdownMenu>
    </Dropdown>
  )
}

// The centred chrome onboarding pages share: the logo, the account menu and
// the page below them.
export function OnboardingChrome({
  children,
  width = 'max-w-sm',
}: {
  children: ReactNode
  width?: string
}) {
  return (
    <CenteredLayout>
      <div className={`grid w-full ${width} grid-cols-1 gap-8`}>
        <div className="flex items-center justify-between gap-4">
          <BrandLogo href="/" />
          <SignedInMenu />
        </div>
        {children}
      </div>
    </CenteredLayout>
  )
}
