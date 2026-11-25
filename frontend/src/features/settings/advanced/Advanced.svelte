<script lang="ts">
  import DistillationCard from './DistillationCard.svelte';
  import IntentAnalysisCard from './IntentAnalysisCard.svelte';
  import RuntimeInfoCard from './RuntimeInfoCard.svelte';
</script>

<!--
  Advanced settings: a vertical stack of capability cards, grouped by
  domain. New capabilities (embedding model, intent analyzer, multimodal
  analyzer, temperature/reasoning) each add a card — no layout surgery.
-->
<p class="group-label">Intelligence &amp; memory</p>
<IntentAnalysisCard />
<DistillationCard />

<p class="group-label">System</p>
<RuntimeInfoCard />
