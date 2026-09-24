
import { request } from './client';
import type { ReviewQueueResponse } from '../types/domain';

// 待复核队列仅供复核员及以上使用；本人拟制的草稿由后端按职责分离排除。
export async function fetchReviewQueue(suggestedLevel = '') {
  const suffix = suggestedLevel ? `?suggestedLevel=${encodeURIComponent(suggestedLevel)}` : '';
  return request<ReviewQueueResponse>(`/priority-review-queue${suffix}`);
}
