import { ErrorNotice, PageHeader, SearchField } from '@harlew-dev/ui'
import * as api from '../../lib/api'
import { FilterBar, type FilterDef } from '../../components/filter-bar'
import { NoMatches, NoRecords } from '../../components/list'
import { SandboxTable, sandboxStatusOptions } from '../../components/sandbox-table'
import { isFiltered, listQuery, type ListSpec } from '../../lib/list-query'
import { loadOrganizationOptions, useOptionLabels } from '../../lib/list-options'
import { useRegions } from '../../lib/regions'
import { useDocumentTitle } from '../../lib/use-document-title'
import { useListState } from '../../lib/use-list'
import { useLoad } from '../../lib/use-load'

const spec: ListSpec = {
  sortKeys: ['created', 'status', 'region', 'workspace'],
  defaultSort: '-created',
  descendingFirst: ['created'],
  filters: {
    status: sandboxStatusOptions.map((option) => option.value),
    region: null,
    organization: null,
  },
}

export function FleetSandboxesPage() {
  useDocumentTitle('Sandboxes')
  const list = useListState(spec)
  const { state } = list
  const regions = useRegions()
  const organizations = useOptionLabels(
    state.filters.organization ?? [],
    loadOrganizationOptions,
  )
  const sandboxes = useLoad(
    `sandboxes:${list.key()}`,
    () =>
      api.listSandboxes({
        ...listQuery(state, spec),
        status: state.filters.status,
        region: state.filters.region,
        organization: state.filters.organization,
      }),
    {
      every: (data) =>
        data?.data.some((sandbox) => sandbox.status === 'creating') ? 10_000 : 0,
    },
  )

  const filters: FilterDef[] = [
    { key: 'status', label: 'Status', options: sandboxStatusOptions },
    {
      key: 'region',
      label: 'Region',
      options: regions.list.map((region) => ({ value: region.slug, label: region.name })),
    },
    {
      key: 'organization',
      label: 'Workspace',
      load: loadOrganizationOptions,
      selectedLabels: organizations,
    },
  ]

  return (
    <>
      <PageHeader title="Sandboxes" description="Every workspace’s sandboxes." />
      <div className="space-y-4">
        <FilterBar
          filters={filters}
          values={state.filters}
          onApply={list.setFilter}
          onRemove={list.removeFilter}
          onClear={list.clearFilters}
          leading={
            <SearchField
              label="Search sandboxes"
              value={state.q}
              onSearch={list.setSearch}
            />
          }
        />
        {sandboxes.error.message && sandboxes.data && (
          <ErrorNotice error={sandboxes.error} onRetry={sandboxes.reload} />
        )}
        {sandboxes.error.message && !sandboxes.data ? (
          <ErrorNotice error={sandboxes.error} onRetry={sandboxes.reload} />
        ) : (
          <SandboxTable
            page={sandboxes.data ?? undefined}
            loading={sandboxes.loading || sandboxes.stale}
            label="Sandboxes"
            sort={state.sort}
            sortHref={(sort) => list.href({ sort, page: 1 })}
            href={list.href}
            fleet
            canChange
            regionName={regions.name}
            onChanged={sandboxes.refresh}
            empty={
              isFiltered(state) ? (
                <NoMatches label="sandboxes" onClear={list.clearAll} />
              ) : (
                <NoRecords
                  title="No sandboxes"
                  description="Sandboxes workspaces start show up here within seconds."
                />
              )
            }
          />
        )}
      </div>
    </>
  )
}
