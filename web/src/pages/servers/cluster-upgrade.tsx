import {
  Badge,
  Button,
  Checkbox,
  ConfirmationDialog,
  Description,
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
  Divider,
  ErrorMessage,
  ErrorNotice,
  Field,
  Input,
  Label,
  ListSkeleton,
  Notice,
  Subheading,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Text,
  toast,
  useAction,
} from '@harlew-dev/ui'
import type { BadgeColor } from '@harlew-dev/ui'
import { useOpenCount } from '@harlew-dev/ui/lib/use-open-count'
import { CheckCircleIcon, XCircleIcon } from '@heroicons/react/16/solid'
import { useEffect, useState } from 'react'
import * as api from '../../lib/api'
import { validVersion } from '../../lib/commands'
import { formatDate } from '../../lib/format'
import { failureMessage, useLoad, type LoadState } from '../../lib/use-load'

// How often the page asks where a running rollout stands. Tests shorten it.
export const upgradeTiming = { every: 4000 }

// A rollout keeps moving in these phases; everything else has settled.
export function isUpgradeActive(phase: string): boolean {
  return phase === 'staging' || phase === 'running' || phase === 'rolling_back' || phase === 'finalizing'
}

const phaseLabels: Record<string, string> = {
  staging: 'Staging',
  running: 'Running',
  paused: 'Paused',
  done: 'Done',
  failed: 'Failed',
  stopped: 'Stopped',
  finalizing: 'Finalizing',
  finalized: 'Finalized',
  rolling_back: 'Rolling back',
  rolled_back: 'Rolled back',
  unknown: 'Unknown',
}

const phaseColors: Record<string, BadgeColor> = {
  staging: 'zinc',
  running: 'lime',
  paused: 'amber',
  done: 'lime',
  failed: 'red',
  stopped: 'amber',
  finalizing: 'lime',
  finalized: 'lime',
  rolling_back: 'amber',
  rolled_back: 'amber',
  unknown: 'zinc',
}

const phaseLabel = (phase: string) => phaseLabels[phase] ?? phase

// A rollout's commands, only where its phase accepts them.
function allowedCommands(upgrade: api.ClusterUpgrade): {
  stop: boolean
  resume: boolean
  rollback: boolean
  finalize: boolean
} {
  const phase = upgrade.phase
  return {
    stop: phase === 'staging' || phase === 'running' || phase === 'paused',
    resume: phase === 'paused',
    // Rollback works from paused, stopped or failed, and at done with hold.
    rollback: phase === 'paused' || phase === 'stopped' || phase === 'failed' || (phase === 'done' && upgrade.hold),
    finalize: phase === 'done',
  }
}

// The live rollout, or nothing when the cluster never had one (404).
export function ClusterUpgradeSection({
  cluster,
  upgrade,
  onChanged,
}: {
  cluster: string
  upgrade: LoadState<api.ClusterUpgrade>
  // The cluster changed: the page reloads its summary (the version).
  onChanged: () => void
}) {
  const [rollbackOpen, setRollbackOpen] = useState(false)
  const [finalizeOpen, setFinalizeOpen] = useState(false)
  const data = upgrade.data

  const stop = useAction(() => api.stopClusterUpgrade(cluster), {
    toError: (failure) => ({ message: failureMessage(failure) }),
  })
  const resume = useAction(() => api.resumeClusterUpgrade(cluster), {
    toError: (failure) => ({ message: failureMessage(failure) }),
  })

  const changeError = stop.error.message ? stop.error : resume.error

  if (upgrade.missing) return null
  if (!data) {
    return upgrade.error.message ? (
      <div className="mt-12">
        <ErrorNotice error={upgrade.error} onRetry={upgrade.reload} />
      </div>
    ) : null
  }

  const refresh = () => upgrade.refresh()

  if (data.phase === 'finalized') {
    const summary =
      data.direction === 'rollback'
        ? `Rolled back to ${data.previous_version ?? 'the previous release'} on ${formatDate(data.updated_at)}.`
        : `Upgraded to ${data.target_version} on ${formatDate(data.updated_at)}.`
    return (
      <div className="mt-12">
        <Subheading>Upgrade</Subheading>
        <Divider className="mt-4" />
        <Text className="mt-4">{summary}</Text>
      </div>
    )
  }

  const commands = allowedCommands(data)

  return (
    <div className="mt-12">
      <div className="flex flex-wrap items-center gap-3">
        <Subheading>Upgrade</Subheading>
        <Badge color={phaseColors[data.phase] ?? 'zinc'}>{phaseLabel(data.phase)}</Badge>
        <Text>
          {data.direction === 'rollback' ? 'Rolling back from' : 'Target'} {data.target_version}
        </Text>
      </div>
      <Divider className="mt-4" />

      {upgrade.error.message && (
        <ErrorNotice error={upgrade.error} onRetry={upgrade.reload} className="mt-4" />
      )}
      {data.error && (
        <Notice variant="error" className="mt-4">
          {data.error}
        </Notice>
      )}

      <Table className="mt-4">
        <TableHead>
          <TableRow>
            <TableHeader>Node</TableHeader>
            <TableHeader>Role</TableHeader>
            <TableHeader>Version</TableHeader>
            <TableHeader>State</TableHeader>
          </TableRow>
        </TableHead>
        <TableBody>
          {data.nodes.map((node) => (
            <TableRow key={node.node}>
              <TableCell className="font-medium">{node.node}</TableCell>
              <TableCell>{node.role || '—'}</TableCell>
              <TableCell className="text-zinc-500 dark:text-zinc-400">
                {versionPath(node)}
              </TableCell>
              <TableCell>
                <NodeState node={node} direction={data.direction} />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>

      {(commands.stop || commands.resume || commands.rollback || commands.finalize) && (
        <div className="mt-6 flex flex-wrap justify-end gap-3">
          {commands.stop && (
            <Button
              outline
              loading={stop.loading}
              onClick={async () => {
                const result = await stop.run()
                if (result.ok) {
                  toast.success('Upgrade stopped.')
                  refresh()
                }
              }}
            >
              Stop upgrade
            </Button>
          )}
          {commands.resume && (
            <Button
              outline
              loading={resume.loading}
              onClick={async () => {
                const result = await resume.run()
                if (result.ok) {
                  toast.success('Upgrade resumed.')
                  refresh()
                }
              }}
            >
              Resume upgrade
            </Button>
          )}
          {commands.rollback && (
            <Button outline onClick={() => setRollbackOpen(true)}>
              Roll back
            </Button>
          )}
          {commands.finalize && <Button onClick={() => setFinalizeOpen(true)}>Finalize upgrade</Button>}
        </div>
      )}
      <ErrorNotice error={changeError} className="mt-4" />

      <ConfirmationDialog
        open={rollbackOpen}
        onClose={() => setRollbackOpen(false)}
        title="Roll back this upgrade?"
        description={`Every node goes back to ${data.previous_version ?? 'its previous release'}. Running sandboxes keep running.`}
        confirm="Roll back"
        onConfirm={() => api.rollbackClusterUpgrade(cluster)}
        onConfirmed={() => {
          toast.success('Rollback started.')
          refresh()
          onChanged()
        }}
      />
      <ConfirmationDialog
        open={finalizeOpen}
        onClose={() => setFinalizeOpen(false)}
        title="Finalize this upgrade?"
        description={`${data.target_version} becomes the release this cluster runs for good. Rollback will no longer be possible.`}
        confirm="Finalize upgrade"
        onConfirm={() => api.finalizeClusterUpgrade(cluster)}
        onConfirmed={() => {
          toast.success('Upgrade finalized.')
          refresh()
          onChanged()
        }}
      />
    </div>
  )
}

function versionPath(node: api.ClusterUpgradeNode): string {
  if (!node.to_version || node.from_version === node.to_version) {
    return node.to_version || node.from_version || '—'
  }
  if (!node.from_version) return node.to_version
  return `${node.from_version} → ${node.to_version}`
}

function NodeState({ node, direction }: { node: api.ClusterUpgradeNode; direction: string }) {
  if (node.state === 'failed') {
    return (
      <div>
        <span className="flex items-center gap-2 font-medium text-red-700 dark:text-red-400">
          <XCircleIcon className="size-4 shrink-0 fill-red-500 dark:fill-red-400" aria-hidden="true" />
          Failed
        </span>
        {node.error && (
          <div className="mt-1 whitespace-normal text-sm text-zinc-500 dark:text-zinc-400">{node.error}</div>
        )}
      </div>
    )
  }
  const onTarget =
    node.state === 'done' ||
    (node.state === 'skipped' && node.to_version !== undefined && node.from_version === node.to_version)
  if (onTarget) {
    return (
      <span className="flex items-center gap-2 text-green-700 dark:text-green-400">
        <CheckCircleIcon className="size-4 shrink-0 fill-green-600 dark:fill-green-500" aria-hidden="true" />
        {direction === 'rollback' ? 'Rolled back' : 'Upgraded'}
      </span>
    )
  }
  if (node.state === 'skipped') {
    return <span className="text-zinc-500 dark:text-zinc-400">Skipped</span>
  }
  return (
    <span className="flex items-center gap-2 text-zinc-500 dark:text-zinc-400">
      <span
        aria-hidden="true"
        className="size-4 shrink-0 animate-spin rounded-full border-2 border-zinc-950/10 border-t-zinc-500 motion-reduce:animate-none dark:border-white/10 dark:border-t-zinc-400"
      />
      {node.state === 'upgrading' ? 'Upgrading' : 'Waiting'}
    </span>
  )
}

// The plan, then Start: the rollout itself is followed on the cluster page.
export function UpgradeClusterDialog({
  open,
  cluster,
  onClose,
  onStarted,
}: {
  open: boolean
  cluster: string
  onClose: () => void
  onStarted: () => void
}) {
  const count = useOpenCount(open)
  return (
    <Dialog open={open} onClose={onClose} size="3xl">
      <UpgradeForm key={count} cluster={cluster} onClose={onClose} onStarted={onStarted} />
    </Dialog>
  )
}

function UpgradeForm({
  cluster,
  onClose,
  onStarted,
}: {
  cluster: string
  onClose: () => void
  onStarted: () => void
}) {
  const [version, setVersion] = useState('')
  const [wanted, setWanted] = useState('')
  const [allowDowntime, setAllowDowntime] = useState(false)

  // Wait for typing to stop so one version costs one plan request.
  useEffect(() => {
    const timer = window.setTimeout(() => setWanted(version.trim()), 300)
    return () => window.clearTimeout(timer)
  }, [version])

  const isLatest = wanted === 'latest'
  const invalid = wanted !== '' && (isLatest || !validVersion(wanted))
  const behind = version.trim() !== wanted
  const plan = useLoad(
    invalid ? null : `cluster:${cluster}:upgrade-plan:${wanted || 'tested'}`,
    () => api.planClusterUpgrade(cluster, wanted === '' ? {} : { version: wanted }),
  )
  const loaded = plan.data
  const needsDowntime = loaded?.needs_allow_downtime === true
  const canStart =
    loaded !== null &&
    loaded.allowed &&
    !behind &&
    !invalid &&
    !plan.stale &&
    (!needsDowntime || allowDowntime)

  const start = useAction(
    () =>
      api.startClusterUpgrade(cluster, {
        version: loaded?.target_version,
        batch: loaded?.batch || undefined,
        allow_downtime: needsDowntime && allowDowntime,
      }),
    { toError: (failure) => ({ message: failureMessage(failure) }) },
  )

  async function submit(event: React.FormEvent) {
    event.preventDefault()
    if (!canStart) return
    const result = await start.run()
    if (result.ok) {
      toast.success('Upgrade started.')
      onStarted()
      onClose()
    }
  }

  const disabledReason = invalid
    ? 'Fix the version first'
    : behind || plan.stale
      ? 'Wait for the plan'
      : plan.loading || !loaded
        ? 'Wait for the plan'
        : !loaded.allowed
          ? 'The plan is refused'
          : needsDowntime && !allowDowntime
            ? 'Allow downtime to start'
            : undefined

  return (
    <form onSubmit={submit}>
      <DialogTitle>Upgrade cluster</DialogTitle>
      <DialogDescription>
        AerolVM restarts one node at a time. Running sandboxes keep running; each node’s API is down for a few
        seconds.
      </DialogDescription>
      <DialogBody className="space-y-6">
        <Field disabled={start.busy}>
          <Label>AerolVM version</Label>
          <Input
            value={version}
            placeholder="The tested release"
            invalid={invalid}
            onChange={(event) => setVersion(event.currentTarget.value.trim())}
          />
          {invalid ? (
            <ErrorMessage>
              {isLatest
                ? 'latest is not supported; give a release tag such as v0.7.1.'
                : 'A release tag such as v0.7.1.'}
            </ErrorMessage>
          ) : (
            <Description>Empty uses the AerolVM release this Lewbox release was tested with.</Description>
          )}
        </Field>

        {invalid ? null : plan.loading ? (
          <ListSkeleton label="Planning the upgrade" rows={4} />
        ) : (
          <div aria-busy={plan.stale || undefined} className="space-y-6">
            <ErrorNotice error={plan.error} onRetry={plan.reload} />
            {loaded && (
              <>
                <div className="flex flex-wrap items-center gap-3">
                  <Subheading>Upgrade to {loaded.target_version}</Subheading>
                  <Badge color={loaded.tested ? 'lime' : 'amber'}>
                    {loaded.tested ? 'Tested release' : 'Untested'}
                  </Badge>
                </div>

                {!loaded.allowed && (
                  <Notice variant="error">
                    <div>
                      This upgrade can’t start:
                      <ul className="mt-1 list-disc space-y-1 pl-5">
                        {loaded.refusals.map((refusal) => (
                          <li key={refusal}>{refusal}</li>
                        ))}
                      </ul>
                    </div>
                  </Notice>
                )}
                {loaded.warnings.length > 0 && (
                  <Notice variant="warning">
                    <ul className="list-disc space-y-1 pl-5">
                      {loaded.warnings.map((warning) => (
                        <li key={warning}>{warning}</li>
                      ))}
                    </ul>
                  </Notice>
                )}

                <Text>
                  {loaded.skew.upgrade_from_min
                    ? `${loaded.allowed ? 'The jump is allowed' : 'The jump is refused'}: nodes must run ${loaded.skew.upgrade_from_min} or later.`
                    : 'The release does not say which versions it can upgrade from.'}
                </Text>
                <Text>
                  {loaded.one_way_migrations.length > 0
                    ? `Finalize runs one-way migrations ${loaded.one_way_migrations.join(', ')}; after it, rollback is no longer possible.`
                    : 'Rollback stays possible until you finalize.'}
                </Text>

                <Table>
                  <TableHead>
                    <TableRow>
                      <TableHeader className="w-0">Order</TableHeader>
                      <TableHeader>Node</TableHeader>
                      <TableHeader>Role</TableHeader>
                      <TableHeader>Version</TableHeader>
                      <TableHeader>Action</TableHeader>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {loaded.nodes.map((node) => (
                      <TableRow key={node.node}>
                        <TableCell className="text-zinc-500 dark:text-zinc-400">
                          {node.order ?? '—'}
                        </TableCell>
                        <TableCell className="font-medium">{node.node}</TableCell>
                        <TableCell>{node.role || '—'}</TableCell>
                        <TableCell className="text-zinc-500 dark:text-zinc-400">
                          {node.version || '—'}
                        </TableCell>
                        <TableCell>{planAction(node)}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>

                {needsDowntime && (
                  <Checkbox
                    checked={allowDowntime}
                    onChange={setAllowDowntime}
                    disabled={start.busy}
                    description="The cluster’s API is down while a Raft server restarts. Running sandboxes keep running."
                  >
                    Allow downtime
                  </Checkbox>
                )}
              </>
            )}
          </div>
        )}

        <ErrorNotice error={start.error} />
      </DialogBody>
      <DialogActions>
        <Button plain onClick={onClose} disabled={start.busy}>
          Cancel
        </Button>
        <Button
          type="submit"
          loading={start.loading}
          disabled={!canStart}
          disabledReason={disabledReason}
        >
          Start upgrade
        </Button>
      </DialogActions>
    </form>
  )
}

function planAction(node: api.ClusterUpgradePlanNode): string {
  if (node.action === 'skip_target') return 'Already on target'
  if (node.action === 'skip_down') return 'Down: skipped'
  return `Upgrade to ${node.target_version}`
}
