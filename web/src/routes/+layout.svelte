<script lang="ts">
  import "../app.css";
  import { page } from "$app/stores";
  import { invalidateAll } from "$app/navigation";
  import { onMount } from "svelte";
  import { age } from "$lib/age";
  import { emptyLine } from "$lib/empty";
  import { jobHref } from "$lib/href";
  import Icon from "$lib/Icon.svelte";
  import type { Card } from "$lib/types";

  let { data, children } = $props();

  let query = $state("");

  onMount(() => {
    const source = new EventSource("/api/stream");
    source.onmessage = () => invalidateAll();
    const key = (event: KeyboardEvent) => {
      if (
        event.key === "/" &&
        document.activeElement?.id !== "f-q" &&
        !event.metaKey &&
        !event.ctrlKey
      ) {
        event.preventDefault();
        document.getElementById("f-q")?.focus();
      }
    };
    document.addEventListener("keydown", key);
    return () => {
      source.close();
      document.removeEventListener("keydown", key);
    };
  });

  let every = $derived(data.flows.flatMap((flow) => flow.jobs));
  let all = $derived(
    every.filter((job) => {
      const q = query.trim().toLowerCase();
      if (!q) return true;
      return [job.flow, job.owner, job.repo, job.key, job.ref, job.title].some(
        (value) => value.toLowerCase().includes(q),
      );
    }),
  );
  let empty = $derived(emptyLine(every.length, all.length));
  let groups: {
    key: string;
    label: string;
    icon: "alert" | "loader" | "check";
    jobs: Card[];
  }[] = $derived([
    {
      key: "blocked",
      label: "blocked",
      icon: "alert",
      jobs: all.filter((job) => job.state === "blocked"),
    },
    {
      key: "started",
      label: "started",
      icon: "loader",
      jobs: all.filter((job) => job.state === "started"),
    },
    {
      key: "done",
      label: "done",
      icon: "check",
      jobs: all.filter((job) => job.state === "done"),
    },
  ]);
</script>

<a class="skip-link" href="#detail">Skip to the record</a>

<header class="app">
  <a class="brand" href="/">Dispatch</a>
</header>

<div class="shell">
  <aside class="rail" aria-label="Pull requests">
    <div class="railtop">
      <div class="searchwrap">
        <Icon name="search" />
        <label class="sr" for="f-q">Search pull requests</label>
        <input
          id="f-q"
          class="search"
          type="search"
          placeholder="search pull requests"
          bind:value={query}
        />
      </div>
    </div>
    <div class="list">
      {#each groups as group}
        {#if group.jobs.length > 0}
          <h2 class="gh {group.key}">
            <Icon name={group.icon} size={12} /><span>{group.label}</span>
          </h2>
          {#each group.jobs as job}
            <a
              class="item"
              aria-current={$page.url.pathname === jobHref(job)
                ? "true"
                : undefined}
              href={jobHref(job)}
            >
              <span class="l1">
                <span class="subject">{job.repo}#{job.key}</span>
                <span class="age">{age(job.cause)}</span>
              </span>
            </a>
          {/each}
        {/if}
      {/each}
      {#if empty}
        <p class="empty">{empty}</p>
      {/if}
    </div>
  </aside>
  <main class="detail" id="detail" tabindex="-1">
    {@render children()}
  </main>
</div>
