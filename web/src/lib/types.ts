export type Action = {
  type: string;
  output: string | null;
  exit: number | null;
  skipped?: boolean;
  agent?: string;
  text?: string;
  path?: string;
  args?: string[];
  name?: string;
  value?: string;
};

export type Task = {
  n: number;
  short: string;
  at: string;
  actions: Action[];
  pc: unknown;
  evidence: unknown;
  tools: unknown;
};

export type Card = {
  flow: string;
  owner: string;
  repo: string;
  key: string;
  ref: string;
  title: string;
  state: string;
  verdict: string;
  cause: string;
  started: string;
  alert: string | null;
  link: string;
};

export type Job = Card & { tasks: Task[] };

export type Flow = { name: string; jobs: Card[] };
