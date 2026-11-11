import {
  Badge,
  Button,
  Code,
  ConfirmationDialog,
  DescriptionDetails,
  DescriptionList,
  DescriptionTerm,
  Divider,
  Dropdown,
  DropdownButton,
  DropdownItem,
  DropdownMenu,
  EmptyState,
  ErrorNotice,
  ListSkeleton,
  PageHeader,
  Subheading,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Text,
  useAction,
  toast,
} from '@harlew-dev/ui'
import { EllipsisHorizontalIcon, TrashIcon } from '@heroicons/react/16/solid'
import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router'
import { AliveBadge, StatusBadge } from '../../components/status'
import { ApiError } from '../../lib/api'
import * as api from '../../lib/api'
import { useAuth } from '../../lib/auth'
import { formatDate, relativeTime } from '../../lib/format'
import { useDocumentTitle } from '../../lib/use-document-title'
import { useLoad } from '../../lib/use-load'
import { AddNodeDialog } from './add-node-dialog'
import { ClusterMetrics, ClusterUptime } from './cluster-health'
import { RemoveNodeDialog } from './remove-node-dialog'
import { NodeActions } from './servers'

const back = { href: '/admin/servers', label: 'Servers' }

export function ClusterDetailPage() {
  const { ref = '' } = useParams()
  const navigate = useNavigate()
  const { user } = useAuth()
  const [updated, setUpdated] = useState<api.ClusterDetail | null>(null)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [addOpen, setAddOpen] = useState(false)
  const [removeNode, setRemoveNode] = useState<api.Node | null>(null)
  const { data, error, failure, loading, reload } = useLoad(
    () => api.getCluster(ref),
    [ref],
  )
  const detail = updated ?? data

  useEffect(() => {
    setUpdated(null)
  }, [ref])

  useDocumentTitle(detail?.name ?? 'Cluster')

  const check = useAction(
    async () => {
      const result = await api.checkCluster(ref)
      setUpdated(result)
      return result
    },
    { toError: (failure) => ({ message: errorMessage(failure) }) },
  )

  const isAdmin = user?.instance_admin === true

  if (loading && !detail) {
    return (
      <>
        <PageHeader title="Cluster" back={back} />
        <ListSkeleton label="Loading the cluster" rows={5} />
      </>
    )
  }

  if (failure instanceof ApiError && failure.status === 404) {
    return (
      <EmptyState
        title="Cluster not found"
        description={`No cluster matched ${ref}. It may have been removed.`}
        action={<Button href="/admin/servers">Back to servers</Button>}
      />
    )
  }

  if (error.message || !detail) {
    return (
      <>
        <PageHeader title="Cluster" back={back} />
        <ErrorNotice error={error} onRetry={reload} />
      </>
    )
  }

  const actions = isAdmin ? (
    <>
      <Button outline onClick={() => void check.run()} loading={check.loading}>
        Run checks
      </Button>
      <Button onClick={() => setAddOpen(true)}>Add node</Button>
      <Dropdown>
        <DropdownButton as={Button} plain aria-label="More actions">
          <EllipsisHorizontalIcon />
        </DropdownButton>
        <DropdownMenu anchor="bottom end">
          <DropdownItem variant="danger" onClick={() => setConfirmOpen(true)}>
            <TrashIcon data-slot="icon" />
            Remove cluster
          </DropdownItem>
        </DropdownMenu>
      </Dropdown>
    </>
  ) : undefined

  const failed = detail.checks.filter((item) => !item.ok).length

  return (
    <>
      <PageHeader
        back={back}
        title={detail.name}
        meta={<StatusBadge status={detail.status} detail={detail.status_detail} />}
        description={
          detail.status_detail ||
          `${detail.slug} · ${detail.runtime} · checked ${relativeTime(detail.checked_at)}`
        }
        actions={actions}
      />

      <ErrorNotice error={check.error} className="mb-8" />

      <Subheading className="mt-12">Summary</Subheading>
      <Divider className="mt-4" />
      <DescriptionList>
        <DescriptionTerm>Slug</DescriptionTerm>
        <DescriptionDetails>{detail.slug}</DescriptionDetails>
        <DescriptionTerm>Admin URL</DescriptionTerm>
        <DescriptionDetails>
          <Code>{detail.admin_url}</Code>
        </DescriptionDetails>
        <DescriptionTerm>Endpoint URL</DescriptionTerm>
        <DescriptionDetails>
          <Code>{detail.endpoint_url}</Code>
        </DescriptionDetails>
        <DescriptionTerm>Runtime</DescriptionTerm>
        <DescriptionDetails>{detail.runtime}</DescriptionDetails>
        <DescriptionTerm>AerolVM version</DescriptionTerm>
        <DescriptionDetails>{detail.version ?? '—'}</DescriptionDetails>
        <DescriptionTerm>Admin token</DescriptionTerm>
        <DescriptionDetails>
          <Code>…{detail.admin_token_last4}</Code>
        </DescriptionDetails>
        <DescriptionTerm>Created</DescriptionTerm>
        <DescriptionDetails title={formatDate(detail.created_at)}>
          {relativeTime(detail.created_at)}
        </DescriptionDetails>
        <DescriptionTerm>Last checked</DescriptionTerm>
        <DescriptionDetails title={formatDate(detail.checked_at)}>
          {relativeTime(detail.checked_at)}
        </DescriptionDetails>
      </DescriptionList>

      <ClusterUptime cluster={detail.slug} />

      <div className="mt-12 flex items-center gap-3">
        <Subheading>Checks</Subheading>
        {detail.checks.length > 0 &&
          (failed === 0 ? (
            <Badge color="lime">All passing</Badge>
          ) : (
            <Badge color="red">{failed} failing</Badge>
          ))}
      </div>
      <div className="mt-4">
        {detail.checks.length === 0 ? (
          <Text>No checks have run yet.</Text>
        ) : (
          <Table>
            <TableHead>
              <TableRow>
                <TableHeader>Check</TableHeader>
                <TableHeader>Result</TableHeader>
                <TableHeader>Detail</TableHeader>
              </TableRow>
            </TableHead>
            <TableBody>
              {detail.checks.map((item) => (
                <TableRow key={item.name}>
                  <TableCell className="font-medium">{item.name}</TableCell>
                  <TableCell>
                    <Badge color={item.ok ? 'lime' : 'red'}>
                      {item.ok ? 'Passed' : item.fatal ? 'Failed (blocking)' : 'Failed'}
                    </Badge>
                  </TableCell>
                  <TableCell className="whitespace-normal text-zinc-500 dark:text-zinc-400">
                    {item.detail ?? '—'}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </div>

      <Subheading className="mt-12">Nodes</Subheading>
      <div className="mt-4">
        {detail.node_list.length === 0 ? (
          <Text>No nodes are recorded for this cluster.</Text>
        ) : (
          <Table>
            <TableHead>
              <TableRow>
                <TableHeader>Node</TableHeader>
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
              {detail.node_list.map((node) => (
                <TableRow key={node.id}>
                  <TableCell className="font-medium">{node.name}</TableCell>
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
                  <TableCell className="text-zinc-500 dark:text-zinc-400">{node.url}</TableCell>
                  {isAdmin && (
                    <TableCell className="w-0 text-right">
                      <NodeActions node={node} onRemove={setRemoveNode} />
                    </TableCell>
                  )}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </div>

      <ClusterMetrics cluster={detail.slug} />

      <ConfirmationDialog
        open={confirmOpen}
        onClose={() => setConfirmOpen(false)}
        title="Remove this cluster?"
        description="Lewbox stops managing this cluster and deletes its stored admin token. The AerolVM cluster and its nodes keep running."
        confirm="Remove cluster"
        variant="danger"
        onConfirm={async () => {
          await api.removeCluster(ref)
        }}
        onConfirmed={() => {
          toast.success('Cluster removed.')
          navigate('/admin/servers')
        }}
      />

      <AddNodeDialog open={addOpen} onClose={() => setAddOpen(false)} clusters={[detail]} cluster={detail} />
      <RemoveNodeDialog
        node={removeNode}
        onClose={() => setRemoveNode(null)}
        onRemoved={() => {
          setUpdated(null)
          reload()
        }}
      />
    </>
  )
}

function errorMessage(failure: unknown): string {
  return failure instanceof Error ? failure.message : 'The checks could not run.'
}
