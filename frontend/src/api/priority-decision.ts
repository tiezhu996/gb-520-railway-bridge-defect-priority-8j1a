
import { request } from './client';
import type { DomainRecord, PriorityReviewQueue, PrioritySuggestedLevel } from '../types/domain';

export async function listPriorityDecision(page = 1, pageSize = 20, search = '') {
  return request<DomainRecord[]>(`/priorities?page=${page}&pageSize=${pageSize}&search=${encodeURIComponent(search)}`);
}
export async function listReviewQueue(suggestedLevel: PrioritySuggestedLevel | '' = '') {
  const filter = suggestedLevel ? `?suggestedLevel=${encodeURIComponent(suggestedLevel)}` : '';
  return request<PriorityReviewQueue>(`/priorities/review-queue${filter}`);
}
export async function createPriorityDecision(input: Partial<DomainRecord>) {
  return request<DomainRecord>('/priorities', { method: 'POST', body: JSON.stringify(input) });
}
export async function transitionPriorityDecision(id: number, status: string, expectedVersion: number, reason: string) {
  return request<DomainRecord>(`/priorities/${id}/transition`, {
    method: 'POST', body: JSON.stringify({ status, expectedVersion, reason }),
  });
}
