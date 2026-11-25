import { Button, PageHeader, SearchField } from '@harlew-dev/ui'
import { useState } from 'react'
import * as api from '../../lib/api'
import { FilterBar, type FilterDef } from '../../components/filter-bar'
import { NoMatches, NoRecords } from '../../components/list'
import { useDocumentTitle } from '../../lib/use-document-title'
import { isFiltered, listQuery, type ListSpec } from '../../lib/list-query'
import { useListState } from '../../lib/use-list'
import { useLoad } from '../../lib/use-load'
import { noRegionText } from '../../components/no-region'
import { SandboxKeyTable } from '../../components/sandbox-key-table'
import { CreateKeyDialog } from './create-key-dialog'
import { usePortalOrg } from './portal-route'

const spec: ListSpec = {
  sortKeys: ['created', 'name', 'expires'],
  defaultSort: '-created',
  descendingFirst: ['created', 'expires'],
  filters: { status: ['active', 'expired', 'revoked'] },
}

const filters: FilterDef[] = [
  {
    key: 'status',
    label: 'Status',
    options: [
      { value: 'active', label: 'Active' },
      { value: 'expired', label: 'Expired' },
      { value: 'revoked', label: 'Revoked' },
    ],
  },
]

export function KeysPage() {
  useDocumentTitle('API keys')
  const { org, can } = usePortalOrg()
  const [createOpen, setCreateOpen] = useState(false)
  const list = useListState(spec)
  const { state } = list
  const keys = useLoad(`sandbox-keys:${org.slug}:${list.key()}`, () =>
    api.listSandboxKeys(org.slug, {
      ...listQuery(state, spec),
      status: state.filters.status as api.KeyStatus[] | undefined,
    }),
  )
  const noRegion = org.region === null
  const create = can.use ? (
    <Button onClick={() => setCreateOpen(true)} disabled={noRegion} disabledReason={noRegionText}>
      Create API key
    </Button>
  ) : undefined
  const any = (keys.data?.pages.items ?? 0) > 0

  return (
    <>
      <PageHeader
        title="API keys"
        description="Each key works in your workspace’s region, with the AerolVM or E2B SDK."
        actions={any ? create : undefined}
      />

      <div className="space-y-4">
        <FilterBar
          filters={filters}
          values={state.filters}
          onApply={list.setFilter}
          onRemove={list.removeFilter}
          onClear={list.clearFilters}
          leading={
            <SearchField label="Search API keys" value={state.q} onSearch={list.setSearch} />
          }
        />
        <SandboxKeyTable
          page={keys.data ?? undefined}
          loading={keys.loading || keys.stale}
          sort={state.sort}
          sortHref={(sort) => list.href({ sort, page: 1 })}
          href={list.href}
          org={org.slug}
          canRevoke={can.use}
          onRevoked={keys.refresh}
          showRegion={false}
          empty={
            isFiltered(state) ? (
              <NoMatches label="API keys" onClear={list.clearAll} />
            ) : (
              <NoRecords
                title="No API keys"
                description={
                  noRegion ? noRegionText : 'Create a key, then run your first sandbox with it.'
                }
                action={create}
              />
            )
          }
        />
      </div>

      <CreateKeyDialog
        open={createOpen}
        org={org}
        onClose={() => setCreateOpen(false)}
        onCreated={keys.refresh}
      />
    </>
  )
}
