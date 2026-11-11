import {
  Badge,
  Button,
  ErrorNotice,
  Heading,
  ListSkeleton,
  Stat,
  Subheading,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Text,
} from '@harlew-dev/ui'
import { useAuth } from '../lib/auth'
import * as api from '../lib/api'
import { formatDate, relativeTime } from '../lib/format'
import { useDocumentTitle } from '../lib/use-document-title'
import { useLoad } from '../lib/use-load'
import { NoClusters } from '../components/no-clusters'
import { StatusBadge } from '../components/status'

function greeting(now: Date): string {
  const hour = now.getHours()
  if (hour < 12) return 'Good morning'
  if (hour < 18) return 'Good afternoon'
  return 'Good evening'
}

function count(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`
}

export function OverviewPage() {
  useDocumentTitle('Overview')
  const { user } = useAuth()
  const { data, error, loading, reload } = useLoad(
    () => Promise.all([api.listClusters(), api.listNodes()]),
    [],
  )

  const [clusters, nodes] = data ?? [null, null]
  const clusterList = clusters?.data ?? []
  const nodeList = nodes?.data ?? []
  const ready = clusterList.filter((cluster) => cluster.status === 'ready').length
  const notReady = clusterList.length - ready
  const alive = nodeList.filter((node) => node.alive === true).length
  const down = nodeList.filter((node) => node.alive === false).length
  const attention = notReady + down
  const versions = [
    ...new Set(nodeList.map((node) => node.version).filter((v): v is string => !!v)),
  ].sort()
  const name = user?.email.split('@')[0]

  return (
    <>
      <Heading>
        {greeting(new Date())}
        {name ? `, ${name}` : ''}
      </Heading>

      <div className="mt-8 flex items-end justify-between gap-4">
        <Subheading>Overview</Subheading>
        <Button outline onClick={reload} disabled={loading}>
          Refresh
        </Button>
      </div>

      {loading && !data ? (
        <div className="mt-4">
          <ListSkeleton label="Loading the overview" rows={4} />
        </div>
      ) : error.message ? (
        <ErrorNotice error={error} onRetry={reload} className="mt-4" />
      ) : (
        <>
          <div className="mt-4 grid gap-8 sm:grid-cols-2 xl:grid-cols-4">
            <Stat title="Clusters" value={clusterList.length} href="/servers">
              {clusterList.length === 0 ? (
                <span>None registered yet</span>
              ) : (
                <>
                  <Badge color="lime">{ready} ready</Badge>
                  {notReady > 0 && <Badge color="amber">{notReady} not ready</Badge>}
                </>
              )}
            </Stat>
            <Stat title="Nodes" value={nodeList.length} href="/servers?tab=nodes">
              {nodeList.length === 0 ? (
                <span>None yet</span>
              ) : (
                <>
                  <Badge color="lime">{alive} alive</Badge>
                  {down > 0 && <Badge color="red">{down} down</Badge>}
                </>
              )}
            </Stat>
            <Stat title="Needs attention" value={attention}>
              {attention === 0 ? (
                <span>Every cluster and node is healthy</span>
              ) : (
                <span>
                  {[
                    notReady > 0 && count(notReady, 'cluster', 'clusters'),
                    down > 0 && count(down, 'node', 'nodes'),
                  ]
                    .filter(Boolean)
                    .join(' and ')}
                </span>
              )}
            </Stat>
            <Stat
              title="AerolVM"
              value={versions.length === 1 ? versions[0] : versions.length === 0 ? '—' : `${versions.length} versions`}
            >
              {versions.length === 1 ? (
                <span>On every node</span>
              ) : versions.length > 1 ? (
                <span>{versions.join(', ')}</span>
              ) : (
                <span>No version reported</span>
              )}
            </Stat>
          </div>

          <Subheading className="mt-14">Clusters</Subheading>
          <div className="mt-4">
            {clusterList.length === 0 ? (
              <NoClusters />
            ) : (
              <Table>
                <TableHead>
                  <TableRow>
                    <TableHeader>Cluster</TableHeader>
                    <TableHeader>Status</TableHeader>
                    <TableHeader>Runtime</TableHeader>
                    <TableHeader className="text-right">Nodes</TableHeader>
                    <TableHeader>Version</TableHeader>
                    <TableHeader className="text-right">Last checked</TableHeader>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {clusterList.map((cluster) => (
                    <TableRow
                      key={cluster.id}
                      href={`/servers/clusters/${encodeURIComponent(cluster.slug)}`}
                      title={`Open ${cluster.name}`}
                    >
                      <TableCell>
                        <div className="font-medium">{cluster.name}</div>
                        <Text size="sm">{cluster.slug}</Text>
                      </TableCell>
                      <TableCell>
                        <StatusBadge status={cluster.status} detail={cluster.status_detail} />
                      </TableCell>
                      <TableCell className="text-zinc-500 dark:text-zinc-400">
                        {cluster.runtime}
                      </TableCell>
                      <TableCell className="text-right tabular-nums">{cluster.nodes}</TableCell>
                      <TableCell className="text-zinc-500 dark:text-zinc-400">
                        {cluster.version ?? '—'}
                      </TableCell>
                      <TableCell
                        className="text-right text-zinc-500 dark:text-zinc-400"
                        title={formatDate(cluster.checked_at)}
                      >
                        {relativeTime(cluster.checked_at)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </div>
        </>
      )}
    </>
  )
}
