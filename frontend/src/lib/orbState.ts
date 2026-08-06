/**
 * Which thinking-orb animation stands for which phase of a run.
 *
 * The orb states are verbs an agent can be doing, and so are our phases, so the
 * mapping is the whole point: a run that is reading the tree should not look
 * like one installing dependencies. `LiveRun` previously showed one pulsing dot
 * for every phase, which is the same problem its own docstring describes —
 * a stuck run and a working run rendering identically.
 *
 * The phase vocabulary is not invented here. It comes from the daemon:
 * `initial_test` / `actor` / `critic` from the file loop, `actor:<tool>` and
 * the rest from `sessionPhase` in pkg/daemon/session_run.go, plus the
 * `clone` / `install: <cmd>` / `test: <cmd>` progress phases. Two states go
 * deliberately unused — `listening` (there is no voice surface) and `shaping`
 * (held for the planner, which is not wired to an orb yet).
 */

/** The nine animations thinking-orbs ships. */
export type OrbState =
  | "working"
  | "searching"
  | "solving"
  | "listening"
  | "connecting"
  | "weaving"
  | "composing"
  | "breathing"
  | "shaping";

/**
 * Phases keyed by their head — the part before any `:`. A live phase arrives as
 * `install: npm ci` or `actor:read_file`, so the command or tool is split off
 * before the lookup.
 */
const BY_HEAD: Record<string, OrbState> = {
  // Fetching the repository: a constellation wiring itself.
  clone: "connecting",
  // A command churning through work with no verdict of its own.
  install: "working",
  // A suite scrambling and then clicking back into pass or fail.
  test: "solving",
  initial_test: "solving",
  // Writing.
  actor: "composing",
  implementer: "composing",
  // Deliberation before a verdict, rather than the verdict itself.
  critic: "breathing",
  // Folding a long transcript back together.
  compaction: "weaving",
};

/** Session-mode tools, from the `actor:<tool>` phase. */
const BY_TOOL: Record<string, OrbState> = {
  read_file: "searching",
  list_files: "searching",
  grep: "searching",
  edit_file: "composing",
  write_file: "composing",
  run: "working",
};

/**
 * The orb for a phase string, falling back to `working` for anything the
 * daemon adds later. An unknown phase should still animate — going blank is a
 * worse failure than showing a generic verb.
 */
export function orbStateForPhase(phase: string | undefined | null): OrbState {
  if (!phase) return "working";
  const trimmed = phase.trim();
  const colon = trimmed.indexOf(":");
  if (colon === -1) return BY_HEAD[trimmed] ?? "working";

  const head = trimmed.slice(0, colon);
  const rest = trimmed.slice(colon + 1).trim();
  // `actor:read_file` names a tool; `install: npm ci` names a command. Only the
  // former narrows the state, and only when the tool is one we know.
  if (head === "actor" && rest) return BY_TOOL[rest] ?? BY_HEAD.actor;
  return BY_HEAD[head] ?? "working";
}

/** Minimal shape of a live progress task — avoids importing the full API type. */
interface RunningTask {
  status: string;
  phase?: string;
}

/** Minimal shape of a job task, which is where a queued task's block reason lives. */
interface QueuedTask {
  status: string;
  blocked_reason?: string;
}

/**
 * Block reasons that mean infrastructure is actively coming up.
 *
 * On the Free tier the provisioner cold-starts a per-org daemon container on
 * submit, and that wait is the longest in the whole product — a task can sit in
 * `provisioning` for tens of seconds before any phase exists to report. It is
 * real work, so it animates.
 *
 * Every other reason is excluded on purpose. `concurrency_cap` and
 * `waiting_on_dependencies` mean this task is doing nothing at all, and the
 * `problem` reasons mean it never will without intervention — animating either
 * would be the UI implying work is happening when none is, which is the thing
 * TaskDrawer's own comment about swapping a spinner for a static warning exists
 * to prevent.
 */
const STARTING_UP = new Set(["provisioning", "awaiting_runner"]);

/**
 * The orb to show for a whole job, or null to show none.
 *
 * Three cases, in precedence order: a task reporting a live phase wins; then a
 * task the control plane says is executing but which has not reported a phase
 * yet; then a runner being started. Anything else — queued behind a cap,
 * blocked, or finished — renders no orb, because none of those are thinking.
 *
 * `progress` and `tasks` are separate feeds: the progress endpoint 404s for a
 * job that never started, so a job waiting on a cold-starting runner has an
 * empty `progress` and its state is only visible on the job's own tasks.
 */
export function jobOrbState(
  progress: RunningTask[],
  tasks: QueuedTask[] = [],
): OrbState | null {
  const running = progress.find(t => t.status === "LEASED" || t.status === "RUNNING");
  if (running) return orbStateForPhase(running.phase);

  if (tasks.some(t => t.status === "LEASED" || t.status === "RUNNING")) return "working";

  const startingUp = tasks.some(
    t => t.status === "QUEUED" && t.blocked_reason && STARTING_UP.has(t.blocked_reason),
  );
  return startingUp ? "connecting" : null;
}
