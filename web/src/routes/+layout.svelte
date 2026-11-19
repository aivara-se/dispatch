<script lang="ts">
  import '../app.css'
  import { page } from '$app/stores'
  import { invalidateAll } from '$app/navigation'
  import { onMount } from 'svelte'
  import { age } from '$lib/age'
  import type { Card } from '$lib/types'

  let { data, children } = $props()

  onMount(() => {
    const source = new EventSource('/api/stream')
    source.onmessage = () => invalidateAll()
    return () => source.close()
  })

  let all = $derived(data.flows.flatMap((flow) => flow.jobs))
  let groups = $derived([
    { key: 'blocked', label: 'needs attention', jobs: all.filter((job) => job.state === 'blocked') },
    { key: 'started', label: 'running', jobs: all.filter((job) => job.state === 'started') },
    { key: 'done', label: 'completed', jobs: all.filter((job) => job.state === 'done') },
  ])

  const href = (job: Card) =>
    `/jobs/${encodeURIComponent(job.flow)}/${encodeURIComponent(job.owner)}/${encodeURIComponent(job.repo)}/${encodeURIComponent(job.key)}`
</script>

<header class="app">
  <a class="brand" href="/">Dispatch</a>
  <span class="readonly">reads the record</span>
</header>

<div class="shell">
  <aside class="rail" aria-label="Jobs">
    <div class="list">
      {#each groups as group}
        {#if group.jobs.length > 0}
          <h2 class="gh {group.key}">
            {group.label}<span class="count">{group.jobs.length}</span>
          </h2>
          {#each group.jobs as job}
            <a class="item" class:current={$page.url.pathname === href(job)} href={href(job)}>
              <div class="l1">
                <span class="rid">{job.flow} · #{job.key}</span>
                <span class="age">{age(job.cause)}</span>
              </div>
              <div class="subject">{job.title}</div>
              <div class="l2">
                <span class="state {job.state}">{job.state}</span>
                <span class="verdict">{job.verdict}</span>
              </div>
            </a>
          {/each}
        {/if}
      {/each}
      {#if all.length === 0}
        <p class="empty">No jobs yet.</p>
      {/if}
    </div>
  </aside>
  <main class="detail">
    {@render children()}
  </main>
</div>

<style>
  .readonly {
    font: 10px/1 var(--sans);
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--muted);
  }
  .count {
    margin-left: auto;
    color: var(--muted);
  }
  .l2 {
    display: flex;
    gap: 8px;
    margin-top: 4px;
    font: 10px/1.4 var(--sans);
  }
  .state {
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }
  .state.blocked {
    color: var(--warn);
  }
  .state.started {
    color: var(--accent);
  }
  .state.done {
    color: var(--ok);
  }
  .verdict {
    color: var(--muted);
  }
</style>
