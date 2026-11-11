// Resolve a markdown link found on a docs page. The Go server serves docs
// from the repository's docs/ folder, and the home page is README.md.

export type DocsLink =
  | { kind: 'internal'; to: string }
  | { kind: 'hash'; href: string }
  | { kind: 'external'; href: string }

const REPO_BLOB = 'https://github.com/harlew-dev/lewbox/blob/main/'
const SCHEME = /^[a-z][a-z\d+.-]*:/i

function currentFile(pagePath: string): string {
  return pagePath === '' ? 'README.md' : `${pagePath}.md`
}

export function resolveDocsLink(href: string, pagePath: string): DocsLink {
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

  // Start at the docs folder's place in the repository so a link can be
  // recognised as either a docs page or a file elsewhere in the repo.
  const parts = ['docs', ...currentFile(pagePath).split('/').slice(0, -1)]
  for (const segment of relative.split('/')) {
    if (segment === '' || segment === '.') continue
    if (segment === '..') {
      if (parts.length > 0) parts.pop()
      continue
    }
    parts.push(segment)
  }

  if (parts[0] !== 'docs' || parts.length === 1) {
    return { kind: 'external', href: `${REPO_BLOB}${parts.join('/')}${query}${hash}` }
  }

  let docsPath = parts.slice(1).join('/')
  if (docsPath === 'README.md') docsPath = ''
  else if (docsPath.endsWith('/README.md')) docsPath = docsPath.slice(0, -'/README.md'.length)
  else if (docsPath.endsWith('.md')) docsPath = docsPath.slice(0, -'.md'.length)

  const to = `/docs${docsPath ? `/${docsPath}` : ''}${query}${hash}`
  return { kind: 'internal', to }
}
