<script lang="ts">
  import { age, hhmm } from '$lib/age'
  import Icon from '$lib/Icon.svelte'
  import type { Action, Task } from '$lib/types'
  import { pick } from '$lib/selection'
  import { toolCalls } from '$lib/tools'

  let { data } = $props()

  let job = $derived(data.job)
  let id = $derived(`${job.flow}/${job.owner}/${job.repo}/${job.key}`)
  let open = $derived(job.state === 'blocked' && job.tasks.length > 0 ? job.tasks[job.tasks.length - 1].n : (job.tasks[0]?.n ?? 0))
  let picked = $state<{ id: string; n: number } | null>(null)
  let current = $derived(pick(picked, id, open))
  let task = $derived(job.tasks.find((item) => item.n === current))
  let tools = $derived(toolCalls(task?.tools))

  const faces = ['meme', 'momo', 'mama', 'mimi']
  const agent = (item: Task) => item.actions.find((action) => action.type === 'prompt' && action.agent)?.agent ?? ''
  const face = (item: Task) => {
    const name = agent(item).toLowerCase()
    return faces.includes(name) ? name : null
  }
  const carried = (action: Action) =>
    action.type === 'script'
      ? [action.path, ...(action.args ?? [])].join(' ')
      : action.type === 'action'
        ? [action.name, action.value].filter(Boolean).join(' ')
        : (action.text ?? '')
  const exitOf = (action: Action) =>
    action.exit === 0 ? 'exit 0' : action.exit === null ? 'skipped' : `exit ${action.exit}`
  const toneOf = (action: Action) =>
    action.exit === 0 ? undefined : action.exit === null ? 'skip' : '1'
</script>

<svelte:head>
  <title>{job.title} · Dispatch</title>
</svelte:head>

<div class="ihead">
  <div class="ref">
    <a href={job.link} target="_blank" rel="noreferrer">{job.ref}</a>
    <span class="flowlab">{job.flow}</span>
  </div>
  <h1>{job.title}</h1>
</div>

{#if job.alert}
  <p class="alert">
    <Icon name="alert" />
    <span><b>{job.state}.</b> {job.alert}</span>
  </p>
{/if}

<div class="body">
  <div class="tasks">
    <div class="stats">
      <div class="stat"><span class="lab">time</span><span class="v">{age(job.started, Date.parse(job.cause))}</span></div>
      <div class="stat"><span class="lab">verdict</span><span class="v">{job.verdict}</span></div>
    </div>
    {#each job.tasks as item}
      <button
        class="task"
        aria-pressed={item.n === current}
        onclick={() => (picked = { id, n: item.n })}
      >
        <span class="av" class:empty={!face(item)}>
          {#if face(item)}<img src="/agents/{face(item)}.jpg" alt={agent(item)} title={agent(item)} />{/if}
        </span>
        <span class="tc">
          <span class="t1"><span class="name">{item.short}</span><span class="dur">{hhmm(item.at)}</span></span>
        </span>
      </button>
    {/each}
  </div>

  <aside class="inspector" aria-label="Actions and tool calls">
    <div class="acts">
      {#each task?.actions ?? [] as action}
        <article class="act" data-exit={toneOf(action)}>
          <div class="ah">
            <span class="at">{action.type}</span>
            {#if action.agent}<span class="aa">{action.agent}</span>{/if}
            <span class="ax">{exitOf(action)}</span>
          </div>
          {#if carried(action)}
            <div class="ab" class:code={action.type !== 'prompt'}>{carried(action)}</div>
          {/if}
          {#if action.output}
            <div class="ao"><span class="k">output</span>{action.output}</div>
          {/if}
        </article>
      {/each}
      {#if tools.total > 0}
        <div class="act tools">
          <div class="ah"><span class="at">tool calls</span><span class="ax">{tools.total}</span></div>
          <ul class="tl">
            {#each tools.calls as call}
              <li><span class="tn">{call.name}</span>{#if call.detail}<span class="td">{call.detail}</span>{/if}</li>
            {/each}
            {#if tools.more > 0}<li class="more">+{tools.more} more</li>{/if}
          </ul>
        </div>
      {/if}
    </div>
  </aside>
</div>
