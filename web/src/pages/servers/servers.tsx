import {
  Badge,
  Button,
  Code,
  Dropdown,
  DropdownButton,
  DropdownItem,
  DropdownMenu,
  EmptyState,
  ErrorNotice,
  Listbox,
  ListboxOption,
  ListSkeleton,
  PageHeader,
  PageTabs,
  PageTransition,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Text,
  TextLink,
  tabFrom,
} from '@harlew-dev/ui'
import { EllipsisHorizontalIcon, TrashIcon } from '@heroicons/react/16/solid'
import { useState } from 'react'
import { useSearchParams } from 'react-router'
import { NoClusters } from '../../components/no-clusters'
import { AliveBadge, StatusBadge } from '../../components/status'
import * as api from '../../lib/api'
import { useAuth } from '../../lib/auth'
import { formatDate, relativeTime } from '../../lib/format'
import { useDocumentTitle } from '../../lib/use-document-title'
import { useLoad } from '../../lib/use-load'
import { AddServerMenu } from './add-server-menu'
import { RemoveNodeDialog } from './remove-node-dialog'

const tabs = [
  { id: 'clusters', label: 'Clusters' },
  { id: 'nodes', label: 'Nodes' },
] as const

// Servers: every AerolVM cluster and node, in two tabs, and every way to add
// one (the CLI's cluster and node commands).
export function ServersPage() {
  const [searchParams] = useSearchParams()
  const tab = tabFrom(searchParams, tabs)
  const { user } = useAuth()
  const isAdmin = user?.instance_admin === true
  useDocumentTitle(tab === 'nodes' ? 'Nodes' : 'Servers')

  const clusters = useLoad(() => api.listClusters(), [])
  const clusterList = clusters.data?.data ?? []
  // Bumped when a node is added or removed, so the nodes tab reloads.
  const [nodesVersion, setNodesVersion] = useState(0)

  return (
    <>
      <PageHeader
        title="Servers"
        description="The AerolVM clusters this orchestrator manages, and their nodes."
        actions={
          isAdmin ? (
            <AddServerMenu
              clusters={clusterList}
              onChanged={() => setNodesVersion((value) => value + 1)}
            />
          ) : undefined
        }
        className="mb-4"
      />
      <PageTabs label="Server views" tabs={tabs} current={tab} className="mb-8" />
      <PageTransition key={tab} from={tab === 'nodes' ? 'right' : 'left'}>
        {tab === 'clusters' ? (
          <ClustersTab load={clusters} />
        ) : (
          <NodesTab key={nodesVersion} clusters={clusterList} isAdmin={isAdmin} />
        )}
      </PageTransition>
    </>
  )
}

function ClustersTab({ load }: { load: ReturnType<typeof useLoad<api.ClusterList>> }) {
  const { data, error, loading, reload } = load
  const clusters = data?.data ?? []
  if (loading && !data) return <ListSkeleton label="Loading clusters" rows={4} />
  if (error.message) return <ErrorNotice error={error} onRetry={reload} />
  if (clusters.length === 0) return <NoClusters />
  return (
    <>
      <Table>
        <TableHead>
          <TableRow>
            <TableHeader>Cluster</TableHeader>
            <TableHeader>Status</TableHeader>
            <TableHeader>Runtime</TableHeader>
            <TableHeader className="text-right">Nodes</TableHeader>
            <TableHeader>Version</TableHeader>
            <TableHeader className="text-right">Checked</TableHeader>
          </TableRow>
        </TableHead>
        <TableBody>
          {clusters.map((cluster) => (
            <TableRow
              key={cluster.id}
              href={`/admin/servers/clusters/${encodeURIComponent(cluster.slug)}`}
              title={`Open ${cluster.name}`}
            >
              <TableCell>
                <div className="font-medium">{cluster.name}</div>
                <Text size="sm">{cluster.slug}</Text>
              </TableCell>
              <TableCell>
                <StatusBadge status={cluster.status} detail={cluster.status_detail} />
              </TableCell>
              <TableCell className="text-zinc-500 dark:text-zinc-400">{cluster.runtime}</TableCell>
              <TableCell className="text-right tabular-nums">{cluster.nodes}</TableCell>
              <TableCell className="text-zinc-500 dark:text-zinc-400">{cluster.version ?? '—'}</TableCell>
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
      <Text className="mt-6">
        Lewbox checks every cluster in the background. Open one to run its checks now.
      </Text>
    </>
  )
}

function NodesTab({ clusters, isAdmin }: { clusters: api.Cluster[]; isAdmin: boolean }) {
  const [searchParams, setSearchParams] = useSearchParams()
  const cluster = searchParams.get('cluster') ?? ''
  const [target, setTarget] = useState<api.Node | null>(null)
  const nodes = useLoad(() => api.listNodes(cluster === '' ? undefined : cluster), [cluster])
  const nodeList = nodes.data?.data ?? []

  function changeCluster(value: string) {
    setSearchParams((current) => {
      const next = new URLSearchParams(current)
      next.set('tab', 'nodes')
      if (value === '') next.delete('cluster')
      else next.set('cluster', value)
      return next
    })
  }

  return (
    <>
      <div className="mb-6 flex flex-wrap items-center justify-between gap-4">
        <div className="w-full max-w-xs">
          <Listbox
            aria-label="Filter by cluster"
            value={cluster}
            onChange={changeCluster}
            placeholder="All clusters"
          >
            <ListboxOption value="">All clusters</ListboxOption>
            {clusters.map((item) => (
              <ListboxOption key={item.id} value={item.slug}>
                {item.name}
              </ListboxOption>
            ))}
          </Listbox>
        </div>
        {nodes.data && (
          <Badge color="zinc">
            {nodeList.filter((node) => node.alive === true).length} of {nodeList.length} alive
          </Badge>
        )}
      </div>

      {nodes.loading && !nodes.data ? (
        <ListSkeleton label="Loading nodes" rows={5} />
      ) : nodes.error.message ? (
        <ErrorNotice error={nodes.error} onRetry={nodes.reload} />
      ) : nodeList.length === 0 ? (
        <EmptyState
          title="No nodes"
          description={
            cluster === ''
              ? 'Nodes appear here when a cluster is registered or a node joins one.'
              : 'No nodes belong to this cluster.'
          }
          action={<Button href="/docs/clusters/add-nodes">Read the node guide</Button>}
        />
      ) : (
        <Table>
          <TableHead>
            <TableRow>
              <TableHeader>Node</TableHeader>
              <TableHeader>Cluster</TableHeader>
              <TableHeader>Role</TableHeader>
              <TableHeader>Status</TableHeader>
              <TableHeader>Version</TableHeader>
              <TableHeader>Last seen</TableHeader>
              <TableHeader>URL</TableHeader>
              {isAdmin && (
                <TableHeader className="w-0">
                  <span className="sr-only">Actions</span>
                </TableHeader>
              )}
            </TableRow>
          </TableHead>
          <TableBody>
            {nodeList.map((node) => (
              <TableRow key={node.id}>
                <TableCell className="font-medium">{node.name}</TableCell>
                <TableCell>
                  {node.cluster ? (
                    <TextLink href={`/admin/servers/clusters/${encodeURIComponent(node.cluster)}`}>
                      {node.cluster}
                    </TextLink>
                  ) : (
                    <Text>—</Text>
                  )}
                </TableCell>
                <TableCell>{node.role ?? '—'}</TableCell>
                <TableCell>
                  <AliveBadge alive={node.alive} />
                </TableCell>
                <TableCell className="text-zinc-500 dark:text-zinc-400">{node.version ?? '—'}</TableCell>
                <TableCell
                  className="text-zinc-500 dark:text-zinc-400"
                  title={formatDate(node.last_seen_at)}
                >
                  {relativeTime(node.last_seen_at)}
                </TableCell>
                <TableCell>
                  <Code>{node.url}</Code>
                </TableCell>
                {isAdmin && (
                  <TableCell className="w-0 text-right">
                    <NodeActions node={node} onRemove={setTarget} />
                  </TableCell>
                )}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      <RemoveNodeDialog node={target} onClose={() => setTarget(null)} onRemoved={nodes.reload} />
    </>
  )
}

export function NodeActions({
  node,
  onRemove,
}: {
  node: api.Node
  onRemove: (node: api.Node) => void
}) {
  return (
    <Dropdown>
      <DropdownButton as={Button} plain aria-label={`Actions for ${node.name}`}>
        <EllipsisHorizontalIcon />
      </DropdownButton>
      <DropdownMenu anchor="bottom end">
        <DropdownItem variant="danger" onClick={() => onRemove(node)}>
          <TrashIcon data-slot="icon" />
          Remove node
        </DropdownItem>
      </DropdownMenu>
    </Dropdown>
  )
}
