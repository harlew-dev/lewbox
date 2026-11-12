// Resolve a markdown link found on a docs page. The Go server serves two
// books from the repository's docs/ folder: consumer/ at /docs and
// platform/ at /platform. Each book's README.md is its home page.

import type { DocsBook } from './api'

export type DocsLink =
  | { kind: 'internal'; to: string }
  | { kind: 'hash'; href: string }
  | { kind: 'external'; href: string }

const REPO_BLOB = 'https://github.com/harlew-dev/lewbox/blob/main/'
const SCHEME = /^[a-z][a-z\d+.-]*:/i

const books: Record<DocsBook, string> = {
  consumer: '/docs',
  platform: '/platform',
}

function bookFor(dir: string): DocsBook | null {
  if (dir === 'consumer' || dir === 'platform') return dir
  return null
}

function currentFile(pagePath: string): string {
  return pagePath === '' ? 'README.md' : `${pagePath}.md`
}

export function resolveDocsLink(
  href: string,
  pagePath: string,
  book: DocsBook = 'consumer',
): DocsLink {
  if (href.startsWith('#')) return { kind: 'hash', href }

  if (href.startsWith('//') || SCHEME.test(href)) {
    return { kind: 'external', href }
  }

  const hashAt = href.indexOf('#')
  const hash = hashAt === -1 ? '' : href.slice(hashAt)
  const withoutHash = hashAt === -1 ? href : href.slice(0, hashAt)
  const queryAt = withoutHash.indexOf('?')
  const query = queryAt === -1 ? '' : withoutHash.slice(queryAt)
  const relative = queryAt === -1 ? withoutHash : withoutHash.slice(0, queryAt)

  if (relative === '') return { kind: 'hash', href: hash || '#' }

  // Start at the page's place in the repository so a link can be recognised
  // as either a page in one of the books or a file elsewhere in the repo.
  const parts = ['docs', book, ...currentFile(pagePath).split('/').slice(0, -1)]
  for (const segment of relative.split('/')) {
    if (segment === '' || segment === '.') continue
    if (segment === '..') {
      if (parts.length > 0) parts.pop()
      continue
    }
    parts.push(segment)
  }

  const target = parts[1] ? bookFor(parts[1]) : null
  if (parts[0] !== 'docs' || target === null || parts.length === 2) {
    return { kind: 'external', href: `${REPO_BLOB}${parts.join('/')}${query}${hash}` }
  }

  let docsPath = parts.slice(2).join('/')
  if (docsPath === 'README.md') docsPath = ''
  else if (docsPath.endsWith('/README.md')) docsPath = docsPath.slice(0, -'/README.md'.length)
  else if (docsPath.endsWith('.md')) docsPath = docsPath.slice(0, -'.md'.length)

  const to = `${books[target]}${docsPath ? `/${docsPath}` : ''}${query}${hash}`
  return { kind: 'internal', to }
}
