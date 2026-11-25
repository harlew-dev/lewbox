import type { Plan, PlanInput } from '../../types/plan'
import { ref, request } from './client'

export function listPlans(): Promise<{ data: Plan[] }> {
  return request<{ data: Plan[] }>('/v0/plans')
}

export function createPlan(input: PlanInput): Promise<Plan> {
  return request<Plan>('/v0/plans', { method: 'POST', body: input })
}

export function updatePlan(planRef: string, input: PlanInput): Promise<Plan> {
  return request<Plan>(`/v0/plans/${ref(planRef)}`, { method: 'PATCH', body: input })
}

export function deletePlan(planRef: string): Promise<Plan> {
  return request<Plan>(`/v0/plans/${ref(planRef)}`, { method: 'DELETE' })
}
