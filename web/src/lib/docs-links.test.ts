import { describe, expect, it } from 'vitest'
import { resolveDocsLink } from './docs-links'

const repo = 'https://github.com/harlew-dev/lewbox/blob/main/'

describe('resolveDocsLink', () => {
  it('resolves relative .md links in the consumer book', () => {
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

  it('resolves links in the platform book to /platform', () => {
    expect(resolveDocsLink('getting-started/quickstart.md', '', 'platform')).toEqual({
      kind: 'internal',
      to: '/platform/getting-started/quickstart',
    })
    expect(resolveDocsLink('../reference/http-api.md', 'accounts/sign-in', 'platform')).toEqual({
      kind: 'internal',
      to: '/platform/reference/http-api',
    })
  })

  it('turns each book’s README into its home page', () => {
    expect(resolveDocsLink('README.md', 'reference/cli')).toEqual({
      kind: 'internal',
      to: '/docs/reference',
    })
    expect(resolveDocsLink('../README.md', 'reference/cli')).toEqual({
      kind: 'internal',
      to: '/docs',
    })
    expect(resolveDocsLink('../README.md', 'reference/cli', 'platform')).toEqual({
      kind: 'internal',
      to: '/platform',
    })
  })

  it('crosses between the two books', () => {
    expect(resolveDocsLink('../platform/README.md', '')).toEqual({
      kind: 'internal',
      to: '/platform',
    })
    expect(resolveDocsLink('../consumer/README.md', '', 'platform')).toEqual({
      kind: 'internal',
      to: '/docs',
    })
  })

  it('sends links outside the books to GitHub', () => {
    expect(resolveDocsLink('../../AGENTS.md', '')).toEqual({
      kind: 'external',
      href: `${repo}AGENTS.md`,
    })
    expect(resolveDocsLink('../../../deploy/dev-cluster/README.md', 'reference/cli')).toEqual({
      kind: 'external',
      href: `${repo}deploy/dev-cluster/README.md`,
    })
    expect(resolveDocsLink('../../MVP.md', '')).toEqual({
      kind: 'external',
      href: `${repo}MVP.md`,
    })
    expect(resolveDocsLink('../design/aerolvm-integration.md', '')).toEqual({
      kind: 'external',
      href: `${repo}docs/design/aerolvm-integration.md`,
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
