<script lang="ts">
  import { age, hhmm } from '$lib/age'
  import type { Action } from '$lib/types'
  import { pick } from '$lib/selection'

  let { data } = $props()

  let job = $derived(data.job)
  let failing = $derived(job.tasks.length > 0 ? job.tasks[job.tasks.length - 1].n : null)
  let id = $derived(`${job.flow}/${job.owner}/${job.repo}/${job.key}`)
  let picked = $state<{ id: string; n: number } | null>(null)
  let current = $derived(pick(picked, id, failing))
  let task = $derived(job.tasks.find((item) => item.n === current))

  const carried = (action: Action) =>
    action.type === 'script'
      ? [action.path, ...(action.args ?? [])].join(' ')
      : action.type === 'action'
        ? [action.name, action.value].filter(Boolean).join(' ')
        : (action.text ?? '')
</script>

<svelte:head>
  <title>{job.title} · Dispatch</title>
</svelte:head>

<div class="ihead">
  <div class="ref">
    <span class="flowlab">{job.flow}</span>
    <a href={job.link} target="_blank" rel="noreferrer">{job.ref}</a>
  </div>
  <h1>{job.title}</h1>
  <p class="cause">
    <span class="state {job.state}">{job.state}</span>
    <span class="sep">·</span>{job.verdict}
    <span class="sep">·</span>{age(job.cause)}
  </p>
</div>

{#if job.alert}
  <div class="alert"><b>Alert:</b><span>{job.alert}</span></div>
{/if}

<div class="body">
  <nav class="tasks" aria-label="Tasks">
    {#each job.tasks as item}
      <button
        class="task"
        class:current={item.n === current}
        class:failing={item.n === failing && job.state === 'blocked'}
        aria-current={item.n === current}
        onclick={() => (picked = { id, n: item.n })}
      >
        <span class="n">{item.n}</span>
        <span class="short">{item.short}</span>
        <span class="at">{hhmm(item.at)}{#if item.n === failing && job.state === 'blocked'} · <span class="failed">failed</span>{/if}</span>
      </button>
    {/each}
  </nav>

  <div class="actions">
    {#if task}
      <section class:failing={task.n === failing && job.state === 'blocked'}>
        <h2>Task {task.n} <span class="dim">· {task.short}</span></h2>
        {#each task.actions as action}
          <article class="action">
            <div class="ahead">
              <span class="type">{action.type}{#if action.agent}<span class="dim"> · {action.agent}</span>{/if}</span>
              <span class="exit" class:failed={action.exit !== null && action.exit !== 0}>
                {action.exit === null ? 'not run' : action.exit === 0 ? 'exit 0' : `failed · exit ${action.exit}`}
              </span>
            </div>
            {#if carried(action)}
              <p class="carried" class:mono={action.type !== 'prompt'}>{carried(action)}</p>
            {/if}
            {#if action.skipped}
              <p class="skipped">skipped</p>
            {:else if action.output}
              <pre>{action.output}</pre>
            {/if}
          </article>
        {/each}
      </section>
    {/if}
  </div>
</div>
