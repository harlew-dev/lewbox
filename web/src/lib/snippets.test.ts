import { describe, expect, it } from 'vitest'
import { sandboxSnippets } from './snippets'

const region = { slug: 'eu-west', name: 'EU West', endpoint_url: 'https://eu-west.api.acme.dev/', runtime: 'docker' }

const key = {
  env: { SB_API_URL: 'https://eu-west.api.acme.dev', SB_API_TOKEN: 'avm_secret' },
  e2b_env: {
    E2B_API_URL: 'https://eu-west.api.acme.dev/e2b',
    E2B_SANDBOX_URL: 'https://eu-west.api.acme.dev/e2b/runtime',
    E2B_API_KEY: 'avm_secret',
  },
}

describe('sandboxSnippets', () => {
  it('offers E2B only with the key’s E2B variables from the API', () => {
    expect(sandboxSnippets(region, key).tabs.map((tab) => tab.label)).toEqual(['TypeScript', 'Python', 'E2B', 'curl'])
    expect(sandboxSnippets(region, { env: key.env }).tabs.map((tab) => tab.label)).toEqual(['TypeScript', 'Python', 'curl'])
    expect(sandboxSnippets(region).tabs.map((tab) => tab.label)).toEqual(['TypeScript', 'Python', 'curl'])
  })

  it('asks for the firecracker runtime where the region runs it', () => {
    const [ts, py] = sandboxSnippets({ ...region, runtime: 'firecracker' }).tabs
    expect(ts!.code).toContain('runtime: "firecracker"')
    expect(py!.code).toContain('"runtime": "firecracker"')
    expect(sandboxSnippets(region).tabs[0]!.code).not.toContain('runtime')
  })

  it('puts the key’s variables in the environment and the E2B example', () => {
    const { env, tabs } = sandboxSnippets(region, key)
    expect(env).toBe('export SB_API_URL=https://eu-west.api.acme.dev\nexport SB_API_TOKEN=avm_secret')
    expect(tabs[2]!.code).toContain('// E2B_API_URL=https://eu-west.api.acme.dev/e2b')
    expect(tabs[2]!.code).toContain('// E2B_SANDBOX_URL=https://eu-west.api.acme.dev/e2b/runtime')
    expect(tabs[2]!.code).toContain('// E2B_API_KEY=avm_secret')
  })

  it('uses the region’s endpoint and a placeholder without a key', () => {
    expect(sandboxSnippets(region).env).toBe('export SB_API_URL=https://eu-west.api.acme.dev\nexport SB_API_TOKEN=<your API key>')
  })
})
