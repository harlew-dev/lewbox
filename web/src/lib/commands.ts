// Values the console puts into shell commands for people to paste. Each is
// checked against the same shape the CLI accepts, so nothing typed here can
// add shell syntax to a command; an invalid value shows as a <placeholder>.

// A DNS label: node names and cluster slugs.
export const LABEL = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/

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

// Installs the latest Lewbox on Ubuntu or Debian with Go's own installer. A
// node doesn't need the web console, which `go install` can't build.
export const installCLICommand =
  'sudo apt install -y golang-go && export PATH="$PATH:$HOME/go/bin" && go install github.com/harlew-dev/lewbox/cmd/lewbox@latest && lewbox version'
