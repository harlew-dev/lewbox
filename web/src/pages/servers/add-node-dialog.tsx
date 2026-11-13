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
  Notice,
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
  installCommand,
  joinClusterCommand,
  nodeConfigureCommand,
  validHost,
  validLabel,
  type NodeRole,
  type Runtime,
} from '../../lib/commands'
import { relativeTime } from '../../lib/format'
import { failureMessage } from '../../lib/use-load'
import { StepIndicator } from './step-indicator'
import { SignInStep } from './sign-in-step'
import { WaitForNodeStep } from './wait-step'

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
    <Dialog open={open} onClose={onClose} size="3xl">
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
  const [step, setStep] = useState<'configure' | 'commands'>('configure')
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
  const domainCluster = cluster.domain !== ''
  const ready =
    method === 'join' || (validLabel(node) && (domainCluster || validHost(host)))
  const install = installCommand({
    access: domainCluster ? 'domain' : 'public-host',
    host,
    domain: cluster.domain,
    dnsProfile: cluster.dns_profile ?? '',
    acmeEmail: '',
    runtime,
    node,
    version: '',
    extraFlags: '',
  })

  return (
    <>
      <DialogTitle>Add a node to {fixed ? cluster.name : 'a cluster'}</DialogTitle>
      <DialogDescription>
        A node joins from its own machine. Choose its role, then run the commands there.
      </DialogDescription>
      <StepIndicator labels={['Configure', 'Install commands']} current={step === 'configure' ? 0 : 1} />
      <DialogBody className="space-y-8">
        {step === 'configure' ? (
          <>
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
              <div className="space-y-6">
                <Field className="max-w-md">
                  <Label>Node name</Label>
                  <Input
                    value={node}
                    placeholder="node-2"
                    invalid={nodeProblem}
                    onChange={(event) => setNode(event.currentTarget.value.trim())}
                  />
                  {nodeProblem && <ErrorMessage>Lowercase letters, digits and hyphens.</ErrorMessage>}
                </Field>
                {domainCluster ? (
                  <div className="grid gap-6 sm:grid-cols-2">
                    <Field disabled>
                      <Label>Domain</Label>
                      <Input readOnly value={cluster.domain} />
                    </Field>
                    <Field disabled>
                      <Label>DNS profile</Label>
                      <Input readOnly value={cluster.dns_profile ?? 'None'} />
                    </Field>
                    <Description className="sm:col-span-2">
                      This cluster is reached on its domain, so the node installs the same way. Change them on the cluster’s
                      page.
                    </Description>
                    {!cluster.dns_profile && (
                      <Notice variant="warning" className="sm:col-span-2">
                        The cluster has a domain but no DNS profile, so the install command has a placeholder. Choose a profile
                        on the cluster’s page first.
                      </Notice>
                    )}
                  </div>
                ) : (
                  <Field className="max-w-md">
                    <Label>This node’s public IP</Label>
                    <Input
                      value={host}
                      placeholder="10.0.0.6"
                      invalid={hostProblem}
                      onChange={(event) => setHost(event.currentTarget.value.trim())}
                    />
                    {hostProblem ? (
                      <ErrorMessage>A hostname or IP address.</ErrorMessage>
                    ) : (
                      <Description>This cluster is reached by IP, so the node installs in public IP mode.</Description>
                    )}
                  </Field>
                )}
              </div>
            ) : (
              <Field className="max-w-xs">
                <Label>The command works for</Label>
                <Listbox value={ttl} onChange={reset(setTTL)}>
                  <ListboxOption value="15m">15 minutes</ListboxOption>
                  <ListboxOption value="1h">1 hour</ListboxOption>
                  <ListboxOption value="24h">24 hours</ListboxOption>
                </Listbox>
              </Field>
            )}
          </>
        ) : method === 'cli' ? (
          <Steps aria-label="Commands to run on the machine">
            <Step
              title="Install the Lewbox CLI on the machine"
              description={
                <>
                  Follow{' '}
                  <TextLink href="/platform/setup/install-the-cli" target="_blank" rel="noreferrer">
                    Install the CLI
                  </TextLink>
                  .
                </>
              }
            />
            <SignInStep tokenName={`Node ${node}`} />
            <Step title="Install AerolVM" description={`With the cluster's runtime, ${runtime}.`}>
              <CodeBlock code={install} />
            </Step>
            <Step
              title="Join the cluster"
              description={
                <>
                  It also applies the cluster’s storage settings on this node. <code>{nodeConfigureCommand}</code> applies
                  them again later, after the cluster’s profiles change.
                </>
              }
            >
              <CodeBlock code={joinClusterCommand({ node, slug: cluster.slug, role })} />
            </Step>
            <WaitForNodeStep cluster={cluster.slug} node={node} />
          </Steps>
        ) : (
          <div className="space-y-4">
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
                <WaitForNodeStep cluster={cluster.slug} />
              </Steps>
            ) : (
              <div className="flex justify-end">
                <Button outline loading={mint.loading} onClick={() => void mint.run()}>
                  Create join command
                </Button>
              </div>
            )}
            <Notice variant="info">
              A node joined without the Lewbox CLI gets no snapshot registry and shows as outdated until{' '}
              <code>{nodeConfigureCommand}</code> runs on it.
            </Notice>
          </div>
        )}
      </DialogBody>
      <DialogActions>
        {step === 'configure' ? (
          <>
            <Button plain onClick={onClose}>
              Cancel
            </Button>
            <Button onClick={() => setStep('commands')} disabled={!ready} disabledReason="Fill in required fields">
              Next: install commands
            </Button>
          </>
        ) : (
          <>
            <Button plain onClick={() => setStep('configure')}>
              Back
            </Button>
            <Button onClick={onClose}>Done</Button>
          </>
        )}
      </DialogActions>
    </>
  )
}
