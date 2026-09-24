<script setup lang="ts">
import EntityPage from '../components/EntityPage.vue';
import ReviewQueuePanel from '../components/ReviewQueuePanel.vue';
import { ENTITY_CONFIGS } from '../types/status';
import { usePriorityDecisionStore } from '../stores/priority-decision';
import { useAuth } from '../hooks/useAuth';

const store = usePriorityDecisionStore();
const { canAtLeast } = useAuth();

// 队列里完成定稿后刷新原列表，保持两套视图数据一致；审计链路由后端照常记录。
function refreshList() {
  void store.load(ENTITY_CONFIGS[3].path);
}
</script>

<template>
	<ReviewQueuePanel v-if="canAtLeast('reviewer')" @reviewed="refreshList" />
	<EntityPage :config="ENTITY_CONFIGS[3]" :store="store" />
</template>
