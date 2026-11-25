import type { Node, NodeList } from '../../types/node'
import { ref, request } from './client'

export function listNodes(cluster?: string): Promise<NodeList> {
  const query = cluster ? `?cluster=${ref(cluster)}` : ''
  return request<NodeList>(`/v0/nodes${query}`)
}

export function removeNode(nodeRef: string, force: boolean): Promise<Node> {
  const query = force ? '?force=true' : ''
  return request<Node>(`/v0/nodes/${ref(nodeRef)}${query}`, {
    method: 'DELETE',
  })
}

export function addNode(input: { name: string; url: string }): Promise<Node> {
  return request<Node>('/v0/nodes', { method: 'POST', body: input })
}
