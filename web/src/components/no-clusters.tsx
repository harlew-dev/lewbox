import { Button, Code, EmptyState } from '@harlew-dev/ui'

export function NoClusters({ action = '/docs/clusters/start-a-cluster' }: { action?: string }) {
  return (
    <EmptyState
      title="No clusters yet"
      description="Start an AerolVM cluster on a machine and register it with Lewbox by running lewbox node add with --new-cluster."
      action={
        <div className="flex flex-col items-center gap-3">
          <Code className="rounded-md bg-zinc-950/5 px-3 py-1.5 dark:bg-white/10">
            lewbox node add node-1 --new-cluster eu-west
          </Code>
          <Button href={action}>Read the cluster guide</Button>
        </div>
      }
    />
  )
}
