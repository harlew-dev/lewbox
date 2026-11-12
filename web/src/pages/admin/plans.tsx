import {
  Badge,
  Button,
  ConfirmationDialog,
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
  Dropdown,
  DropdownButton,
  DropdownItem,
  DropdownMenu,
  EmptyState,
  ErrorNotice,
  Field,
  FieldGroup,
  Input,
  Label,
  ListSkeleton,
  PageHeader,
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
import { useOpenCount } from '@harlew-dev/ui/lib/use-open-count'
import { EllipsisHorizontalIcon, PencilIcon, TrashIcon } from '@heroicons/react/16/solid'
import { useState, type FormEvent } from 'react'
import * as api from '../../lib/api'
import { formatLimit, formatMemory } from '../../lib/format'
import { invalidLimitReason, limitText, parseLimit, validLimits } from '../../lib/limits'
import { useDocumentTitle } from '../../lib/use-document-title'
import { failureMessage, useLoad } from '../../lib/use-load'

export function PlansPage() {
  useDocumentTitle('Plans')
  const plans = useLoad(() => api.listPlans(), [])
  const list = plans.data?.data ?? []
  const [editing, setEditing] = useState<api.Plan | 'new' | null>(null)
  const [deleting, setDeleting] = useState<api.Plan | null>(null)

  return (
    <>
      <PageHeader
        title="Plans"
        description="Named sets of limits. You give a customer a plan in each region."
        actions={<Button onClick={() => setEditing('new')}>New plan</Button>}
      />

      {plans.loading && !plans.data ? (
        <ListSkeleton label="Loading plans" rows={3} />
      ) : plans.error.message ? (
        <ErrorNotice error={plans.error} onRetry={plans.reload} />
      ) : list.length === 0 ? (
        <EmptyState
          title="No plans"
          description="A plan is a set of limits you give customers in a region."
          action={<Button onClick={() => setEditing('new')}>New plan</Button>}
        />
      ) : (
        <Table>
          <TableHead>
            <TableRow>
              <TableHeader>Plan</TableHeader>
              <TableHeader className="text-right">Sandboxes</TableHeader>
              <TableHeader className="text-right">vCPUs</TableHeader>
              <TableHeader className="text-right">Memory</TableHeader>
              <TableHeader className="text-right">Subscriptions</TableHeader>
              <TableHeader className="w-0">
                <span className="sr-only">Actions</span>
              </TableHeader>
            </TableRow>
          </TableHead>
          <TableBody>
            {list.map((plan) => (
              <TableRow key={plan.id}>
                <TableCell>
                  <div className="flex items-center gap-2 font-medium">
                    {plan.name}
                    {plan.default && <Badge color="zinc">Default</Badge>}
                  </div>
                  <Text size="sm">{plan.slug}</Text>
                </TableCell>
                <TableCell className="text-right tabular-nums">{formatLimit(plan.max_sandboxes)}</TableCell>
                <TableCell className="text-right tabular-nums">{formatLimit(plan.max_vcpus)}</TableCell>
                <TableCell className="text-right tabular-nums">{formatLimit(plan.max_memory_mb, formatMemory)}</TableCell>
                <TableCell className="text-right tabular-nums">{plan.subscriptions}</TableCell>
                <TableCell className="w-0 text-right">
                  <Dropdown>
                    <DropdownButton as={Button} plain aria-label={`Actions for ${plan.name}`}>
                      <EllipsisHorizontalIcon />
                    </DropdownButton>
                    <DropdownMenu anchor="bottom end">
                      <DropdownItem onClick={() => setEditing(plan)}>
                        <PencilIcon data-slot="icon" />
                        Edit plan
                      </DropdownItem>
                      <DropdownItem
                        variant="danger"
                        disabled={plan.default || plan.subscriptions > 0}
                        onClick={() => setDeleting(plan)}
                      >
                        <TrashIcon data-slot="icon" />
                        Delete plan
                      </DropdownItem>
                    </DropdownMenu>
                  </Dropdown>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      <PlanDialog plan={editing} onClose={() => setEditing(null)} onSaved={plans.refresh} />
      <ConfirmationDialog
        open={deleting !== null}
        onClose={() => setDeleting(null)}
        title={deleting ? `Delete ${deleting.name}?` : 'Delete plan?'}
        description="No customer has it in any region. This can’t be undone."
        confirm="Delete plan"
        variant="danger"
        onConfirm={async () => {
          if (deleting) await api.deletePlan(deleting.id)
        }}
        onConfirmed={() => {
          toast.success('Plan deleted.')
          plans.refresh()
        }}
      />
    </>
  )
}

function PlanDialog({
  plan,
  onClose,
  onSaved,
}: {
  plan: api.Plan | 'new' | null
  onClose: () => void
  onSaved: () => void
}) {
  const count = useOpenCount(plan !== null)
  return (
    <Dialog open={plan !== null} onClose={onClose} size="lg">
      {plan && <PlanForm key={count} plan={plan === 'new' ? null : plan} onClose={onClose} onSaved={onSaved} />}
    </Dialog>
  )
}

function PlanForm({ plan, onClose, onSaved }: { plan: api.Plan | null; onClose: () => void; onSaved: () => void }) {
  const [name, setName] = useState(plan?.name ?? '')
  const [slug, setSlug] = useState(plan?.slug ?? '')
  const [sandboxes, setSandboxes] = useState(limitText(plan?.max_sandboxes))
  const [vcpus, setVcpus] = useState(limitText(plan?.max_vcpus))
  const [memory, setMemory] = useState(limitText(plan?.max_memory_mb))
  const valid = validLimits(sandboxes, vcpus, memory)

  const save = useAction(
    () => {
      const input: api.PlanInput = {
        name: name.trim(),
        slug: slug.trim(),
        max_sandboxes: parseLimit(sandboxes),
        max_vcpus: parseLimit(vcpus),
        max_memory_mb: parseLimit(memory),
      }
      return plan ? api.updatePlan(plan.id, input) : api.createPlan(input)
    },
    { toError: (failure) => ({ message: failureMessage(failure) }) },
  )

  async function submit(event: FormEvent) {
    event.preventDefault()
    const result = await save.run()
    if (result.ok) {
      toast.success(plan ? 'Plan saved.' : `${result.value.name} created.`)
      onSaved()
      onClose()
    }
  }

  return (
    <form onSubmit={submit}>
      <DialogTitle>{plan ? `Edit ${plan.name}` : 'New plan'}</DialogTitle>
      <DialogDescription>Leave a limit empty for no limit.</DialogDescription>
      <DialogBody>
        <FieldGroup>
          <ErrorNotice error={save.error} />
          <div className="grid gap-4 sm:grid-cols-2">
            <Field disabled={save.busy}>
              <Label>Name</Label>
              <Input required autoFocus value={name} placeholder="Pro" onChange={(event) => setName(event.currentTarget.value)} />
            </Field>
            <Field disabled={save.busy}>
              <Label>Slug</Label>
              <Input required value={slug} placeholder="pro" onChange={(event) => setSlug(event.currentTarget.value)} />
            </Field>
          </div>
          <div className="grid gap-4 sm:grid-cols-3">
            <Field disabled={save.busy}>
              <Label>Sandboxes</Label>
              <Input type="number" min={1} step={1} value={sandboxes} placeholder="No limit" onChange={(event) => setSandboxes(event.currentTarget.value)} />
            </Field>
            <Field disabled={save.busy}>
              <Label>vCPUs</Label>
              <Input type="number" min={1} step={1} value={vcpus} placeholder="No limit" onChange={(event) => setVcpus(event.currentTarget.value)} />
            </Field>
            <Field disabled={save.busy}>
              <Label>Memory (MB)</Label>
              <Input type="number" min={1} step={1} value={memory} placeholder="No limit" onChange={(event) => setMemory(event.currentTarget.value)} />
            </Field>
          </div>
        </FieldGroup>
      </DialogBody>
      <DialogActions>
        <Button plain onClick={onClose} disabled={save.busy}>
          Cancel
        </Button>
        <Button
          type="submit"
          loading={save.loading}
          disabled={name.trim() === '' || slug.trim() === '' || !valid}
          disabledReason={valid ? 'Fill in required fields' : invalidLimitReason}
        >
          {plan ? 'Save plan' : 'Create plan'}
        </Button>
      </DialogActions>
    </form>
  )
}
