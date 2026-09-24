
<script setup lang="ts">
import EntityPage from '../components/EntityPage.vue';
import ReviewQueuePanel from '../components/ReviewQueuePanel.vue';
import { ENTITY_CONFIGS } from '../types/status';
import { usePriorityDecisionStore } from '../stores/priority-decision';
import { useAuth } from '../hooks/useAuth';

const store = usePriorityDecisionStore();
const { canAtLeast } = useAuth();
</script>
<template>
  <EntityPage :config="ENTITY_CONFIGS[3]" :store="store">
    <template v-if="canAtLeast('reviewer')" #intro>
      <ReviewQueuePanel />
    </template>
  </EntityPage>
</template>
