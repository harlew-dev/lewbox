import { describe, expect, it } from 'vitest'
import {
  dnsRecordsOffCommand,
  installCommand,
  joinClusterCommand,
  newClusterCommand,
  validDomain,
  validEmail,
  validInstallerFlags,
  validVersion,
  type InstallOptions,
} from './commands'

const base: InstallOptions = {
  access: 'local',
  host: '',
  domain: '',
  dnsProfile: '',
  acmeEmail: '',
  runtime: 'docker',
  node: 'node-1',
  version: '',
  extraFlags: '',
}

describe('installCommand', () => {
  it('installs in local mode', () => {
    expect(installCommand(base)).toBe('lewbox install --runtime docker --name node-1')
  })

  it('installs on a public IP', () => {
    expect(installCommand({ ...base, access: 'public-host', host: '10.0.0.5', runtime: 'firecracker' })).toBe(
      'lewbox install --public-host 10.0.0.5 --runtime firecracker --name node-1',
    )
  })

  it('installs a domain with a DNS profile, and the email only when set', () => {
    const domain = { ...base, access: 'domain' as const, domain: 'eu.api.example.com', dnsProfile: 'cf-prod' }
    expect(installCommand(domain)).toBe(
      'lewbox install --domain eu.api.example.com --dns-profile cf-prod --runtime docker --name node-1',
    )
    expect(installCommand({ ...domain, acmeEmail: 'ops@example.com' })).toContain(
      '--dns-profile cf-prod --acme-email ops@example.com --runtime',
    )
  })

  it('adds the version and installer flags last', () => {
    expect(
      installCommand({ ...base, version: 'v0.7.1', extraFlags: '--lb-health-port=8081  --no-foo' }),
    ).toBe('lewbox install --runtime docker --name node-1 --aerolvm-version v0.7.1 -- --lb-health-port=8081 --no-foo')
    expect(installCommand({ ...base, version: 'latest' })).toContain('--aerolvm-version latest')
  })

  it('shows a placeholder for every invalid value', () => {
    const command = installCommand({
      ...base,
      access: 'domain',
      domain: 'x; rm -rf /',
      dnsProfile: 'Bad Name',
      acmeEmail: 'a b@c',
      node: '$(id)',
      version: '1.0; id',
      extraFlags: '--ok $(id)',
    })
    expect(command).toBe(
      'lewbox install --domain <domain> --dns-profile <dns-profile> --acme-email <acme-email> --runtime docker --name <node-name> --aerolvm-version <version> -- <installer-flags>',
    )
    expect(installCommand({ ...base, access: 'public-host', host: 'a b' })).toContain('--public-host <public-host>')
  })
})

describe('newClusterCommand', () => {
  const defaults = { retention: 'instant', snapshotName: 'id' } as const

  it('starts a cluster, with storage options only when chosen', () => {
    expect(newClusterCommand({ ...defaults, node: 'node-1', slug: 'eu-west', storageProfile: '', stopPolicy: 'keep' })).toBe(
      'lewbox node add node-1 --new-cluster eu-west',
    )
    expect(
      newClusterCommand({ ...defaults, node: 'node-1', slug: 'eu-west', storageProfile: 's3-main', stopPolicy: 'keep' }),
    ).toBe('lewbox node add node-1 --new-cluster eu-west --storage-profile s3-main')
    expect(
      newClusterCommand({ ...defaults, node: 'node-1', slug: 'eu-west', storageProfile: 's3-main', stopPolicy: 'archive' }),
    ).toBe('lewbox node add node-1 --new-cluster eu-west --storage-profile s3-main --stop-policy archive')
  })

  it('adds the archive settings only when archiving and not the default', () => {
    const base = { node: 'node-1', slug: 'lab', storageProfile: 's3-main' }
    expect(newClusterCommand({ ...base, stopPolicy: 'archive', retention: '1d', snapshotName: 'name' })).toBe(
      'lewbox node add node-1 --new-cluster lab --storage-profile s3-main --stop-policy archive --archive-local-retention 1d --archive-snapshot-name name',
    )
    expect(newClusterCommand({ ...base, stopPolicy: 'keep', retention: '1d', snapshotName: 'name' })).toBe(
      'lewbox node add node-1 --new-cluster lab --storage-profile s3-main',
    )
    // A value from outside the list never reaches the command.
    const bad = { ...base, stopPolicy: 'archive', retention: '1d; rm -rf /', snapshotName: 'id' } as never
    expect(newClusterCommand(bad)).toBe('lewbox node add node-1 --new-cluster lab --storage-profile s3-main --stop-policy archive')
  })

  it('shows placeholders for bad input', () => {
    expect(newClusterCommand({ ...defaults, node: '', slug: 'A b', storageProfile: '$x', stopPolicy: 'keep' })).toBe(
      'lewbox node add <node-name> --new-cluster <slug> --storage-profile <storage-profile>',
    )
  })

  it('passes only the advanced settings that were filled in', () => {
    const advanced = {
      docker_timeout: '1h', max_sandbox_cpu: '4', max_sandbox_memory_mb: '4096', max_sandbox_disk_gb: '',
      idle_stop_minutes: '30', destroy_if_idle_for: '24h', max_age: '', firecracker_vm_slots: '512',
    }
    expect(
      newClusterCommand({ ...defaults, node: 'node-1', slug: 'lab', storageProfile: '', stopPolicy: 'keep', runtime: 'firecracker', advanced }),
    ).toBe(
      'lewbox node add node-1 --new-cluster lab --docker-timeout 1h --max-sandbox-cpu 4 --max-sandbox-memory-mb 4096 --idle-stop-minutes 30 --destroy-if-idle-for 24h --firecracker-vm-slots 512',
    )
    // The pool is Firecracker-only; below 256 or above 1024 is a placeholder.
    expect(
      newClusterCommand({ ...defaults, node: 'node-1', slug: 'lab', storageProfile: '', stopPolicy: 'keep', runtime: 'docker', advanced }),
    ).not.toContain('--firecracker-vm-slots')
    expect(
      newClusterCommand({
        ...defaults, node: 'node-1', slug: 'lab', storageProfile: '', stopPolicy: 'keep', runtime: 'firecracker',
        advanced: { ...advanced, max_sandbox_cpu: '0.5', firecracker_vm_slots: '128' },
      }),
    ).toContain('--max-sandbox-cpu <max-sandbox-cpu>')
  })
})

describe('joinClusterCommand and dnsRecordsOffCommand', () => {
  it('builds the join and the records-off commands', () => {
    expect(joinClusterCommand({ node: 'node-2', slug: 'eu-west', role: 'worker' })).toBe(
      'lewbox node add node-2 --cluster eu-west --role worker',
    )
    expect(joinClusterCommand({ node: '', slug: 'eu-west', role: 'mixed' })).toContain('<node-name>')
    expect(dnsRecordsOffCommand('eu-west')).toBe('lewbox cluster set eu-west --dns-records off')
    expect(dnsRecordsOffCommand('')).toBe('lewbox cluster set <slug> --dns-records off')
  })
})

describe('validators', () => {
  it('accepts and rejects', () => {
    expect(validDomain('eu.api.example.com')).toBe(true)
    expect(validDomain('localhost')).toBe(false)
    expect(validDomain('a.com; ls')).toBe(false)
    expect(validEmail('ops@example.com')).toBe(true)
    expect(validEmail('ops@example')).toBe(false)
    expect(validVersion('v0.7.0')).toBe(true)
    expect(validVersion('latest')).toBe(true)
    expect(validVersion('0.7.0')).toBe(false)
    expect(validInstallerFlags('')).toBe(true)
    expect(validInstallerFlags('--a --b=c')).toBe(true)
    expect(validInstallerFlags('-a')).toBe(false)
    expect(validInstallerFlags('--a=b;c')).toBe(false)
    expect(validInstallerFlags('--a b')).toBe(false)
  })
})
