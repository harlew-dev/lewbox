import {
  Badge,
  Button,
  Code,
  Dropdown,
  DropdownButton,
  DropdownItem,
  DropdownMenu,
  ErrorNotice,
  ListSkeleton,
  PageHeader,
  PageTabs,
  PageTransition,
  PaginatedTable,
  SearchField,
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
import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router'
import { FilterBar, type FilterDef } from '../../components/filter-bar'
import { NoMatches, NoRecords, SortHeader } from '../../components/list'
import { NoClusters } from '../../components/no-clusters'
import { AliveBadge, StatusBadge } from '../../components/status'
import * as api from '../../lib/api'
import { formatDate, relativeTime } from '../../lib/format'
import { isFiltered, listQuery, type ListSpec } from '../../lib/list-query'
import { routes } from '../../lib/routes'
import { useDocumentTitle } from '../../lib/use-document-title'
import { useListState } from '../../lib/use-list'
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
  useDocumentTitle(tab === 'nodes' ? 'Nodes' : 'Servers')

  const clusters = useLoad('clusters', () => api.listClusters())
  const clusterList = clusters.data?.data ?? []
  // Bumped when a server may have been added, so the nodes tab refreshes.
  const [nodesVersion, setNodesVersion] = useState(0)
  const refreshClusters = clusters.refresh

  return (
    <>
      <PageHeader
        title="Servers"
        description="The AerolVM clusters this orchestrator manages, and their nodes."
        actions={
          <AddServerMenu
            clusters={clusterList}
            onChanged={() => {
              refreshClusters()
              setNodesVersion((value) => value + 1)
            }}
          />
        }
        className="mb-4"
      />
      <PageTabs label="Server views" tabs={tabs} current={tab} className="mb-8" />
      <PageTransition key={tab} from={tab === 'nodes' ? 'right' : 'left'}>
        {tab === 'clusters' ? (
          <ClustersTab load={clusters} />
        ) : (
          <NodesTab version={nodesVersion} clusters={clusterList} />
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
              href={routes.admin.cluster(cluster.slug)}
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

const nodeSpec: ListSpec = {
  sortKeys: ['cluster', 'name'],
  defaultSort: 'cluster',
  filters: {
    cluster: null,
    role: ['mixed', 'server', 'worker', 'ingress'],
  },
}

const roleOptions = [
  { value: 'mixed', label: 'Mixed' },
  { value: 'server', label: 'Server' },
  { value: 'worker', label: 'Worker' },
  { value: 'ingress', label: 'Ingress' },
]

function NodesTab({ clusters, version }: { clusters: api.Cluster[]; version: number }) {
  const [target, setTarget] = useState<api.Node | null>(null)
  const list = useListState(nodeSpec)
  const { state } = list
  const nodes = useLoad(`nodes:${list.key()}`, () =>
    api.listNodes({
      ...listQuery(state, nodeSpec),
      cluster: state.filters.cluster,
      role: state.filters.role,
    }),
  )
  const alive = useLoad(`nodes:alive:${state.filters.cluster?.join(',') ?? 'all'}`, () =>
    api.listNodes({ cluster: state.filters.cluster, alive: true, per_page: 1 }),
  )
  const total = useLoad(`nodes:total:${state.filters.cluster?.join(',') ?? 'all'}`, () =>
    api.listNodes({ cluster: state.filters.cluster, per_page: 1 }),
  )
  const refreshNodes = nodes.refresh
  // Refresh in the background, keeping the list on screen.
  useEffect(() => {
    if (version > 0) refreshNodes()
  }, [version, refreshNodes])

  const filters: FilterDef[] = [
    {
      key: 'cluster',
      label: 'Cluster',
      options: clusters.map((cluster) => ({ value: cluster.slug, label: cluster.name })),
    },
    { key: 'role', label: 'Role', options: roleOptions },
  ]

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div className="w-full">
          <FilterBar
            filters={filters}
            values={state.filters}
            onApply={list.setFilter}
            onRemove={list.removeFilter}
            onClear={list.clearFilters}
            leading={
              <SearchField label="Search nodes" value={state.q} onSearch={list.setSearch} />
            }
          />
        </div>
        {total.data && (
          <Badge color="zinc">
            {alive.data?.pages.items ?? 0} of {total.data.pages.items} alive
          </Badge>
        )}
      </div>

      {nodes.error.message && nodes.data && (
        <ErrorNotice error={nodes.error} onRetry={nodes.reload} />
      )}
      {nodes.error.message && !nodes.data ? (
        <ErrorNotice error={nodes.error} onRetry={nodes.reload} />
      ) : (
        <PaginatedTable
          page={nodes.data ?? undefined}
          loading={nodes.loading || nodes.stale}
          label="Nodes"
          href={list.href}
          rowKey={(node) => node.id}
          head={
            <TableRow>
              <SortHeader state={state} spec={nodeSpec} column="name" base={list.params}>
                Node
              </SortHeader>
              <SortHeader state={state} spec={nodeSpec} column="cluster" base={list.params}>
                Cluster
              </SortHeader>
              <TableHeader>Role</TableHeader>
              <TableHeader>Status</TableHeader>
              <TableHeader>Version</TableHeader>
              <TableHeader>Last seen</TableHeader>
              <TableHeader>URL</TableHeader>
              <TableHeader className="w-0">
                <span className="sr-only">Actions</span>
              </TableHeader>
            </TableRow>
          }
          row={(node) => (
            <TableRow key={node.id}>
              <TableCell className="font-medium">{node.name}</TableCell>
              <TableCell>
                {node.cluster ? (
                  <TextLink href={routes.admin.cluster(node.cluster)}>{node.cluster}</TextLink>
                ) : (
                  <Text>—</Text>
                )}
              </TableCell>
              <TableCell>{node.role ?? '—'}</TableCell>
              <TableCell>
                <AliveBadge alive={node.alive} />
              </TableCell>
              <TableCell className="text-zinc-500 dark:text-zinc-400">
                {node.version ?? '—'}
              </TableCell>
              <TableCell
                className="text-zinc-500 dark:text-zinc-400"
                title={formatDate(node.last_seen_at)}
              >
                {relativeTime(node.last_seen_at)}
              </TableCell>
              <TableCell>
                <Code>{node.url}</Code>
              </TableCell>
              <TableCell className="w-0 text-right">
                <NodeActions node={node} onRemove={setTarget} />
              </TableCell>
            </TableRow>
          )}
          empty={
            isFiltered(state) ? (
              <NoMatches label="nodes" onClear={list.clearAll} />
            ) : (
              <NoRecords
                title="No nodes"
                description="Nodes appear here when a cluster is registered or a node joins one."
                action={
                  <Button href={routes.platformDocs('clusters/add-nodes')}>
                    Read the node guide
                  </Button>
                }
              />
            )
          }
        />
      )}

      <RemoveNodeDialog node={target} onClose={() => setTarget(null)} onRemoved={nodes.reload} />
    </div>
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
