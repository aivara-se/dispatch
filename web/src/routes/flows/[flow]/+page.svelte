<script lang="ts">
  import { age } from '$lib/age'
  import type { Card } from '$lib/types'

  let { data } = $props()

  const href = (job: Card) =>
    `/jobs/${encodeURIComponent(job.flow)}/${encodeURIComponent(job.owner)}/${encodeURIComponent(job.repo)}/${encodeURIComponent(job.key)}`
</script>

<svelte:head>
  <title>{data.flow} · Dispatch</title>
</svelte:head>

<div class="ihead">
  <div class="ref"><span class="flowlab">{data.flow}</span></div>
  <h1>{data.flow}</h1>
  <p class="cause">{data.jobs.length} jobs</p>
</div>

<div class="rows">
  {#each data.jobs as job}
    <a class="row" href={href(job)}>
      <span class="rowtop"><span class="mono">{job.ref}</span><span class="age">{age(job.cause)}</span></span>
      <span class="rowtitle">{job.title}</span>
      <span class="rowstate">{job.state} · {job.verdict}</span>
    </a>
  {:else}
    <p class="empty">This flow has no jobs.</p>
  {/each}
</div>
