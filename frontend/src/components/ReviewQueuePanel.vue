<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { fetchReviewQueue } from '../api/review-queue';
import { request } from '../api/client';
import { useAuth } from '../hooks/useAuth';
import type { PrioritySuggestedLevel, ReviewQueueItem, ReviewQueueResponse } from '../types/domain';
import SeverityBadge from './common/SeverityBadge.vue';
import ConfirmDialog from './common/ConfirmDialog.vue';

const emit = defineEmits<{ reviewed: [] }>();
const { session } = useAuth();

const levels: Array<{ value: '' | PrioritySuggestedLevel; label: string }> = [
  { value: '', label: '全部建议等级' },
  { value: 'observe', label: '观察 observe' },
  { value: 'restrict', label: '限速 restrict' },
  { value: 'urgent', label: '立即处置 urgent' },
];
const levelLabel: Record<PrioritySuggestedLevel, string> = { observe: '观察', restrict: '限速', urgent: '立即处置' };
const levelClass: Record<PrioritySuggestedLevel, string> = { observe: 'suggestion--observe', restrict: 'suggestion--restrict', urgent: 'suggestion--urgent' };

const queue = ref<ReviewQueueResponse | null>(null);
const filter = ref<'' | PrioritySuggestedLevel>('');
const loading = ref(false);
const error = ref('');
const pending = ref<{ item: ReviewQueueItem; status: PrioritySuggestedLevel } | null>(null);
const submitting = ref(false);

async function load() {
  loading.value = true;
  error.value = '';
  try {
    queue.value = (await fetchReviewQueue(filter.value)).data;
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : String(reason);
  } finally {
    loading.value = false;
  }
}

onMounted(load);

async function confirmReview() {
  if (!pending.value || submitting.value) return;
  submitting.value = true;
  try {
    await request(`/priorities/${pending.value.item.id}/transition`, {
      method: 'POST',
      body: JSON.stringify({
        status: pending.value.status,
        expectedVersion: pending.value.item.version,
        reason: '复核员依据待复核队列排序与建议等级完成独立复核',
      }),
    });
    pending.value = null;
    await load();
    // 定稿后原列表也需要刷新，交给父页面统一处理。
    emit('reviewed');
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : String(reason);
  } finally {
    submitting.value = false;
  }
}

function formatHours(hours: number): string {
  if (hours < 1) return `${Math.round(hours * 60)} 分钟`;
  return `${hours.toFixed(1)} 小时`;
}
</script>

<template>
	<section class="review-queue">
		<header class="review-queue__head">
			<div>
				<p class="eyebrow">复核员工作队列</p>
				<h2>待复核队列</h2>
				<p>按风险等级 → 指标值 → 等待时长排序，风险高、指标高、等得久的草稿排在前面；你本人拟制的草稿不进入此队列。</p>
			</div>
			<el-select v-model="filter" style="width: 200px" @change="load">
				<el-option v-for="option in levels" :key="option.value || 'all'" :value="option.value" :label="option.label" />
			</el-select>
		</header>

		<div class="review-queue__summary" v-if="queue">
			<span>待复核草稿 <strong>{{ queue.totalDrafts }}</strong></span>
			<span>你可复核 <strong>{{ queue.visibleDrafts }}</strong></span>
			<span>本人拟制已排除 <strong>{{ queue.excludedOwn }}</strong></span>
			<span v-if="filter">筛选后 <strong>{{ queue.items.length }}</strong></span>
			<el-button link type="primary" :loading="loading" @click="load">刷新排序</el-button>
		</div>

		<el-alert v-if="error" :title="error" type="error" show-icon />

		<div class="table-shell">
			<el-table v-loading="loading" :data="queue?.items ?? []">
				<el-table-column label="序" width="56">
					<template #default="{ row }"><strong class="queue-rank">#{{ row.order }}</strong></template>
				</el-table-column>
				<el-table-column label="草稿" min-width="180">
					<template #default="{ row }"><strong>{{ row.name }}</strong><small>{{ row.code }} · {{ row.facility }}</small></template>
				</el-table-column>
				<el-table-column label="风险" width="90">
					<template #default="{ row }"><SeverityBadge :severity="row.riskLevel" /></template>
				</el-table-column>
				<el-table-column label="指标值" width="120">
					<template #default="{ row }">{{ row.metricValue }} {{ row.metricUnit }}</template>
				</el-table-column>
				<el-table-column label="等待时长" width="120">
					<template #default="{ row }"><strong>{{ formatHours(row.waitingHours) }}</strong><small>拟制人 {{ row.preparedBy }}</small></template>
				</el-table-column>
				<el-table-column label="建议等级" width="120">
					<template #default="{ row }"><span :class="`suggestion ${levelClass[row.suggestedLevel as PrioritySuggestedLevel]}`">{{ levelLabel[row.suggestedLevel as PrioritySuggestedLevel] }}</span></template>
				</el-table-column>
				<el-table-column label="排序原因" min-width="150">
					<template #default="{ row }">
						<el-popover placement="left" :width="340" trigger="click">
							<template #reference><el-button link type="primary">查看排序原因</el-button></template>
							<ul class="reason-list">
								<li v-for="(reason, index) in row.orderingReasons" :key="index">{{ reason }}</li>
							</ul>
						</el-popover>
					</template>
				</el-table-column>
				<el-table-column label="复核操作" width="280">
					<template #default="{ row }">
						<div class="row-actions">
							<el-button v-for="target in (['observe', 'restrict', 'urgent'] as PrioritySuggestedLevel[])" :key="target"
								:type="target === row.suggestedLevel ? 'primary' : 'default'" link
								@click="pending = { item: row, status: target }">
								{{ target === row.suggestedLevel ? `定为${levelLabel[target]}(建议)` : `定为${levelLabel[target]}` }}
							</el-button>
						</div>
					</template>
				</el-table-column>
				<template #empty>
					<div class="empty queue-empty">{{ queue?.emptyReason || '没有符合筛选条件的待复核草稿。' }}</div>
				</template>
			</el-table>
		</div>

		<ConfirmDialog :model-value="Boolean(pending)" title="确认独立复核" @update:model-value="pending = null" @confirm="confirmReview">
			<p>该操作会将草稿定稿并写入版本审计；复核人 {{ session?.username }} 与拟制人职责分离已由系统校验。</p>
			<strong v-if="pending">{{ pending.item.code }}：draft → {{ pending.status }}（建议 {{ pending.item.suggestedLevel }}）</strong>
		</ConfirmDialog>
	</section>
</template>

<style scoped>
.review-queue { background: white; border: 1px solid #dbe4e8; border-top: 3px solid #2a5d9d; padding: 16px 18px 18px; margin-bottom: 26px; }
.review-queue__head { display: flex; justify-content: space-between; align-items: flex-start; gap: 18px; margin-bottom: 12px; }
.review-queue__head h2 { margin: 4px 0 6px; font-size: 20px; }
.review-queue__head p:last-child { margin: 0; color: #667886; font-size: 13px; max-width: 820px; }
.review-queue__summary { display: flex; flex-wrap: wrap; gap: 18px; align-items: center; color: #526473; font-size: 13px; background: #f4f8fa; border: 1px solid #e3ebef; padding: 9px 12px; margin-bottom: 12px; }
.review-queue__summary strong { color: #173d5c; font-size: 15px; margin-left: 3px; }
.queue-rank { color: #2a5d9d; font-size: 15px; }
.suggestion { display: inline-flex; border-radius: 3px; padding: 4px 9px; font-size: 12px; font-weight: 750; }
.suggestion--observe { color: #176c55; background: #dff3ec; }
.suggestion--restrict { color: #8a5a08; background: #fff1ce; }
.suggestion--urgent { color: white; background: #a52331; }
.reason-list { margin: 0; padding-left: 18px; display: grid; gap: 7px; color: #41525e; font-size: 13px; line-height: 1.5; }
.queue-empty { padding: 34px 20px; line-height: 1.7; }
</style>
