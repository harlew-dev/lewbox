import type { Node, NodeList, NodeListQuery } from '../../types/node'
import { query, ref, request, toPage, type PageBody } from './client'

export async function listNodes(filter: NodeListQuery = {}): Promise<NodeList> {
  return toPage(await request<PageBody<Node>>(`/v0/nodes${query(filter)}`))
}

export function removeNode(nodeRef: string, force: boolean): Promise<Node> {
  return request<Node>(`/v0/nodes/${ref(nodeRef)}${query({ force: force ? 'true' : undefined })}`, {
    method: 'DELETE',
  })
}

export function addNode(input: { name: string; url: string }): Promise<Node> {
  return request<Node>('/v0/nodes', { method: 'POST', body: input })
}
