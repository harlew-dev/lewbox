import { describe, expect, it } from 'vitest'
import { sandboxSnippets } from './snippets'

const region = { slug: 'eu-west', name: 'EU West', endpoint_url: 'https://eu-west.api.acme.dev/', runtime: 'docker' }

describe('sandboxSnippets', () => {
  it('offers E2B on docker regions only', () => {
    expect(sandboxSnippets(region).tabs.map((tab) => tab.label)).toEqual(['TypeScript', 'Python', 'E2B', 'curl'])
    const firecracker = sandboxSnippets({ ...region, runtime: 'firecracker' })
    expect(firecracker.tabs.map((tab) => tab.label)).toEqual(['TypeScript', 'Python', 'curl'])
  })

  it('asks for the firecracker runtime where the region runs it', () => {
    const [ts, py] = sandboxSnippets({ ...region, runtime: 'firecracker' }).tabs
    expect(ts!.code).toContain('runtime: "firecracker"')
    expect(py!.code).toContain('"runtime": "firecracker"')
    expect(sandboxSnippets(region).tabs[0]!.code).not.toContain('runtime')
  })

  it('puts the key and endpoint in the environment', () => {
    const { env, tabs } = sandboxSnippets(region, 'avm_secret')
    expect(env).toBe('export SB_API_URL=https://eu-west.api.acme.dev\nexport SB_API_TOKEN=avm_secret')
    expect(tabs[2]!.code).toContain('// E2B_API_URL=https://eu-west.api.acme.dev/e2b')
    expect(tabs[2]!.code).toContain('// E2B_API_KEY=avm_secret')
  })
})
