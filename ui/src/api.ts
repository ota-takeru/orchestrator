import type {
  ChangeRequest,
  DashboardData,
  DashboardWireData,
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
  SetupStatus,
  TaskArtifact,
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
    checkBody,
    setupStatus
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
    getJSON<{ violations: InvariantViolation[] }>("/api/check"),
    getJSON<SetupStatus>("/api/setup")
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
    projectViolations: checkBody.violations,
    setupStatus
  };
}

async function loadProjectDashboardData(projectID: string): Promise<DashboardData> {
  const base = `/api/projects/${encodeURIComponent(projectID)}`;
  return normalizeDashboard(await getJSON<DashboardWireData>(`${base}/dashboard`));
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

export async function saveEnvBinding(key: string, value: string, scope = "project", environmentID = "", projectID?: string): Promise<void> {
  const path = projectID ? `/api/projects/${encodeURIComponent(projectID)}/env/bindings` : "/api/env/bindings";
  await postJSON(path, { key, value, scope, environment_id: environmentID });
}

export async function loadTaskArtifacts(taskID: string, projectID?: string): Promise<TaskArtifact[]> {
  const path = projectID
    ? `/api/projects/${encodeURIComponent(projectID)}/tasks/${encodeURIComponent(taskID)}/artifacts`
    : `/api/tasks/${encodeURIComponent(taskID)}/artifacts`;
  const body = await getJSON<{ artifacts: TaskArtifact[] }>(path);
  return body.artifacts;
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

function normalizeDashboard(data: DashboardWireData): DashboardData {
  return {
    snapshot: data.snapshot,
    tasks: data.tasks ?? [],
    featureRequests: data.feature_requests ?? [],
    queueItems: data.queue_items ?? [],
    workStatus: data.work_status ?? emptyWorkStatus(),
    planningStatus: data.planning_status ?? { runs: [], artifacts: [], queue: [] },
    changeRequests: data.change_requests ?? [],
    dependencyRisks: data.dependency_risks ?? [],
    decisions: data.decisions ?? [],
    baselineIssues: data.baseline_issues ?? [],
    trustedArtifacts: data.trusted_artifacts ?? [],
    pathMappings: data.path_mappings ?? [],
    toolchainSetupCards: data.toolchain_setup_cards ?? [],
    mergeStatus: data.merge_status ?? { queue: [], ready: true },
    projectViolations: data.project_violations ?? [],
    setupStatus: data.setup_status
  };
}
