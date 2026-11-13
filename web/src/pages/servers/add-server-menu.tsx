import {
  Button,
  Dropdown,
  DropdownButton,
  DropdownDescription,
  DropdownDivider,
  DropdownItem,
  DropdownLabel,
  DropdownMenu,
} from '@harlew-dev/ui'
import { ChevronDownIcon } from '@heroicons/react/16/solid'
import {
  LinkIcon,
  PlusIcon,
  RectangleStackIcon,
  ServerIcon,
} from '@heroicons/react/16/solid'
import { useState } from 'react'
import type * as api from '../../lib/api'
import { AddNodeDialog } from './add-node-dialog'
import { RegisterClusterDialog, RegisterNodeDialog } from './register-dialogs'
import { StartClusterDialog } from './start-cluster-dialog'

type Open = 'start' | 'join' | 'register-cluster' | 'register-node' | null

// Every way to add a server, in one menu: the page's primary action.
export function AddServerMenu({
  clusters,
  onChanged,
}: {
  clusters: api.Cluster[]
  onChanged: () => void
}) {
  const [open, setOpen] = useState<Open>(null)
  const close = () => setOpen(null)
  return (
    <>
      <Dropdown>
        <DropdownButton as={Button}>
          <PlusIcon />
          Add server
          <ChevronDownIcon />
        </DropdownButton>
        <DropdownMenu anchor="bottom end" className="min-w-80">
          <DropdownItem onClick={() => setOpen('start')}>
            <RectangleStackIcon data-slot="icon" />
            <DropdownLabel>Start a new cluster</DropdownLabel>
            <DropdownDescription>On a fresh machine, its first node</DropdownDescription>
          </DropdownItem>
          <DropdownItem disabled={clusters.length === 0} onClick={() => setOpen('join')}>
            <ServerIcon data-slot="icon" />
            <DropdownLabel>Add a node to a cluster</DropdownLabel>
            <DropdownDescription>Join another machine</DropdownDescription>
          </DropdownItem>
          <DropdownDivider />
          <DropdownItem onClick={() => setOpen('register-cluster')}>
            <LinkIcon data-slot="icon" />
            <DropdownLabel>Register an existing cluster</DropdownLabel>
            <DropdownDescription>With its admin URL and token</DropdownDescription>
          </DropdownItem>
          <DropdownItem onClick={() => setOpen('register-node')}>
            <LinkIcon data-slot="icon" />
            <DropdownLabel>Register a node by URL</DropdownLabel>
            <DropdownDescription>Without changing the machine</DropdownDescription>
          </DropdownItem>
        </DropdownMenu>
      </Dropdown>

      {/* Refresh the lists once the dialog closes, whether or not it saw the
          server connect. */}
      <StartClusterDialog
        open={open === 'start'}
        onClose={() => {
          close()
          onChanged()
        }}
      />
      <AddNodeDialog
        open={open === 'join'}
        onClose={() => {
          close()
          onChanged()
        }}
        clusters={clusters}
      />
      <RegisterClusterDialog open={open === 'register-cluster'} onClose={close} />
      <RegisterNodeDialog open={open === 'register-node'} onClose={close} onRegistered={onChanged} />
    </>
  )
}
