import type {
  ChangeRequest,
  DashboardData,
  Decision,
  DependencyRisk,
  FeatureRequest,
  HumanInboxSnapshot,
  InvariantViolation,
  MemoryRecord,
  MergeGateStatus,
  PathMapping,
  PlanningStatus,
  RegisteredProject,
  TaskRecord,
  ToolchainSetupCard,
  TrustedArtifact,
  WorkQueueItem,
  WorkStatus
} from "./types";

async function getJSON<T>(path: string): Promise<T> {
  const response = await fetch(path, {
    headers: {
      Accept: "application/json"
    }
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `${response.status} ${response.statusText}`);
  }
  return (await response.json()) as T;
}

export async function loadProjects(): Promise<RegisteredProject[]> {
  const body = await getJSON<{ projects: RegisteredProject[] }>("/api/projects");
  return body.projects;
}

export async function loadDashboardData(projectID?: string): Promise<DashboardData> {
  if (projectID) {
    return loadProjectDashboardData(projectID);
  }
  const [
    snapshot,
    taskBody,
    requestBody,
    queueBody,
    workStatus,
    planningStatus,
    changeRequestBody,
    dependencyRiskBody,
    decisionBody,
    memoryBody,
    artifactBody,
    pathMappingBody,
    setupBody,
    mergeStatus,
    checkBody
  ] = await Promise.all([
    getJSON<HumanInboxSnapshot>("/api/ui/snapshot?limit=12"),
    getJSON<{ tasks: TaskRecord[] }>("/api/tasks"),
    getJSON<{ requests: FeatureRequest[] }>("/api/requests"),
    getJSON<{ items: WorkQueueItem[] }>("/api/queue"),
    getJSON<WorkStatus>("/api/work/status"),
    getJSON<PlanningStatus>("/api/planning/status"),
    getJSON<{ change_requests: ChangeRequest[] }>("/api/change-requests"),
    getJSON<{ risks: DependencyRisk[] }>("/api/dependency-risks"),
    getJSON<{ decisions: Decision[] }>("/api/decisions?status=open"),
    getJSON<{ memories: MemoryRecord[] }>("/api/memory?type=baseline_issue"),
    getJSON<{ artifacts: TrustedArtifact[] }>("/api/artifacts/trusted"),
    getJSON<{ mappings: PathMapping[] }>("/api/platform/path-mappings"),
    getJSON<{ cards: ToolchainSetupCard[] }>("/api/platform/toolchain-setup"),
    getJSON<MergeGateStatus>("/api/merge/status"),
    getJSON<{ violations: InvariantViolation[] }>("/api/check")
  ]);

  return {
    snapshot,
    tasks: taskBody.tasks,
    featureRequests: requestBody.requests,
    queueItems: queueBody.items,
    workStatus,
    planningStatus,
    changeRequests: changeRequestBody.change_requests,
    dependencyRisks: dependencyRiskBody.risks,
    decisions: decisionBody.decisions,
    baselineIssues: memoryBody.memories,
    trustedArtifacts: artifactBody.artifacts,
    pathMappings: pathMappingBody.mappings,
    toolchainSetupCards: setupBody.cards,
    mergeStatus,
    projectViolations: checkBody.violations
  };
}

async function loadProjectDashboardData(projectID: string): Promise<DashboardData> {
  const base = `/api/projects/${encodeURIComponent(projectID)}`;
  const [snapshot, taskBody, inboxBody] = await Promise.all([
    getJSON<HumanInboxSnapshot>(`${base}/snapshot`),
    getJSON<{ tasks: TaskRecord[] }>(`${base}/tasks`),
    getJSON<{ items: HumanInboxSnapshot["open_inbox_items"] }>(`${base}/inbox?status=open`)
  ]);
  return {
    snapshot: { ...snapshot, open_inbox_items: snapshot.open_inbox_items ?? inboxBody.items },
    tasks: taskBody.tasks,
    featureRequests: [],
    queueItems: [],
    workStatus: emptyWorkStatus(),
    planningStatus: { runs: [], artifacts: [], queue: [] },
    changeRequests: [],
    dependencyRisks: [],
    decisions: [],
    baselineIssues: [],
    trustedArtifacts: [],
    pathMappings: [],
    toolchainSetupCards: [],
    mergeStatus: { queue: [], ready: true },
    projectViolations: []
  };
}

export async function approveInboxItem(id: string, notes: string, option?: string, projectID?: string): Promise<void> {
  const path = projectID ? `/api/projects/${encodeURIComponent(projectID)}/inbox/${encodeURIComponent(id)}/approve` : `/api/inbox/${encodeURIComponent(id)}/approve`;
  const response = await fetch(path, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json"
    },
    body: JSON.stringify({ notes, option })
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `${response.status} ${response.statusText}`);
  }
}

export async function createFeatureRequest(text: string, projectID?: string): Promise<void> {
  const path = projectID ? `/api/projects/${encodeURIComponent(projectID)}/requests` : "/api/requests";
  await postJSON(path, { text });
}

export async function createChangeRequest(text: string, projectID?: string): Promise<void> {
  const path = projectID ? `/api/projects/${encodeURIComponent(projectID)}/change-requests` : "/api/change-requests";
  await postJSON(path, { text });
}

export async function saveEnvBinding(key: string, value: string, scope = "project", environmentID = ""): Promise<void> {
  await postJSON("/api/env/bindings", { key, value, scope, environment_id: environmentID });
}

async function postJSON(path: string, body: unknown): Promise<void> {
  const response = await fetch(path, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json"
    },
    body: JSON.stringify(body)
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `${response.status} ${response.statusText}`);
  }
}

function emptyWorkStatus(): WorkStatus {
  return {
    worker_runs: [],
    planning: {
      runs: [],
      artifacts: [],
      queue: []
    }
  };
}
