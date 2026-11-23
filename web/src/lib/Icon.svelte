<script lang="ts">
  let {
    name,
    size = 14,
  }: { name: "search" | "alert" | "loader" | "check"; size?: number } =
    $props();
</script>

<svg class="i" class:sm={size === 12} viewBox="0 0 24 24" aria-hidden="true">
  {#if name === "search"}
    <path d="m21 21-4.34-4.34" /><circle cx="11" cy="11" r="8" />
  {:else if name === "alert"}
    <path
      d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3"
    /><path d="M12 9v4" /><path d="M12 17h.01" />
  {:else if name === "loader"}
    <path d="M21 12a9 9 0 1 1-6.219-8.56" />
  {:else}
    <circle cx="12" cy="12" r="10" /><path d="m16 9-5.5 5.5L8 12" />
  {/if}
</svg>
