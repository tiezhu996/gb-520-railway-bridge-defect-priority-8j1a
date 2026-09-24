<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { listReviewQueue } from '../api/priority-decision';
import type { PriorityReviewQueue, PriorityReviewItem, PrioritySuggestedLevel } from '../types/domain';
import SeverityBadge from './common/SeverityBadge.vue';

const queue = ref<PriorityReviewQueue | null>(null);
const loading = ref(false);
const error = ref('');
const filter = ref<PrioritySuggestedLevel | ''>('');

const filterOptions: { value: PrioritySuggestedLevel | ''; label: string }[] = [
  { value: '', label: '全部建议等级' },
  { value: 'observe', label: 'observe 观察' },
  { value: 'restrict', label: 'restrict 限速' },
  { value: 'urgent', label: 'urgent 立即处置' },
];

const levelTagType: Record<PrioritySuggestedLevel, 'info' | 'warning' | 'danger'> = {
  observe: 'info',
  restrict: 'warning',
  urgent: 'danger',
};
const levelLabel: Record<PrioritySuggestedLevel, string> = {
  observe: 'observe 观察',
  restrict: 'restrict 限速',
  urgent: 'urgent 立即处置',
};

async function load() {
  loading.value = true;
  error.value = '';
  try {
    queue.value = (await listReviewQueue(filter.value)).data;
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : String(reason);
  } finally {
    loading.value = false;
  }
}

onMounted(load);
</script>

<template>
  <section class="review-queue">
    <header class="review-queue__header">
      <div>
        <p class="eyebrow">待复核队列</p>
        <h2>优先处理顺序</h2>
        <p>仅含他人拟制的 draft 草稿，按风险等级 → 指标值 → 等待时长降序排列；本人拟制草稿不进队列。</p>
      </div>
      <div class="review-queue__actions">
        <el-select v-model="filter" style="width: 190px" @change="load">
          <el-option v-for="option in filterOptions" :key="option.value" :value="option.value" :label="option.label" />
        </el-select>
        <el-button @click="load" :loading="loading">刷新队列</el-button>
      </div>
    </header>

    <el-alert v-if="error" :title="error" type="error" show-icon />

    <p v-if="queue" class="review-queue__summary">
      草稿总数 {{ queue.totalDrafts }} 条 · 待复核 {{ queue.queueCount }} 条 ·
      已排除本人拟制 {{ queue.excludedOwnCount }} 条<template v-if="queue.filter"> · 建议等级筛选隐藏 {{ queue.filteredCount }} 条</template>
    </p>

    <div v-loading="loading" class="review-queue__body">
      <el-empty v-if="queue && queue.items.length === 0 && queue.emptyReason" :description="queue.emptyReason"/>
      <el-table v-else-if="queue" :data="queue.items" :row-class-name="(scope: { row: PriorityReviewItem }) => scope.row.rank === 1 ? 'review-queue__top' : ''">
        <el-table-column prop="rank" label="顺位" width="64"/>
        <el-table-column label="草稿" min-width="200">
          <template #default="{ row }">
            <strong>{{ row.code }}</strong>
            <small>{{ row.name }}</small>
            <small>{{ row.facility }}</small>
          </template>
        </el-table-column>
        <el-table-column label="风险等级" width="100">
          <template #default="{ row }"><SeverityBadge :severity="row.riskLevel"/><small>第 {{ row.riskRank }} 档</small></template>
        </el-table-column>
        <el-table-column label="指标值" width="110">
          <template #default="{ row }">{{ row.metricValue }} {{ row.metricUnit }}</template>
        </el-table-column>
        <el-table-column label="等待小时" width="100">
          <template #default="{ row }"><strong :class="{ 'wait-long': row.waitingHours >= 48 }">{{ row.waitingHours.toFixed(1) }} h</strong></template>
        </el-table-column>
        <el-table-column label="建议等级" width="150">
          <template #default="{ row }"><el-tag :type="levelTagType[row.suggestedLevel as PrioritySuggestedLevel]" disable-transitions>{{ levelLabel[row.suggestedLevel as PrioritySuggestedLevel] }}</el-tag></template>
        </el-table-column>
        <el-table-column prop="preparedBy" label="拟制人" width="110"/>
        <el-table-column label="排序原因" min-width="320">
          <template #default="{ row }"><small class="review-queue__reason">{{ row.sortReason }}</small></template>
        </el-table-column>
      </el-table>
    </div>
  </section>
</template>
