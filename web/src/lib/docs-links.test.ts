import { describe, expect, it } from 'vitest'
import { resolveDocsLink } from './docs-links'

const repo = 'https://github.com/harlew-dev/lewbox/blob/main/'

describe('resolveDocsLink', () => {
  it('resolves relative .md links against the page path', () => {
    expect(resolveDocsLink('getting-started/quickstart.md', '')).toEqual({
      kind: 'internal',
      to: '/docs/getting-started/quickstart',
    })
    expect(resolveDocsLink('../setup/requirements.md', 'getting-started/quickstart')).toEqual({
      kind: 'internal',
      to: '/docs/setup/requirements',
    })
    expect(resolveDocsLink('../reference/cli.md#errors', 'clusters/start-a-cluster')).toEqual({
      kind: 'internal',
      to: '/docs/reference/cli#errors',
    })
  })

  it('turns the docs README into the home page', () => {
    expect(resolveDocsLink('README.md', 'reference/cli')).toEqual({
      kind: 'internal',
      to: '/docs/reference',
    })
    expect(resolveDocsLink('../README.md', 'reference/cli')).toEqual({
      kind: 'internal',
      to: '/docs',
    })
  })

  it('sends links outside the docs folder to GitHub', () => {
    expect(resolveDocsLink('../AGENTS.md', '')).toEqual({
      kind: 'external',
      href: `${repo}AGENTS.md`,
    })
    expect(resolveDocsLink('../../deploy/dev-cluster/README.md', 'reference/cli')).toEqual({
      kind: 'external',
      href: `${repo}deploy/dev-cluster/README.md`,
    })
    expect(resolveDocsLink('../MVP.md', '')).toEqual({
      kind: 'external',
      href: `${repo}MVP.md`,
    })
  })

  it('leaves external URLs and fragments alone', () => {
    expect(resolveDocsLink('https://github.com/aerol-ai/microvm', '')).toEqual({
      kind: 'external',
      href: 'https://github.com/aerol-ai/microvm',
    })
    expect(resolveDocsLink('#errors', 'reference/http-api')).toEqual({
      kind: 'hash',
      href: '#errors',
    })
    expect(resolveDocsLink('mailto:ops@example.com', '')).toEqual({
      kind: 'external',
      href: 'mailto:ops@example.com',
    })
  })
})
