import {
  Button,
  CodeBlock,
  Description,
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
  ErrorMessage,
  Field,
  Input,
  Label,
  Listbox,
  ListboxOption,
  Step,
  Steps,
  TextLink,
} from '@harlew-dev/ui'
import { useOpenCount } from '@harlew-dev/ui/lib/use-open-count'
import { useState } from 'react'
import {
  arg,
  installCLICommand,
  installCommand,
  serverURL,
  validHost,
  validLabel,
  type Runtime,
} from '../../lib/commands'

// Starting a cluster happens on its first machine: the console can only say
// what to run there. The fields fill in the commands.
export function StartClusterDialog({
  open,
  onClose,
}: {
  open: boolean
  onClose: () => void
}) {
  const count = useOpenCount(open)
  return (
    <Dialog open={open} onClose={onClose} size="2xl">
      <StartCluster key={count} onClose={onClose} />
    </Dialog>
  )
}

function StartCluster({ onClose }: { onClose: () => void }) {
  const [slug, setSlug] = useState('')
  const [node, setNode] = useState('node-1')
  const [host, setHost] = useState('')
  const [runtime, setRuntime] = useState<Runtime>('docker')

  const slugProblem = slug !== '' && !validLabel(slug)
  const nodeProblem = node !== '' && !validLabel(node)
  const hostProblem = host !== '' && !validHost(host)

  return (
    <>
      <DialogTitle>Start a new cluster</DialogTitle>
      <DialogDescription>
        A cluster starts on the machine that will be its first node. Fill these in, then run
        the commands on that machine. The cluster shows up here when the last one finishes.
      </DialogDescription>
      <DialogBody>
        <div className="grid gap-6 sm:grid-cols-2">
          <Field>
            <Label>Cluster slug</Label>
            <Input
              value={slug}
              placeholder="eu-west"
              invalid={slugProblem}
              onChange={(event) => setSlug(event.currentTarget.value.trim())}
            />
            {slugProblem && <ErrorMessage>Lowercase letters, digits and hyphens.</ErrorMessage>}
          </Field>
          <Field>
            <Label>First node's name</Label>
            <Input
              value={node}
              invalid={nodeProblem}
              onChange={(event) => setNode(event.currentTarget.value.trim())}
            />
            {nodeProblem && <ErrorMessage>Lowercase letters, digits and hyphens.</ErrorMessage>}
          </Field>
          <Field>
            <Label>Public host</Label>
            <Input
              value={host}
              placeholder="10.0.0.5"
              invalid={hostProblem}
              onChange={(event) => setHost(event.currentTarget.value.trim())}
            />
            {hostProblem ? (
              <ErrorMessage>A hostname or IP address.</ErrorMessage>
            ) : (
              <Description>The address other nodes and your apps reach it on.</Description>
            )}
          </Field>
          <Field>
            <Label>Runtime</Label>
            <Listbox value={runtime} onChange={setRuntime}>
              <ListboxOption value="docker">Docker</ListboxOption>
              <ListboxOption value="firecracker">Firecracker</ListboxOption>
            </Listbox>
            <Description>Every node in a cluster uses the same one.</Description>
          </Field>
        </div>

        <Steps className="mt-10" aria-label="Commands to run on the machine">
          <Step
            title="Install the Lewbox CLI on the machine"
            description={
              <>
                On Ubuntu or Debian. Run it, and the next commands, as your own user, not root. Other systems: <TextLink href="/platform/setup/install-the-cli">Install the CLI</TextLink>.
              </>
            }
          >
            <CodeBlock code={installCLICommand} />
          </Step>
          <Step title="Sign in to this server">
            <CodeBlock code={`lewbox login ${serverURL()}`} />
          </Step>
          <Step title="Install AerolVM" description="It checks the machine first, then installs a pinned, verified release.">
            <CodeBlock code={installCommand(host, runtime)} />
          </Step>
          <Step title="Start the cluster" description="It starts AerolVM, starts the cluster and registers it here.">
            <CodeBlock
              code={`lewbox node add ${arg(node, validLabel, 'node-name')} --new-cluster ${arg(slug, validLabel, 'slug')}`}
            />
          </Step>
        </Steps>
      </DialogBody>
      <DialogActions>
        <Button onClick={onClose}>Done</Button>
      </DialogActions>
    </>
  )
}
