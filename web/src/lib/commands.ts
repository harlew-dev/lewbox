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

export function installCommand(host: string, runtime: Runtime): string {
  return `lewbox install --public-host ${arg(host, validHost, 'public-host')} --runtime ${runtime}`
}
