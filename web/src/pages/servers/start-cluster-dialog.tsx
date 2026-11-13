import {
  Button,
  Checkbox,
  CodeBlock,
  Description,
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
  Disclosure,
  ErrorMessage,
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
  TextLink,
} from '@harlew-dev/ui'
import { useOpenCount } from '@harlew-dev/ui/lib/use-open-count'
import { useState } from 'react'
import * as api from '../../lib/api'
import {
  dnsRecordsOffCommand,
  installCLICommand,
  installCommand,
  newClusterCommand,
  serverURL,
  validDomain,
  validEmail,
  validHost,
  validInstallerFlags,
  validLabel,
  validVersion,
  type AccessMode,
  type Runtime,
  type StopPolicy,
} from '../../lib/commands'
import { useLoad } from '../../lib/use-load'
import { StepIndicator } from './step-indicator'

// Starting a cluster happens on its first machine: the console can only say
// what to run there. Step one gathers every choice; step two shows the
// commands those choices produce.
export function StartClusterDialog({
  open,
  onClose,
}: {
  open: boolean
  onClose: () => void
}) {
  const count = useOpenCount(open)
  return (
    <Dialog open={open} onClose={onClose} size="3xl">
      <StartCluster key={count} onClose={onClose} />
    </Dialog>
  )
}

const accessOptions: { value: AccessMode; label: string; description: string }[] = [
  { value: 'domain', label: 'Domain', description: 'Production. A domain with TLS certificates, through a DNS profile.' },
  { value: 'public-host', label: 'Public IP', description: 'Labs. Reached by IP address or hostname over plain HTTP, no TLS.' },
  { value: 'local', label: 'Local', description: 'Development. Only this machine reaches it.' },
]

function StartCluster({ onClose }: { onClose: () => void }) {
  const [step, setStep] = useState<'configure' | 'commands'>('configure')
  const [slug, setSlug] = useState('')
  const [node, setNode] = useState('node-1')
  const [access, setAccess] = useState<AccessMode>('domain')
  const [domain, setDomain] = useState('')
  const [dnsProfile, setDNSProfile] = useState('')
  const [acmeEmail, setACMEEmail] = useState('')
  const [manageDNS, setManageDNS] = useState(true)
  const [host, setHost] = useState('')
  const [runtime, setRuntime] = useState<Runtime>('docker')
  const [storageProfile, setStorageProfile] = useState('')
  const [stopPolicy, setStopPolicy] = useState<StopPolicy>('keep')
  const [version, setVersion] = useState('')
  const [extraFlags, setExtraFlags] = useState('')

  const dnsProfiles = useLoad(() => api.listDNSProfiles(), [])
  const storageProfiles = useLoad(() => api.listStorageProfiles(), [])
  const dnsList = dnsProfiles.data?.data ?? []
  const storageList = storageProfiles.data?.data ?? []
  const selectedDNS = dnsList.find((profile) => profile.name === dnsProfile)

  // Local mode can't run Firecracker; archive needs storage and Docker.
  const effectiveRuntime: Runtime = access === 'local' ? 'docker' : runtime
  const archiveBlocked =
    storageProfile === '' ? 'Choose a storage profile first.' : effectiveRuntime !== 'docker' ? 'Archiving needs the Docker runtime.' : ''
  const effectivePolicy: StopPolicy = archiveBlocked ? 'keep' : stopPolicy

  const slugProblem = slug !== '' && !validLabel(slug)
  const nodeProblem = node !== '' && !validLabel(node)
  const hostProblem = host !== '' && !validHost(host)
  const domainProblem = domain !== '' && !validDomain(domain)
  const emailProblem = acmeEmail !== '' && !validEmail(acmeEmail)
  const versionProblem = version !== '' && !validVersion(version)
  const flagsProblem = !validInstallerFlags(extraFlags)

  const ready =
    validLabel(slug) &&
    validLabel(node) &&
    !emailProblem &&
    !versionProblem &&
    !flagsProblem &&
    (access === 'local' ||
      (access === 'public-host' ? validHost(host) : validDomain(domain) && dnsProfile !== ''))

  const install = installCommand({
    access,
    host,
    domain,
    dnsProfile,
    acmeEmail,
    runtime: effectiveRuntime,
    node,
    version,
    extraFlags,
  })
  const addNode = newClusterCommand({ node, slug, storageProfile, stopPolicy: effectivePolicy })

  if (step === 'commands') {
    return (
      <>
        <DialogTitle>Start a new cluster</DialogTitle>
        <DialogDescription>
          Run these on the machine that will be the cluster’s first node. The cluster shows up here when the last one
          finishes.
        </DialogDescription>
        <StepIndicator labels={['Configure', 'Install commands']} current={1} />
        <DialogBody>
          <Steps aria-label="Commands to run on the machine">
            <Step
              title="Install the Lewbox CLI on the machine"
              description={
                <>
                  On Ubuntu or Debian. Run it, and the next commands, as your own user, not root. Other systems:{' '}
                  <TextLink href="/platform/setup/install-the-cli">Install the CLI</TextLink>.
                </>
              }
            >
              <CodeBlock code={installCLICommand} />
            </Step>
            <Step title="Sign in to this server">
              <CodeBlock code={`lewbox login ${serverURL()}`} />
            </Step>
            <Step title="Install AerolVM" description="It checks the machine first, then installs a pinned, verified release.">
              <CodeBlock code={install} />
            </Step>
            <Step title="Start the cluster" description="It starts AerolVM, starts the cluster and registers it here.">
              <CodeBlock code={addNode} />
            </Step>
            {access === 'domain' && !manageDNS && (
              <Step
                title="Keep the DNS records yourself"
                description={
                  <>
                    Point <strong>{validDomain(domain) ? domain : '<domain>'}</strong> and{' '}
                    <strong>*.{validDomain(domain) ? domain : '<domain>'}</strong> at the load balancer or the node. Once
                    the cluster exists, tell Lewbox not to write the records:
                  </>
                }
              >
                <CodeBlock code={dnsRecordsOffCommand(slug)} />
              </Step>
            )}
          </Steps>
        </DialogBody>
        <DialogActions>
          <Button plain onClick={() => setStep('configure')}>
            Back
          </Button>
          <Button onClick={onClose}>Done</Button>
        </DialogActions>
      </>
    )
  }

  return (
    <>
      <DialogTitle>Start a new cluster</DialogTitle>
      <DialogDescription>
        A cluster starts on the machine that will be its first node. Choose how it should run, then take the commands
        for that machine.
      </DialogDescription>
      <StepIndicator labels={['Configure', 'Install commands']} current={0} />
      <DialogBody className="space-y-8">
        <div className="grid gap-6 sm:grid-cols-2">
          <Field>
            <Label>Cluster slug</Label>
            <Input
              value={slug}
              placeholder="eu-west"
              invalid={slugProblem}
              onChange={(event) => setSlug(event.currentTarget.value.trim())}
            />
            {slugProblem ? (
              <ErrorMessage>Lowercase letters, digits and hyphens.</ErrorMessage>
            ) : (
              <Description>Customers see the cluster as a region.</Description>
            )}
          </Field>
          <Field>
            <Label>First node’s name</Label>
            <Input
              value={node}
              invalid={nodeProblem}
              onChange={(event) => setNode(event.currentTarget.value.trim())}
            />
            {nodeProblem && <ErrorMessage>Lowercase letters, digits and hyphens.</ErrorMessage>}
          </Field>
        </div>

        <RadioGroup
          label="How customers reach it"
          value={access}
          onChange={setAccess}
          options={accessOptions}
        />

        {access === 'domain' && (
          <div className="space-y-6">
            <div className="grid gap-6 sm:grid-cols-2">
              <Field>
                <Label>Domain</Label>
                <Input
                  value={domain}
                  placeholder="eu.api.example.com"
                  invalid={domainProblem}
                  onChange={(event) => setDomain(event.currentTarget.value.trim())}
                />
                {domainProblem ? (
                  <ErrorMessage>A domain name, such as eu.api.example.com.</ErrorMessage>
                ) : (
                  <Description>Sandboxes get names under it, so it needs a wildcard certificate.</Description>
                )}
              </Field>
              <Field>
                <Label>DNS profile</Label>
                <Listbox value={dnsProfile} onChange={setDNSProfile} placeholder="Choose a profile">
                  {dnsList.map((profile) => (
                    <ListboxOption key={profile.id} value={profile.name}>
                      {profile.name} ({profile.zone})
                    </ListboxOption>
                  ))}
                </Listbox>
                <Description>
                  {dnsList.length === 0 && !dnsProfiles.loading ? (
                    <>
                      No DNS profile yet. Domain mode needs one, to get certificates:{' '}
                      <TextLink href="/admin/settings?tab=dns">add a DNS profile</TextLink>.
                    </>
                  ) : (
                    <>
                      Its zone must contain the domain. <TextLink href="/admin/settings?tab=dns">Settings → DNS profiles</TextLink>
                    </>
                  )}
                </Description>
              </Field>
            </div>
            <Field>
              <Label>ACME email</Label>
              <Input
                value={acmeEmail}
                type="email"
                placeholder={selectedDNS?.acme_email || 'ops@example.com'}
                invalid={emailProblem}
                onChange={(event) => setACMEEmail(event.currentTarget.value.trim())}
              />
              {emailProblem ? (
                <ErrorMessage>An email address.</ErrorMessage>
              ) : (
                <Description>Optional. The profile’s address is used when this is empty.</Description>
              )}
            </Field>
            <Checkbox
              checked={manageDNS}
              onChange={setManageDNS}
              description="Lewbox keeps the domain and *.domain records pointing at the cluster’s nodes. Turn it off when the domain points at a load balancer."
            >
              Lewbox manages DNS records
            </Checkbox>
          </div>
        )}

        {access === 'public-host' && (
          <Field className="max-w-md">
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
        )}

        <Field className="max-w-md">
          <Label>Runtime</Label>
          <Listbox value={effectiveRuntime} onChange={setRuntime}>
            <ListboxOption value="docker">Docker</ListboxOption>
            {access !== 'local' && <ListboxOption value="firecracker">Firecracker</ListboxOption>}
          </Listbox>
          <Description>
            Every node in a cluster uses the same one. Firecracker needs /dev/kvm on the machine and a domain or public
            IP.
          </Description>
        </Field>

        <div className="space-y-6">
          <Field className="max-w-md">
            <Label>Storage profile</Label>
            <Listbox value={storageProfile} onChange={setStorageProfile}>
              <ListboxOption value="">None</ListboxOption>
              {storageList.map((profile) => (
                <ListboxOption key={profile.id} value={profile.name}>
                  {profile.name} ({profile.bucket})
                </ListboxOption>
              ))}
            </Listbox>
            <Description>
              Optional. A bucket for snapshots and archived sandboxes.{' '}
              <TextLink href="/admin/settings?tab=storage">Settings → Storage profiles</TextLink>
            </Description>
          </Field>
          <RadioGroup
            label="When a sandbox stops"
            value={effectivePolicy}
            onChange={setStopPolicy}
            disabled={archiveBlocked !== ''}
            options={[
              { value: 'keep', label: 'Keep it on its server', description: 'The default. The sandbox stays where it ran.' },
              {
                value: 'archive',
                label: 'Archive it to storage',
                description: archiveBlocked
                  ? `Not available: ${archiveBlocked}`
                  : 'The server is freed. Restoring starts a new sandbox with a new ID.',
              },
            ]}
          />
          {archiveBlocked && <Text>Archiving is off. {archiveBlocked}</Text>}
        </div>

        <Disclosure title="Advanced">
          <div className="grid gap-6 sm:grid-cols-2">
            <Field>
              <Label>AerolVM version</Label>
              <Input
                value={version}
                placeholder="v0.7.0"
                invalid={versionProblem}
                onChange={(event) => setVersion(event.currentTarget.value.trim())}
              />
              {versionProblem ? (
                <ErrorMessage>A release tag such as v0.7.0, or latest.</ErrorMessage>
              ) : (
                <Description>Empty uses the tested release. Another version is untested.</Description>
              )}
            </Field>
            <Field>
              <Label>Extra installer flags</Label>
              <Input
                value={extraFlags}
                placeholder="--lb-health-port=8081"
                invalid={flagsProblem}
                onChange={(event) => setExtraFlags(event.currentTarget.value)}
              />
              {flagsProblem ? (
                <ErrorMessage>Only --flag or --flag=value, with letters, digits and . _ : / @ + -</ErrorMessage>
              ) : (
                <Description>Passed to AerolVM’s installer after <code>--</code>.</Description>
              )}
            </Field>
          </div>
        </Disclosure>
        {access === 'public-host' && (
          <Notice variant="warning">Public IP mode has no TLS. Use it for labs, not for customers’ traffic.</Notice>
        )}
      </DialogBody>
      <DialogActions>
        <Button plain onClick={onClose}>
          Cancel
        </Button>
        <Button onClick={() => setStep('commands')} disabled={!ready} disabledReason="Fill in required fields">
          Next: install commands
        </Button>
      </DialogActions>
    </>
  )
}
