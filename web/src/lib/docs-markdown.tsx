import {
  cloneElement,
  isValidElement,
  type ReactElement,
  type ReactNode,
} from 'react'
import GithubSlugger from 'github-slugger'
import { CodeBlock, Notice } from '@harlew-dev/ui'
import { Link } from 'react-router'
import type { Components } from 'react-markdown'
import { resolveDocsLink } from '../lib/docs-links'

// Minimal hast shapes, so the plugin needs no extra type packages.
interface HastNode {
  type: string
  tagName?: string
  value?: string
  properties?: Record<string, unknown>
  children?: HastNode[]
}

function hastText(node: HastNode): string {
  if (node.type === 'text') return node.value ?? ''
  return (node.children ?? []).map(hastText).join('')
}

// rehypeHeadingIds gives h2-h4 the ids GitHub would, while the markdown is
// parsed. The ids live in the tree, so they don't depend on how often React
// renders the heading components. One slugger per parse, so duplicates get
// -1, -2 like GitHub.
export function rehypeHeadingIds() {
  return (tree: HastNode) => {
    const slugger = new GithubSlugger()
    const visit = (node: HastNode) => {
      if (node.type === 'element' && /^h[2-4]$/.test(node.tagName ?? '')) {
        node.properties = { ...node.properties, id: slugger.slug(hastText(node)) }
      }
      node.children?.forEach(visit)
    }
    visit(tree)
  }
}

export interface DocHeading {
  level: 2 | 3 | 4
  text: string
  id: string
}

const FENCE = /^\s*(```|~~~)/
const HEADING = /^(#{2,4})\s+(.+?)\s*#*\s*$/

function stripInline(value: string): string {
  return value
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
    .replace(/`([^`]*)`/g, '$1')
    .replace(/\*\*([^*]+)\*\*/g, '$1')
    .replace(/__([^_]+)__/g, '$1')
    .replace(/\*([^*]+)\*/g, '$1')
    .replace(/_([^_]+)_/g, '$1')
    .replace(/<[^>]+>/g, '')
}

// The ids GitHub would give the page's headings, in document order. One
// slugger for the page, so duplicate headings get -1, -2 like GitHub.
export function extractHeadings(markdown: string): DocHeading[] {
  const slugger = new GithubSlugger()
  const headings: DocHeading[] = []
  let fence: string | null = null

  for (const line of markdown.split('\n')) {
    const fenceMatch = line.match(FENCE)
    if (fenceMatch) {
      const marker = fenceMatch[1] ?? ''
      if (fence === null) fence = marker
      else if (fence === marker) fence = null
      continue
    }
    if (fence !== null) continue

    const match = line.match(HEADING)
    if (!match) continue
    const hashes = match[1] ?? ''
    const text = stripInline((match[2] ?? '').trim())
    if (text === '') continue
    headings.push({
      level: hashes.length as 2 | 3 | 4,
      text,
      id: slugger.slug(text),
    })
  }
  return headings
}

function nodeText(node: ReactNode): string {
  if (typeof node === 'string' || typeof node === 'number') return String(node)
  if (Array.isArray(node)) return node.map(nodeText).join('')
  if (isValidElement<{ children?: ReactNode }>(node)) {
    return nodeText(node.props.children)
  }
  return ''
}

const ALERT = /^\s*\[!(NOTE|TIP|WARNING|IMPORTANT|CAUTION)\][ \t]*\n?/
const ALERT_VARIANTS = {
  NOTE: 'info',
  TIP: 'success',
  WARNING: 'warning',
  IMPORTANT: 'info',
  CAUTION: 'error',
} as const

type AlertKind = keyof typeof ALERT_VARIANTS

function firstText(node: ReactNode): string {
  if (typeof node === 'string') return node
  if (Array.isArray(node)) {
    for (const child of node) {
      const found = firstText(child)
      if (found.trim() !== '') return found
    }
    return ''
  }
  if (isValidElement<{ children?: ReactNode }>(node)) return firstText(node.props.children)
  return ''
}

function alertKind(node: ReactNode): AlertKind | null {
  const match = firstText(node).match(ALERT)
  return match ? ((match[1] ?? 'NOTE') as AlertKind) : null
}

interface StripResult {
  node: ReactNode
  done: boolean
}

function stripAlert(node: ReactNode): StripResult {
  if (typeof node === 'string') {
    const match = node.match(ALERT)
    if (match) return { node: node.slice(match[0].length), done: true }
    return { node, done: false }
  }
  if (Array.isArray(node)) {
    const result: ReactNode[] = []
    let done = false
    for (const child of node) {
      if (done) {
        result.push(child)
        continue
      }
      const stripped = stripAlert(child)
      result.push(stripped.node)
      done = stripped.done
    }
    return { node: result, done }
  }
  if (isValidElement<{ children?: ReactNode }>(node)) {
    const stripped = stripAlert(node.props.children)
    if (!stripped.done) return { node, done: false }
    return {
      node: cloneElement(
        node as ReactElement<{ children?: ReactNode }>,
        undefined,
        stripped.node,
      ),
      done: true,
    }
  }
  return { node, done: false }
}

function MarkdownBlockquote({ children }: { children?: ReactNode }) {
  const kind = alertKind(children)
  if (!kind) return <blockquote>{children}</blockquote>
  const stripped = stripAlert(children)
  return (
    <Notice variant={ALERT_VARIANTS[kind]} className="my-5 [&_p]:my-0!">
      {stripped.node}
    </Notice>
  )
}

function flattenText(node: ReactNode): string {
  if (typeof node === 'string' || typeof node === 'number') return String(node)
  if (Array.isArray(node)) return node.map(flattenText).join('')
  if (isValidElement<{ children?: ReactNode }>(node)) {
    return flattenText(node.props.children)
  }
  return ''
}

function MarkdownCode({ children }: { children?: ReactNode }) {
  const text = flattenText(children).replace(/\n$/, '')
  return <CodeBlock code={text}>{children}</CodeBlock>
}

function MarkdownHeading({
  level,
  id,
  children,
}: {
  level: 2 | 3 | 4
  id: string
  children?: ReactNode
}) {
  const Element = `h${level}` as const
  const text = nodeText(children)
  return (
    <Element id={id} className="group">
      {children}
      <a
        href={`#${id}`}
        aria-label={`Link to ${text}`}
        className="ml-2 text-zinc-400 opacity-0 transition-opacity group-hover:opacity-100 focus:opacity-100 dark:text-zinc-500"
      >
        #
      </a>
    </Element>
  )
}

export function markdownComponents(pagePath: string): Components {
  const heading = (level: 2 | 3 | 4) =>
    function Heading({ id, children }: { id?: string; children?: ReactNode }) {
      return (
        <MarkdownHeading level={level} id={id ?? ''}>
          {children}
        </MarkdownHeading>
      )
    }

  return {
    h2: heading(2),
    h3: heading(3),
    h4: heading(4),
    blockquote: MarkdownBlockquote,
    pre: MarkdownCode,
    a: function MarkdownLink({
      href,
      children,
    }: {
      href?: string
      children?: ReactNode
    }) {
      const resolved = resolveDocsLink(href ?? '', pagePath)
      if (resolved.kind === 'internal') {
        return <Link to={resolved.to}>{children}</Link>
      }
      if (resolved.kind === 'external') {
        if (/^(https?:)?\/\//i.test(resolved.href)) {
          return (
            <a href={resolved.href} target="_blank" rel="noreferrer">
              {children}
            </a>
          )
        }
        if (/^mailto:/i.test(resolved.href)) {
          return <a href={resolved.href}>{children}</a>
        }
        // A scheme the docs shouldn't use (javascript:, data:) stays text.
        return <span>{children}</span>
      }
      return <a href={resolved.href}>{children}</a>
    },
    img: function MarkdownImage({ src, alt }: { src?: string; alt?: string }) {
      return <img src={src} alt={alt ?? ''} className="max-w-full rounded-lg" />
    },
    table: function MarkdownTable({ children }: { children?: ReactNode }) {
      return (
        <div className="overflow-x-auto">
          <table>{children}</table>
        </div>
      )
    },
  }
}
