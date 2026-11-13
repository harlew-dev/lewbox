import { Button, CodeBlock, Step } from '@harlew-dev/ui'
import { KeyIcon } from '@heroicons/react/16/solid'
import { useState } from 'react'
import { CreateTokenDialog } from '../access-tokens'
import { serverURL } from '../../lib/commands'

// "Sign in to this server" in the server dialogs. A machine signs in with an
// access token rather than a password; the token dialog opens over the
// server dialog, which keeps its place.
export function SignInStep({ tokenName }: { tokenName: string }) {
  const [open, setOpen] = useState(false)
  return (
    <Step
      title="Sign in to this server"
      description="With an access token. The command asks for it; paste the token when it does."
    >
      <CodeBlock code={`lewbox login ${serverURL()} --token`} />
      <div className="mt-3">
        <Button outline onClick={() => setOpen(true)}>
          <KeyIcon />
          Create an access token
        </Button>
      </div>
      <CreateTokenDialog open={open} onClose={() => setOpen(false)} onCreated={() => {}} defaultName={tokenName} />
    </Step>
  )
}
