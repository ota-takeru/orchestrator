import type { Meta, StoryObj } from "@storybook/react-vite";
import App from "./App";
import type { DashboardWireData, ProjectListData, ProjectPathSuggestion } from "./types";

const now = "2026-05-27T00:00:00Z";

const pathSuggestion: ProjectPathSuggestion = {
  display_name: "New Project",
  slug: "new-project",
  authority_runtime: "windows",
  base_path: "C:\\Users\\otata\\Desktop\\programming",
  project_root: "C:\\Users\\otata\\Desktop\\programming\\new-project"
};

const projects: ProjectListData = {
  projects: [],
  runtime_options: [
    {
      authority_runtime: "windows",
      label: "Windows",
      description: "Create and operate the project from this Windows host.",
      detected: true,
      available: true,
      recommended: true
    },
    {
      authority_runtime: "wsl",
      label: "WSL",
      description: "Use the configured WSL authority for Linux-first projects.",
      detected: true,
      available: true,
      recommended: false,
      wsl_distro: "Ubuntu"
    }
  ],
  project_path_suggestion: pathSuggestion
};

const dashboard: DashboardWireData = {
  snapshot: {
    project_id: "storybook",
    generated_at: now,
    counts: {
      open_inbox_items: 0,
      running_tasks: 0,
      waiting_for_human_tasks: 0,
      blocked_tasks: 0,
      queued_requests: 0,
      open_decisions: 0,
      running_workers: 0,
      open_merge_queue: 0,
      baseline_issues: 0
    },
    open_inbox_items: [],
    recommended_next_commands: ["devos request --json <TEXT>"]
  },
  tasks: [],
  feature_requests: [],
  queue_items: [],
  work_status: {
    worker_runs: [],
    planning: {
      runs: [],
      artifacts: [],
      queue: []
    }
  },
  planning_status: {
    runs: [],
    artifacts: [],
    queue: []
  },
  change_requests: [],
  dependency_risks: [],
  decisions: [],
  baseline_issues: [],
  artifacts: [],
  trusted_artifacts: [],
  path_mappings: [],
  toolchain_setup_cards: [],
  merge_status: {
    queue: [],
    blockers: ["No queued tasks"],
    blocking_inbox_items: [],
    ready: false
  },
  project_violations: [],
  setup_status: {
    project_root: "C:\\Users\\otata\\Desktop\\programming\\orchestrator",
    git_repository: true,
    git_clean: true,
    git_dirty_files: [],
    gitignore_env_local: true,
    required_verification_configured: true,
    protected_paths: [".env", ".devagent"],
    environment_bindings: [],
    toolchain_setup_cards: [],
    actions: [],
    blockers: []
  }
};

const meta = {
  title: "App/Human Inbox",
  component: App,
  parameters: {
    layout: "fullscreen"
  },
  beforeEach: () => {
    const originalFetch = window.fetch;
    window.fetch = async (input, init) => mockFetch(input, init);
    return () => {
      window.fetch = originalFetch;
    };
  }
} satisfies Meta<typeof App>;

export default meta;

type Story = StoryObj<typeof meta>;

export const EmptyProjectRegistry: Story = {};

async function mockFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const url = new URL(typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url, window.location.origin);
  const method = init?.method ?? "GET";

  if (url.pathname === "/api/projects" && method === "GET") {
    return json(projects);
  }

  if (url.pathname === "/api/ui/snapshot") return json(dashboard.snapshot);
  if (url.pathname === "/api/tasks") return json({ tasks: dashboard.tasks });
  if (url.pathname === "/api/requests") return json({ requests: dashboard.feature_requests });
  if (url.pathname === "/api/queue") return json({ items: dashboard.queue_items });
  if (url.pathname === "/api/work/status") return json(dashboard.work_status);
  if (url.pathname === "/api/planning/status") return json(dashboard.planning_status);
  if (url.pathname === "/api/change-requests") return json({ change_requests: dashboard.change_requests });
  if (url.pathname === "/api/dependency-risks") return json({ risks: dashboard.dependency_risks });
  if (url.pathname === "/api/decisions") return json({ decisions: dashboard.decisions });
  if (url.pathname === "/api/memory") return json({ memories: dashboard.baseline_issues });
  if (url.pathname === "/api/artifacts") return json({ artifacts: dashboard.artifacts });
  if (url.pathname === "/api/artifacts/trusted") return json({ artifacts: dashboard.trusted_artifacts });
  if (url.pathname === "/api/platform/path-mappings") return json({ mappings: dashboard.path_mappings });
  if (url.pathname === "/api/platform/toolchain-setup") return json({ cards: dashboard.toolchain_setup_cards });
  if (url.pathname === "/api/merge/status") return json(dashboard.merge_status);
  if (url.pathname === "/api/check") return json({ violations: dashboard.project_violations });
  if (url.pathname === "/api/setup") return json(dashboard.setup_status);

  if (url.pathname === "/api/project-paths/suggest" || url.pathname === "/api/projects/path-suggest") {
    return json(suggestedPath(url));
  }

  if (method === "POST") {
    return json({});
  }

  return new Response(`Unhandled mock route: ${method} ${url.pathname}`, { status: 404 });
}

function suggestedPath(url: URL): ProjectPathSuggestion {
  const displayName = url.searchParams.get("name")?.trim() || "New Project";
  const slug = displayName
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/(^-|-$)/g, "") || "new-project";
  const basePath = url.searchParams.get("base")?.trim() || pathSuggestion.base_path;
  return {
    ...pathSuggestion,
    display_name: displayName,
    slug,
    base_path: basePath,
    project_root: `${basePath}\\${slug}`
  };
}

function json(body: unknown) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: {
      "Content-Type": "application/json"
    }
  });
}
