import { NotFound } from '@harlew-dev/ui'
import { useDocumentTitle } from '../lib/use-document-title'

export function NotFoundPage() {
  useDocumentTitle('Not found')
  return <NotFound href="/" />
}
