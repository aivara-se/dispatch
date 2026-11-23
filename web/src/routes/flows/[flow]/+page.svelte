<script lang="ts">
  let { data } = $props();
</script>

<svelte:head>
  <title>{data.flow} · Dispatch</title>
</svelte:head>

<div class="ihead">
  <div class="ref"><span class="flowlab">{data.flow}</span></div>
  <h1>{data.flow}</h1>
</div>

<p class="empty">This flow has no jobs.</p>
