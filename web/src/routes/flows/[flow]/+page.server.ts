import { error, redirect } from "@sveltejs/kit";
import type { Card } from "$lib/types";
import { jobHref } from "$lib/href";
import { get, NotFound } from "$lib/server/record";

export async function load({ params }) {
  let jobs: Card[] = [];
  try {
    jobs = (
      await get<{ jobs: Card[] }>(
        `/v1/flows/${encodeURIComponent(params.flow)}/jobs`,
      )
    ).jobs;
  } catch (cause) {
    if (cause instanceof NotFound) throw error(404, "no such flow");
    throw cause;
  }
  const open =
    jobs.find((job) => job.state === "blocked") ??
    jobs.find((job) => job.state === "started") ??
    jobs[0];
  if (open) throw redirect(302, jobHref(open));
  return { flow: params.flow };
}
