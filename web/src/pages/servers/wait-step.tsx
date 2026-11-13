import { Step, Text, TextLink } from '@harlew-dev/ui'
import { CheckCircleIcon, ExclamationTriangleIcon } from '@heroicons/react/16/solid'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import * as api from '../../lib/api'

// How often to ask. Tests shorten it.
export const waitTiming = { every: 3000 }

export type WaitResult<T> = { state: 'waiting' } | { state: 'done'; value: T } | { state: 'taken'; value: T }

// usePollFor asks `check` every few seconds until it stops answering
// `waiting`. It first loads a baseline (what was there before the commands
// ran) and hands it to every check. An error (a dropped connection) just
// means "not yet".
export function usePollFor<B, T>(
  baseline: () => Promise<B>,
  check: (before: B) => Promise<WaitResult<T>>,
  key: string,
): WaitResult<T> {
  const [result, setResult] = useState<WaitResult<T>>({ state: 'waiting' })
  const latest = useRef({ baseline, check })
  latest.current = { baseline, check }

  useEffect(() => {
    let stopped = false
    let timer = 0
    setResult({ state: 'waiting' })
    const run = async () => {
      let before: B
      try {
        before = await latest.current.baseline()
      } catch {
        if (!stopped) timer = window.setTimeout(run, waitTiming.every)
        return
      }
      const poll = async () => {
        if (stopped) return
        let next: WaitResult<T> = { state: 'waiting' }
        try {
          next = await latest.current.check(before)
        } catch {
          // Not there yet.
        }
        if (stopped) return
        if (next.state === 'waiting') timer = window.setTimeout(poll, waitTiming.every)
        else setResult(next)
      }
      await poll()
    }
    void run()
    return () => {
      stopped = true
      window.clearTimeout(timer)
    }
  }, [key])

  return result
}

function Status({
  result,
  waiting,
  done,
  taken,
}: {
  result: WaitResult<unknown>
  waiting: string
  done: ReactNode
  taken: string
}) {
  if (result.state === 'done') {
    return (
      <Text className="flex items-center gap-2">
        <CheckCircleIcon className="size-4 shrink-0 fill-green-600 dark:fill-green-500" aria-hidden="true" />
        <span>{done}</span>
      </Text>
    )
  }
  if (result.state === 'taken') {
    return (
      <Text className="flex items-center gap-2">
        <ExclamationTriangleIcon className="size-4 shrink-0 fill-amber-500" aria-hidden="true" />
        <span>{taken}</span>
      </Text>
    )
  }
  return (
    <Text className="flex items-center gap-2">
      <span
        aria-hidden="true"
        className="size-4 shrink-0 animate-spin rounded-full border-2 border-zinc-950/10 border-t-zinc-500 motion-reduce:animate-none dark:border-white/10 dark:border-t-zinc-400"
      />
      <span>{waiting}</span>
    </Text>
  )
}

const clusterPath = (slug: string) => `/admin/servers/clusters/${encodeURIComponent(slug)}`

// The last step of "Start a new cluster": waits for the cluster to be
// registered here, which `lewbox node add --new-cluster` does as it finishes.
export function WaitForClusterStep({ slug }: { slug: string }) {
  const find = async () => {
    try {
      return await api.getCluster(slug)
    } catch (failure) {
      if (failure instanceof api.ApiError && failure.status === 404) return null
      throw failure
    }
  }
  const result = usePollFor(
    find,
    async (before): Promise<WaitResult<api.Cluster>> => {
      if (before) return { state: 'taken', value: before }
      const cluster = await find()
      return cluster ? { state: 'done', value: cluster } : { state: 'waiting' }
    },
    slug,
  )
  return (
    <Step
      title="Wait for the cluster"
      description="This page checks every few seconds; you can leave it open while the commands run."
    >
      <div role="status" aria-live="polite">
        <Status
          result={result}
          waiting={`Waiting for cluster “${slug}” to connect…`}
          done={
            <>
              Cluster “{slug}” is connected. <TextLink href={clusterPath(slug)}>Open it</TextLink>.
            </>
          }
          taken={`A cluster named “${slug}” already exists here. Go back and choose another slug.`}
        />
      </div>
    </Step>
  )
}

// The last step of "Add a node": waits for a node that wasn't in the cluster
// when the commands were shown. With a name, only that node counts.
export function WaitForNodeStep({ cluster, node }: { cluster: string; node?: string }) {
  const list = async () => (await api.listNodes(cluster)).data
  const result = usePollFor(
    list,
    async (before): Promise<WaitResult<api.Node>> => {
      const known = new Set(before.map((item) => item.id))
      if (node) {
        const existing = before.find((item) => item.name === node)
        if (existing) return { state: 'taken', value: existing }
      }
      const added = (await list()).find((item) => !known.has(item.id) && (!node || item.name === node))
      return added ? { state: 'done', value: added } : { state: 'waiting' }
    },
    `${cluster}/${node ?? ''}`,
  )
  const name = node ? `node “${node}”` : 'the new node'
  return (
    <Step
      title="Wait for the node"
      description="This page checks every few seconds; you can leave it open while the commands run."
    >
      <div role="status" aria-live="polite">
        <Status
          result={result}
          waiting={`Waiting for ${name} to join “${cluster}”…`}
          done={`Node “${result.state === 'done' ? result.value.name : node}” joined “${cluster}”.`}
          taken={`“${node}” is already a node of “${cluster}”. Go back and choose another name.`}
        />
      </div>
    </Step>
  )
}
