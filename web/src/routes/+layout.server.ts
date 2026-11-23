import type { Flow } from "$lib/types";
import { get } from "$lib/server/record";

export async function load() {
  const { flows } = await get<{ flows: { name: string; jobs: number }[] }>(
    "/v1/flows",
  );
  const full: Flow[] = await Promise.all(
    flows.map(async (flow) => {
      const { jobs } = await get<{ jobs: Flow["jobs"] }>(
        `/v1/flows/${encodeURIComponent(flow.name)}/jobs`,
      );
      return { name: flow.name, jobs };
    }),
  );
  return { flows: full };
}
