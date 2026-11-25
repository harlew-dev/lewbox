import type { DocsBook, DocsNav, DocsPage } from '../../types/docs'
import { request } from './client'

export function docsNav(book: DocsBook = 'consumer'): Promise<DocsNav> {
  return request<DocsNav>(book === 'platform' ? '/v0/platform' : '/v0/docs')
}

export function docsPage(book: DocsBook, path: string): Promise<DocsPage> {
  const base = book === 'platform' ? '/v0/platform' : '/v0/docs'
  const clean = path.replace(/^\/+|\/+$/g, '')
  const suffix = clean
    .split('/')
    .map((segment) => encodeURIComponent(segment))
    .join('/')
  return request<DocsPage>(`${base}/${suffix}`)
}
