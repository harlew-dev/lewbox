import {
  EmptyState,
  ErrorNotice,
  ListSkeleton,
  PageHeader,
  Stat,
  Subheading,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@harlew-dev/ui'
import * as api from '../../lib/api'
import { formatDate, formatMoney } from '../../lib/format'
import { useInstance } from '../../lib/instance'
import { useDocumentTitle } from '../../lib/use-document-title'
import { useLoad } from '../../lib/use-load'
import { NotFoundPage } from '../not-found'
import { usePortalOrg } from './portal-route'

const kinds: Record<string, string> = {
  grant: 'Credits added',
  usage: 'Usage',
  monthly: 'Monthly credits',
  signup: 'Welcome credits',
}

// The ledger, newest first: credits in, usage out.
export function CreditTable({ credits }: { credits: api.Credits }) {
  if (credits.data.length === 0) {
    return <EmptyState title="No transactions yet" description="Credits added and usage charged show up here." />
  }
  return (
    <Table>
      <TableHead>
        <TableRow>
          <TableHeader>Date</TableHeader>
          <TableHeader>Description</TableHeader>
          <TableHeader className="text-right">Amount</TableHeader>
        </TableRow>
      </TableHead>
      <TableBody>
        {credits.data.map((txn) => (
          <TableRow key={txn.id}>
            <TableCell className="text-zinc-500 dark:text-zinc-400">{formatDate(txn.created_at)}</TableCell>
            <TableCell>
              {kinds[txn.kind] ?? txn.kind}
              {txn.note && <span className="text-zinc-500 dark:text-zinc-400"> · {txn.note}</span>}
            </TableCell>
            <TableCell className="text-right tabular-nums">
              {txn.amount_micros > 0 ? '+' : ''}
              {formatMoney(txn.amount_micros, credits.currency)}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

export function BillingPage() {
  useDocumentTitle('Billing')
  const { org } = usePortalOrg()
  const { billing, loaded } = useInstance()
  const on = billing !== 'off'
  const credits = useLoad(() => (on ? api.getCredits(org.slug) : Promise.resolve(null)), [org.slug, on])
  if (loaded && !on) return <NotFoundPage />
  const data = credits.data

  return (
    <>
      <PageHeader title="Billing" />
      {credits.loading && !data ? (
        <ListSkeleton label="Loading billing" rows={4} />
      ) : credits.error.message || !data ? (
        <ErrorNotice error={credits.error} onRetry={credits.reload} />
      ) : (
        <>
          <div className="grid gap-8 sm:grid-cols-3">
            <Stat title="Balance" value={formatMoney(data.balance_micros, data.currency)}>
              {data.unbilled_micros > 0 && <span>{formatMoney(data.unbilled_micros, data.currency)} not yet charged</span>}
            </Stat>
            <Stat title="Spent this month" value={formatMoney(data.month_spend_micros, data.currency)} />
            <Stat title="Monthly credits" value={formatMoney(data.monthly_credits_micros, data.currency)}>
              <span>Added on the 1st of each month</span>
            </Stat>
          </div>
          <Subheading className="mt-14 mb-4">Transactions</Subheading>
          <CreditTable credits={data} />
        </>
      )}
    </>
  )
}
