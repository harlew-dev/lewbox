import {
  Button,
  Divider,
  ErrorNotice,
  Heading,
  Link,
  ListSkeleton,
  Prose,
  TableOfContents,
  Text,
} from '@harlew-dev/ui'
import { ArrowLeftIcon, ArrowRightIcon } from '@heroicons/react/16/solid'
import { useEffect, useMemo } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { useLocation, useParams } from 'react-router'
import { ApiError } from '../lib/api'
import * as api from '../lib/api'
import {
  extractHeadings,
  markdownComponents,
  rehypeHeadingIds,
} from '../lib/docs-markdown'
import { useDocumentTitle } from '../lib/use-document-title'
import { useLoad } from '../lib/use-load'

const prefixes: Record<api.DocsBook, string> = {
  consumer: '/docs',
  platform: '/platform',
}

function PageLink({
  link,
  direction,
  prefix,
}: {
  link: api.PageLink
  direction: 'previous' | 'next'
  prefix: string
}) {
  const next = direction === 'next'
  const Arrow = next ? ArrowRightIcon : ArrowLeftIcon
  return (
    <Link
      href={`${prefix}/${link.path}`}
      className={`group flex flex-col gap-1 rounded-md ${next ? 'ml-auto items-end text-right' : 'items-start'}`}
    >
      <span className="flex items-center gap-1.5 text-sm/6 text-zinc-500 dark:text-zinc-400">
        {!next && <Arrow className="size-4 fill-current" />}
        {next ? 'Next' : 'Previous'}
        {next && <Arrow className="size-4 fill-current" />}
      </span>
      <span className="text-base/6 font-medium text-zinc-950 group-data-hover:underline sm:text-sm/6 dark:text-white">
        {link.title}
      </span>
    </Link>
  )
}

function DocsBookPage({ book }: { book: api.DocsBook }) {
  const params = useParams()
  const location = useLocation()
  const pagePath = (params['*'] ?? '').replace(/^\/+|\/+$/g, '')
  const prefix = prefixes[book]
  const { data: page, error, failure, loading, reload } = useLoad(
    () => api.docsPage(book, pagePath),
    [book, pagePath],
  )
  const headings = useMemo(
    () => extractHeadings(page?.markdown ?? ''),
    [page?.markdown],
  )
  const components = useMemo(
    () => markdownComponents(pagePath, book),
    [book, pagePath],
  )
  const toc = useMemo(
    () =>
      headings
        .filter((heading) => heading.level === 2 || heading.level === 3)
        .map((heading) => ({
          id: heading.id,
          text: heading.text,
          nested: heading.level === 3,
        })),
    [headings],
  )

  useDocumentTitle(page?.title ?? (book === 'platform' ? 'Platform docs' : 'Documentation'))

  useEffect(() => {
    if (!page) return
    if (location.hash) {
      const id = decodeURIComponent(location.hash.slice(1))
      document.getElementById(id)?.scrollIntoView()
    } else {
      window.scrollTo({ top: 0 })
    }
  }, [page, location.hash])

  if (loading && !page) {
    return (
      <div className="max-w-3xl">
        <ListSkeleton label="Loading the page" rows={8} />
      </div>
    )
  }

  if (failure instanceof ApiError && failure.status === 404) {
    return (
      <div className="max-w-3xl space-y-4">
        <Heading>Page not found</Heading>
        <Text>There is no documentation page at this address.</Text>
        <Button href={prefix}>Back to the documentation</Button>
      </div>
    )
  }

  if (error.message || !page) {
    return (
      <div className="max-w-3xl">
        <ErrorNotice error={error} onRetry={reload} />
      </div>
    )
  }

  return (
    <div className="grid gap-x-16 xl:grid-cols-[minmax(0,1fr)_13rem]">
      <article className="max-w-3xl min-w-0">
        <header className="mb-10">
          <h1 className="text-3xl/9 font-semibold tracking-tight text-zinc-950 dark:text-white">
            {page.title}
          </h1>
          {page.description && (
            <p className="mt-4 text-lg/8 text-zinc-600 dark:text-zinc-400">
              {page.description}
            </p>
          )}
        </header>

        <Prose>
          <ReactMarkdown
            remarkPlugins={[remarkGfm]}
            rehypePlugins={[rehypeHeadingIds]}
            components={components}
          >
            {page.markdown}
          </ReactMarkdown>
        </Prose>

        {(page.prev || page.next) && (
          <div className="mt-16">
            <Divider />
            <nav
              aria-label="Documentation pages"
              className="mt-8 flex flex-wrap justify-between gap-6"
            >
              {page.prev && <PageLink link={page.prev} direction="previous" prefix={prefix} />}
              {page.next && <PageLink link={page.next} direction="next" prefix={prefix} />}
            </nav>
          </div>
        )}
      </article>

      <aside className="hidden xl:block">
        <div className="sticky top-26 max-h-[calc(100dvh-(--spacing(32)))] overflow-y-auto pl-1">
          <TableOfContents items={toc} />
        </div>
      </aside>
    </div>
  )
}

export function DocsPage() {
  return <DocsBookPage book="consumer" />
}

export function PlatformDocsPage() {
  return <DocsBookPage book="platform" />
}
