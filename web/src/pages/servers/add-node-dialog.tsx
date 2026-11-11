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
  ErrorNotice,
  Field,
  Input,
  Label,
  Listbox,
  ListboxOption,
  RadioGroup,
  Step,
  Steps,
  Text,
  TextButton,
  TextLink,
  useAction,
} from '@harlew-dev/ui'
import { useOpenCount } from '@harlew-dev/ui/lib/use-open-count'
import { useState } from 'react'
import * as api from '../../lib/api'
import {
  arg,
  installCommand,
  serverURL,
  validHost,
  validLabel,
  type NodeRole,
  type Runtime,
} from '../../lib/commands'
import { relativeTime } from '../../lib/format'
import { failureMessage } from '../../lib/use-load'

const roles: { value: NodeRole; label: string; description: string }[] = [
  { value: 'mixed', label: 'Mixed', description: 'Runs sandboxes and votes in the cluster. The usual choice.' },
  { value: 'worker', label: 'Worker', description: 'Runs sandboxes only.' },
  { value: 'server', label: 'Server', description: 'Votes in the cluster and runs no sandboxes.' },
  { value: 'ingress', label: 'Ingress', description: 'Takes customer traffic only.' },
]

type Method = 'cli' | 'join'

// Adding a node happens on the new machine. With the Lewbox CLI there, the
// console says what to run; without it, it mints a single-use join token and
// gives the installer's join command.
export function AddNodeDialog({
  open,
  onClose,
  clusters,
  cluster,
}: {
  open: boolean
  onClose: () => void
  clusters: api.Cluster[]
  // A fixed cluster (from its page); otherwise the dialog asks.
  cluster?: api.Cluster
}) {
  const count = useOpenCount(open)
  return (
    <Dialog open={open} onClose={onClose} size="2xl">
      <AddNode key={count} onClose={onClose} clusters={clusters} fixed={cluster} />
    </Dialog>
  )
}

function AddNode({
  onClose,
  clusters,
  fixed,
}: {
  onClose: () => void
  clusters: api.Cluster[]
  fixed?: api.Cluster
}) {
  const [slug, setSlug] = useState(fixed?.slug ?? clusters[0]?.slug ?? '')
  const [node, setNode] = useState('')
  const [host, setHost] = useState('')
  const [role, setRole] = useState<NodeRole>('mixed')
  const [method, setMethod] = useState<Method>('cli')
  const [ttl, setTTL] = useState('1h')
  const [join, setJoin] = useState<api.JoinToken | null>(null)
  const [showToken, setShowToken] = useState(false)

  const cluster = fixed ?? clusters.find((item) => item.slug === slug)
  const runtime = (cluster?.runtime ?? 'docker') as Runtime
  const nodeProblem = node !== '' && !validLabel(node)
  const hostProblem = host !== '' && !validHost(host)

  const mint = useAction(
    async () => {
      if (!cluster) return
      setJoin(await api.createJoinToken(cluster.slug, { role, ttl }))
      setShowToken(false)
    },
    { toError: (failure) => ({ message: failureMessage(failure) }) },
  )

  // A changed choice makes an earlier join command stale.
  function reset<T>(set: (value: T) => void) {
    return (value: T) => {
      set(value)
      setJoin(null)
    }
  }

  if (!cluster) {
    return (
      <>
        <DialogTitle>Add a node</DialogTitle>
        <DialogDescription>Start a cluster first: a node joins an existing cluster.</DialogDescription>
        <DialogActions>
          <Button onClick={onClose}>Close</Button>
        </DialogActions>
      </>
    )
  }

  const masked = join ? join.command.replace(join.token, '•'.repeat(12)) : ''

  return (
    <>
      <DialogTitle>Add a node to {fixed ? cluster.name : 'a cluster'}</DialogTitle>
      <DialogDescription>
        A node joins from its own machine. Choose its role, then run the commands there.
      </DialogDescription>
      <DialogBody className="space-y-8">
        <div className="grid gap-6 sm:grid-cols-2">
          {!fixed && (
            <Field>
              <Label>Cluster</Label>
              <Listbox value={slug} onChange={reset(setSlug)}>
                {clusters.map((item) => (
                  <ListboxOption key={item.id} value={item.slug}>
                    {item.name}
                  </ListboxOption>
                ))}
              </Listbox>
            </Field>
          )}
          <Field>
            <Label>Role</Label>
            <Listbox value={role} onChange={reset(setRole)}>
              {roles.map((item) => (
                <ListboxOption key={item.value} value={item.value}>
                  {item.label}
                </ListboxOption>
              ))}
            </Listbox>
            <Description>{roles.find((item) => item.value === role)?.description}</Description>
          </Field>
        </div>

        <RadioGroup
          label="How will you set it up?"
          value={method}
          onChange={setMethod}
          options={[
            {
              value: 'cli',
              label: 'With the Lewbox CLI on the machine',
              description: 'Recommended. Lewbox installs a verified AerolVM, joins the cluster and registers the node.',
            },
            {
              value: 'join',
              label: 'With AerolVM’s installer only',
              description: 'A single-use join command for a machine that won’t run the Lewbox CLI.',
            },
          ]}
        />

        {method === 'cli' ? (
          <>
            <div className="grid gap-6 sm:grid-cols-2">
              <Field>
                <Label>Node name</Label>
                <Input
                  value={node}
                  placeholder="node-2"
                  invalid={nodeProblem}
                  onChange={(event) => setNode(event.currentTarget.value.trim())}
                />
                {nodeProblem && <ErrorMessage>Lowercase letters, digits and hyphens.</ErrorMessage>}
              </Field>
              <Field>
                <Label>Public host</Label>
                <Input
                  value={host}
                  placeholder="10.0.0.6"
                  invalid={hostProblem}
                  onChange={(event) => setHost(event.currentTarget.value.trim())}
                />
                {hostProblem && <ErrorMessage>A hostname or IP address.</ErrorMessage>}
              </Field>
            </div>
            <Steps aria-label="Commands to run on the machine">
              <Step
                title="Install the Lewbox CLI on the machine"
                description={
                  <>
                    See <TextLink href="/docs/setup/install-the-cli">Install the CLI</TextLink>.
                  </>
                }
              />
              <Step title="Sign in to this server">
                <CodeBlock code={`lewbox login ${serverURL()}`} />
              </Step>
              <Step title="Install AerolVM" description={`With the cluster's runtime, ${runtime}.`}>
                <CodeBlock code={installCommand(host, runtime)} />
              </Step>
              <Step title="Join the cluster">
                <CodeBlock
                  code={`lewbox node add ${arg(node, validLabel, 'node-name')} --cluster ${cluster.slug} --role ${role}`}
                />
              </Step>
            </Steps>
          </>
        ) : (
          <div className="space-y-4">
            <Field className="max-w-xs">
              <Label>The command works for</Label>
              <Listbox value={ttl} onChange={reset(setTTL)}>
                <ListboxOption value="15m">15 minutes</ListboxOption>
                <ListboxOption value="1h">1 hour</ListboxOption>
                <ListboxOption value="24h">24 hours</ListboxOption>
              </Listbox>
            </Field>
            <ErrorNotice error={mint.error} />
            {join ? (
              <Steps aria-label="Command to run on the machine">
                <Step
                  title="Run this on the machine, as a user with sudo"
                  description={`It works once and expires ${relativeTime(join.expires_at)}. Lewbox lists the node after its next check of the cluster.`}
                >
                  <CodeBlock code={join.command} label="Copy join command">
                    <code>{showToken ? join.command : masked}</code>
                  </CodeBlock>
                  <Text className="mt-2">
                    The token is hidden; Copy copies the whole command.{' '}
                    <TextButton onClick={() => setShowToken(!showToken)}>
                      {showToken ? 'Hide the token' : 'Show the token'}
                    </TextButton>
                  </Text>
                </Step>
              </Steps>
            ) : (
              <div className="flex justify-end">
                <Button outline loading={mint.loading} onClick={() => void mint.run()}>
                  Create join command
                </Button>
              </div>
            )}
          </div>
        )}
      </DialogBody>
      <DialogActions>
        <Button onClick={onClose}>Done</Button>
      </DialogActions>
    </>
  )
}
