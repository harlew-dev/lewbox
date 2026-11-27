// Values the console puts into shell commands for people to paste. Each is
// checked against the same shape the CLI accepts, so nothing typed here can
// add shell syntax to a command; an invalid value shows as a <placeholder>.

// A DNS label: node names and cluster slugs.
export const LABEL = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/

// The published one-line installer, as shown in the console.
export const installScriptCommand =
  'curl -fsSL https://raw.githubusercontent.com/harlew-dev/lewbox/main/install.sh | sh'

// A hostname or an IPv4 or IPv6 address.
const HOST = /^(?=.{1,253}$)[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$|^[0-9A-Fa-f:]{2,39}$/

export type Runtime = 'docker' | 'firecracker'
export type NodeRole = 'mixed' | 'server' | 'worker' | 'ingress'

export function validLabel(value: string): boolean {
  return LABEL.test(value)
}

export function validHost(value: string): boolean {
  return HOST.test(value)
}

// The value if it passes `valid`, otherwise a placeholder such as <slug>.
export function arg(value: string, valid: (value: string) => boolean, placeholder: string): string {
  return valid(value) ? value : `<${placeholder}>`
}

// This server's URL, for `lewbox login`.
export function serverURL(): string {
  return typeof window === 'undefined' ? 'https://lewbox.example.com' : window.location.origin
}

// A domain name with at least one dot.
const DOMAIN = /^(?=.{3,253}$)([A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$/

const EMAIL = /^[A-Za-z0-9._%+-]{1,64}@[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?\.[A-Za-z]{2,}$/

// The same shape as the CLI's release tags; `latest` is the newest release.
const VERSION = /^v[0-9]+\.[0-9]+\.[0-9]+([.+-][0-9A-Za-z.-]+)?$/

// One installer flag: --flag or --flag=value, with safe characters only.
const INSTALLER_FLAG = /^--[a-z0-9][a-z0-9-]*(=[A-Za-z0-9._:/@+-]+)?$/

export function validDomain(value: string): boolean {
  return DOMAIN.test(value)
}

export function validEmail(value: string): boolean {
  return EMAIL.test(value)
}

export function validVersion(value: string): boolean {
  return value === 'latest' || (value.length <= 64 && VERSION.test(value))
}

// Whitespace-separated --flag / --flag=value tokens; empty is valid.
export function validInstallerFlags(value: string): boolean {
  return value.trim() === '' || value.trim().split(/\s+/).every((token) => INSTALLER_FLAG.test(token))
}

export type AccessMode = 'domain' | 'public-host' | 'local'
export type StopPolicy = 'keep' | 'archive'
export type ArchiveRetention = 'instant' | '1h' | '1d' | '7d' | 'never'
export type ArchiveSnapshotName = 'id' | 'name'
const RETENTIONS: readonly string[] = ['instant', '1h', '1d', '7d', 'never']

// The advanced AerolVM settings, kept as strings while the form is edited.
export interface AdvancedForm {
  docker_timeout: string
  max_sandbox_cpu: string
  max_sandbox_memory_mb: string
  max_sandbox_disk_gb: string
  idle_stop_minutes: string
  destroy_if_idle_for: string
  max_age: string
  firecracker_vm_slots: string
}

export const emptyAdvancedForm: AdvancedForm = {
  docker_timeout: '',
  max_sandbox_cpu: '',
  max_sandbox_memory_mb: '',
  max_sandbox_disk_gb: '',
  idle_stop_minutes: '',
  destroy_if_idle_for: '',
  max_age: '',
  firecracker_vm_slots: '',
}

export const ADVANCED_FIELDS: readonly (keyof AdvancedForm)[] = [
  'docker_timeout',
  'max_sandbox_cpu',
  'max_sandbox_memory_mb',
  'max_sandbox_disk_gb',
  'idle_stop_minutes',
  'destroy_if_idle_for',
  'max_age',
  'firecracker_vm_slots',
]

// A Go duration in hours, minutes and seconds: 15m, 1h30m, 90s.
function durationMinutes(value: string): number | null {
  const match = value.trim().match(/^(?:(\d+(?:\.\d+)?)h)?(?:(\d+(?:\.\d+)?)m)?(?:(\d+(?:\.\d+)?)s)?$/)
  if (!match) return null
  const hours = Number(match[1] ?? 0)
  const minutes = Number(match[2] ?? 0)
  const seconds = Number(match[3] ?? 0)
  if (hours === 0 && minutes === 0 && seconds === 0) return null
  return hours * 60 + minutes + seconds / 60
}

function validDuration(value: string, minMinutes: number, maxMinutes: number): boolean {
  const minutes = durationMinutes(value)
  return minutes !== null && minutes >= minMinutes && minutes <= maxMinutes
}

function validNumber(value: string, min: number, max = Number.POSITIVE_INFINITY, integer = false): boolean {
  if (value.trim() === '') return false
  const parsed = Number(value)
  return Number.isFinite(parsed) && parsed >= min && parsed <= max && (!integer || Number.isInteger(parsed))
}

// What is wrong with each filled-in advanced setting. Empty fields are valid:
// they keep the default or turn the setting off.
export function advancedProblems(
  values: AdvancedForm,
  runtime: Runtime,
): Partial<Record<keyof AdvancedForm, string>> {
  const problems: Partial<Record<keyof AdvancedForm, string>> = {}
  const text = (field: keyof AdvancedForm) => values[field].trim()
  if (text('docker_timeout') !== '' && !validDuration(text('docker_timeout'), 1, 120)) {
    problems.docker_timeout = 'A duration from 1m to 2h, such as 15m.'
  }
  if (text('max_sandbox_cpu') !== '' && !validNumber(text('max_sandbox_cpu'), 1)) {
    problems.max_sandbox_cpu = 'At least 1, AerolVM’s default size.'
  }
  if (text('max_sandbox_memory_mb') !== '' && !validNumber(text('max_sandbox_memory_mb'), 1024, undefined, true)) {
    problems.max_sandbox_memory_mb = 'At least 1024, AerolVM’s default size.'
  }
  if (text('max_sandbox_disk_gb') !== '' && !validNumber(text('max_sandbox_disk_gb'), 10, undefined, true)) {
    problems.max_sandbox_disk_gb = 'At least 10, AerolVM’s default size.'
  }
  if (text('idle_stop_minutes') !== '' && !validNumber(text('idle_stop_minutes'), 1, undefined, true)) {
    problems.idle_stop_minutes = 'A whole number of minutes.'
  }
  if (text('destroy_if_idle_for') !== '' && !validDuration(text('destroy_if_idle_for'), 0, 720 * 60)) {
    problems.destroy_if_idle_for = 'A duration of at most 720h, such as 24h.'
  }
  if (text('max_age') !== '' && !validDuration(text('max_age'), 0, 720 * 60)) {
    problems.max_age = 'A duration of at most 720h, such as 24h.'
  }
  if (
    runtime === 'firecracker' &&
    text('firecracker_vm_slots') !== '' &&
    !validNumber(text('firecracker_vm_slots'), 256, 1024, true)
  ) {
    problems.firecracker_vm_slots = '256–1024, and only above the current value.'
  }
  return problems
}

// The advanced flags for a command, only for fields that were filled in.
function advancedFlags(values: AdvancedForm, runtime: Runtime): string[] {
  const parts: string[] = []
  const add = (value: string, valid: boolean, flag: string) => {
    const trimmed = value.trim()
    if (trimmed === '') return
    parts.push(`--${flag} ${valid ? trimmed : `<${flag}>`}`)
  }
  add(values.docker_timeout, validDuration(values.docker_timeout, 1, 120), 'docker-timeout')
  add(values.max_sandbox_cpu, validNumber(values.max_sandbox_cpu, 1), 'max-sandbox-cpu')
  add(
    values.max_sandbox_memory_mb,
    validNumber(values.max_sandbox_memory_mb, 1024, undefined, true),
    'max-sandbox-memory-mb',
  )
  add(values.max_sandbox_disk_gb, validNumber(values.max_sandbox_disk_gb, 10, undefined, true), 'max-sandbox-disk-gb')
  add(values.idle_stop_minutes, validNumber(values.idle_stop_minutes, 1, undefined, true), 'idle-stop-minutes')
  add(values.destroy_if_idle_for, validDuration(values.destroy_if_idle_for, 0, 720 * 60), 'destroy-if-idle-for')
  add(values.max_age, validDuration(values.max_age, 0, 720 * 60), 'max-age')
  if (runtime === 'firecracker') {
    add(
      values.firecracker_vm_slots,
      validNumber(values.firecracker_vm_slots, 256, 1024, true),
      'firecracker-vm-slots',
    )
  }
  return parts
}


export interface InstallOptions {
  access: AccessMode
  // Public IP mode.
  host: string
  // Domain mode.
  domain: string
  dnsProfile: string
  // Optional; the DNS profile's address is used when empty.
  acmeEmail: string
  runtime: Runtime
  // The node's name, passed so install and node add agree.
  node: string
  // Optional AerolVM version.
  version: string
  // Optional extra installer flags, passed after `--`.
  extraFlags: string
}

// `lewbox install …` with exactly the chosen options. Every value is checked;
// an invalid one is shown as a <placeholder>.
export function installCommand(options: InstallOptions): string {
  const parts = ['lewbox install']
  if (options.access === 'domain') {
    parts.push(`--domain ${arg(options.domain, validDomain, 'domain')}`)
    parts.push(`--dns-profile ${arg(options.dnsProfile, validLabel, 'dns-profile')}`)
    if (options.acmeEmail !== '') parts.push(`--acme-email ${arg(options.acmeEmail, validEmail, 'acme-email')}`)
  } else if (options.access === 'public-host') {
    parts.push(`--public-host ${arg(options.host, validHost, 'public-host')}`)
  }
  parts.push(`--runtime ${options.runtime === 'firecracker' ? 'firecracker' : 'docker'}`)
  if (options.node !== '') parts.push(`--name ${arg(options.node, validLabel, 'node-name')}`)
  if (options.version !== '') parts.push(`--aerolvm-version ${arg(options.version, validVersion, 'version')}`)
  const extra = options.extraFlags.trim()
  if (extra !== '') parts.push(`-- ${validInstallerFlags(extra) ? extra.split(/\s+/).join(' ') : '<installer-flags>'}`)
  return parts.join(' ')
}

export interface NewClusterOptions {
  node: string
  slug: string
  // Empty for none.
  storageProfile: string
  stopPolicy: StopPolicy
  // Only sent when archiving and not the default (instant, id).
  retention: ArchiveRetention
  snapshotName: ArchiveSnapshotName
  // Advanced settings, only the filled-in ones are passed.
  runtime?: Runtime
  advanced?: AdvancedForm
}

// `lewbox node add … --new-cluster …`, with the storage options when chosen.
export function newClusterCommand(options: NewClusterOptions): string {
  const parts = [
    `lewbox node add ${arg(options.node, validLabel, 'node-name')}`,
    `--new-cluster ${arg(options.slug, validLabel, 'slug')}`,
  ]
  if (options.storageProfile !== '') {
    parts.push(`--storage-profile ${arg(options.storageProfile, validLabel, 'storage-profile')}`)
  }
  if (options.stopPolicy === 'archive') {
    parts.push('--stop-policy archive')
    if (options.retention !== 'instant' && RETENTIONS.includes(options.retention)) {
      parts.push(`--archive-local-retention ${options.retention}`)
    }
    if (options.snapshotName === 'name') parts.push('--archive-snapshot-name name')
  }
  parts.push(...advancedFlags(options.advanced ?? emptyAdvancedForm, options.runtime ?? 'docker'))
  return parts.join(' ')
}

// `lewbox node add … --cluster … --role …`.
export function joinClusterCommand(options: { node: string; slug: string; role: NodeRole }): string {
  const role = ['mixed', 'server', 'worker', 'ingress'].includes(options.role) ? options.role : 'mixed'
  return `lewbox node add ${arg(options.node, validLabel, 'node-name')} --cluster ${arg(options.slug, validLabel, 'slug')} --role ${role}`
}

// Turns off Lewbox's DNS records for a cluster pointed at a load balancer.
export function dnsRecordsOffCommand(slug: string): string {
  return `lewbox cluster set ${arg(slug, validLabel, 'slug')} --dns-records off`
}

// Applies the cluster's storage and DNS settings on a node.
export const nodeConfigureCommand = 'lewbox node configure'
