<script lang="ts">
  let { data } = $props()
</script>

<svelte:head>
  <title>Dispatch</title>
</svelte:head>

{#if data.empty}
  <p class="empty">No flows are configured.</p>
{/if}
