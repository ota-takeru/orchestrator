import type {
  ChangeRequest,
  DashboardData,
  DashboardWireData,
  Decision,
  DependencyRisk,
  FeatureRequest,
  ArtifactRecord,
  HumanInboxSnapshot,
  InvariantViolation,
  MemoryRecord,
  MergeGateStatus,
  PathMapping,
  PlanningStatus,
  RegisteredProject,
  SetupActionResult,
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
  const body = await getJSON<{ projects?: RegisteredProject[] | null }>("/api/projects");
  return body.projects ?? [];
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
    artifactsBody,
    artifactBody,
    pathMappingBody,
    setupBody,
    mergeStatus,
    checkBody,
    setupStatus
  ] = await Promise.all([
    getJSON<HumanInboxSnapshot>("/api/ui/snapshot?limit=12"),
    getJSON<{ tasks?: TaskRecord[] | null }>("/api/tasks"),
    getJSON<{ requests?: FeatureRequest[] | null }>("/api/requests"),
    getJSON<{ items?: WorkQueueItem[] | null }>("/api/queue"),
    getJSON<WorkStatus>("/api/work/status"),
    getJSON<PlanningStatus>("/api/planning/status"),
    getJSON<{ change_requests?: ChangeRequest[] | null }>("/api/change-requests"),
    getJSON<{ risks?: DependencyRisk[] | null }>("/api/dependency-risks"),
    getJSON<{ decisions?: Decision[] | null }>("/api/decisions?status=open"),
    getJSON<{ memories?: MemoryRecord[] | null }>("/api/memory?type=baseline_issue"),
    getJSON<{ artifacts?: ArtifactRecord[] | null }>("/api/artifacts"),
    getJSON<{ artifacts?: TrustedArtifact[] | null }>("/api/artifacts/trusted"),
    getJSON<{ mappings?: PathMapping[] | null }>("/api/platform/path-mappings"),
    getJSON<{ cards?: ToolchainSetupCard[] | null }>("/api/platform/toolchain-setup"),
    getJSON<MergeGateStatus>("/api/merge/status"),
    getJSON<{ violations?: InvariantViolation[] | null }>("/api/check"),
    getJSON<SetupStatus>("/api/setup")
  ]);

  return normalizeDashboard({
    snapshot,
    tasks: taskBody.tasks ?? [],
    feature_requests: requestBody.requests ?? [],
    queue_items: queueBody.items ?? [],
    work_status: workStatus,
    planning_status: planningStatus,
    change_requests: changeRequestBody.change_requests ?? [],
    dependency_risks: dependencyRiskBody.risks ?? [],
    decisions: decisionBody.decisions ?? [],
    baseline_issues: memoryBody.memories ?? [],
    artifacts: artifactsBody.artifacts ?? [],
    trusted_artifacts: artifactBody.artifacts ?? [],
    path_mappings: pathMappingBody.mappings ?? [],
    toolchain_setup_cards: setupBody.cards ?? [],
    merge_status: mergeStatus,
    project_violations: checkBody.violations ?? [],
    setup_status: setupStatus
  });
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
      Accept: "application/json",
      ...localAuthHeaders()
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

export async function runChangeRequestAction(id: string, action: "analyze" | "approve", projectID?: string): Promise<void> {
  const base = projectID ? `/api/projects/${encodeURIComponent(projectID)}/change-requests` : "/api/change-requests";
  await postJSON(`${base}/${encodeURIComponent(id)}/${action}`, action === "approve" ? { option: "approve" } : {});
}

export async function startWork(projectID?: string, adapter = "fake"): Promise<void> {
  const path = projectID ? `/api/projects/${encodeURIComponent(projectID)}/work/start` : "/api/work/start";
  await postJSON(path, {
    mode: "sequential",
    adapter,
    planning_concurrency: 3,
    implementation_concurrency: 1
  });
}

export async function approveArtifact(artifactID: string, version: number, projectID?: string): Promise<void> {
  const path = projectID
    ? `/api/projects/${encodeURIComponent(projectID)}/artifacts/${encodeURIComponent(artifactID)}/approve`
    : `/api/artifacts/${encodeURIComponent(artifactID)}/approve`;
  await postJSON(path, { version, status: "approved", notes: "Approved from DevOS UI" });
}

export async function materializeTasks(projectID?: string): Promise<void> {
  const path = projectID ? `/api/projects/${encodeURIComponent(projectID)}/tasks/materialize` : "/api/tasks/materialize";
  await postJSON(path, {});
}

export async function saveEnvBinding(key: string, value: string, scope = "project", environmentID = "", projectID?: string): Promise<void> {
  const path = projectID ? `/api/projects/${encodeURIComponent(projectID)}/env/bindings` : "/api/env/bindings";
  await postJSON(path, { key, value, scope, environment_id: environmentID });
}

export async function requestDependencyApproval(input: {
  name: string;
  package_manager: string;
  dependency_type: string;
  reason: string;
  risk: string;
  alternatives?: string;
  files_affected?: string;
}, projectID?: string): Promise<void> {
  const path = projectID ? `/api/projects/${encodeURIComponent(projectID)}/dependency-approvals` : "/api/dependency-approvals";
  await postJSON(path, input);
}

export async function runSetupAction(actionID: string, projectID?: string): Promise<SetupActionResult> {
  const path = projectID
    ? `/api/projects/${encodeURIComponent(projectID)}/setup/actions/${encodeURIComponent(actionID)}`
    : `/api/setup/actions/${encodeURIComponent(actionID)}`;
  return postJSON<SetupActionResult>(path, {});
}

export async function loadTaskArtifacts(taskID: string, projectID?: string): Promise<TaskArtifact[]> {
  const path = projectID
    ? `/api/projects/${encodeURIComponent(projectID)}/tasks/${encodeURIComponent(taskID)}/artifacts`
    : `/api/tasks/${encodeURIComponent(taskID)}/artifacts`;
  const body = await getJSON<{ artifacts: TaskArtifact[] }>(path);
  return body.artifacts;
}

export async function runTaskAction(taskID: string, action: "verify" | "review-approve" | "review-reject" | "merge-approve", projectID?: string): Promise<void> {
  const taskPath = projectID ? `/api/projects/${encodeURIComponent(projectID)}/tasks/${encodeURIComponent(taskID)}` : `/api/tasks/${encodeURIComponent(taskID)}`;
  const suffix =
    action === "verify"
      ? "/verify"
      : action === "review-approve"
        ? "/review/approve"
        : action === "review-reject"
          ? "/review/reject"
          : "/merge/approve";
  await postJSON(`${taskPath}${suffix}`, { notes: "Submitted from DevOS UI" });
}

async function postJSON<T = void>(path: string, body: unknown): Promise<T> {
  const response = await fetch(path, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json",
      ...localAuthHeaders()
    },
    body: JSON.stringify(body)
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `${response.status} ${response.statusText}`);
  }
  if (response.status === 204) {
    return undefined as T;
  }
  return (await response.json()) as T;
}

function localAuthHeaders(): Record<string, string> {
  const headers: Record<string, string> = {};
  const token = globalThis.localStorage?.getItem("devos.localToken")?.trim();
  if (token) {
    headers["X-DevOS-Token"] = token;
    headers["X-DevOS-Nonce"] = randomNonce();
  }
  return headers;
}

function randomNonce() {
  const array = new Uint32Array(2);
  globalThis.crypto?.getRandomValues(array);
  return `${Date.now().toString(36)}-${array[0].toString(36)}${array[1].toString(36)}`;
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

function normalizeSnapshot(snapshot: HumanInboxSnapshot): HumanInboxSnapshot {
  return {
    ...snapshot,
    open_inbox_items: snapshot.open_inbox_items ?? [],
    recommended_next_commands: snapshot.recommended_next_commands ?? []
  };
}

function normalizeWorkStatus(status?: WorkStatus | null): WorkStatus {
  return {
    worker_runs: status?.worker_runs ?? [],
    planning: {
      runs: status?.planning?.runs ?? [],
      artifacts: status?.planning?.artifacts ?? [],
      queue: status?.planning?.queue ?? []
    }
  };
}

function normalizePlanningStatus(status?: PlanningStatus | null): PlanningStatus {
  return {
    runs: status?.runs ?? [],
    artifacts: status?.artifacts ?? [],
    queue: status?.queue ?? []
  };
}

function normalizeMergeStatus(status?: MergeGateStatus | null): MergeGateStatus {
  return {
    queue: status?.queue ?? [],
    blockers: status?.blockers ?? [],
    blocking_inbox_items: status?.blocking_inbox_items ?? [],
    ready: status?.ready ?? false
  };
}

function normalizeSetupStatus(status?: SetupStatus | null): SetupStatus | undefined {
  if (!status) return undefined;
  return {
    ...status,
    git_dirty_files: status.git_dirty_files ?? [],
    protected_paths: status.protected_paths ?? [],
    environment_bindings: status.environment_bindings ?? [],
    toolchain_setup_cards: status.toolchain_setup_cards ?? [],
    actions: status.actions ?? [],
    blockers: status.blockers ?? []
  };
}

function normalizeDashboard(data: DashboardWireData): DashboardData {
  return {
    snapshot: normalizeSnapshot(data.snapshot),
    tasks: data.tasks ?? [],
    featureRequests: data.feature_requests ?? [],
    queueItems: data.queue_items ?? [],
    workStatus: normalizeWorkStatus(data.work_status ?? emptyWorkStatus()),
    planningStatus: normalizePlanningStatus(data.planning_status),
    changeRequests: data.change_requests ?? [],
    dependencyRisks: data.dependency_risks ?? [],
    decisions: data.decisions ?? [],
    baselineIssues: data.baseline_issues ?? [],
    artifacts: data.artifacts ?? [],
    trustedArtifacts: data.trusted_artifacts ?? [],
    pathMappings: data.path_mappings ?? [],
    toolchainSetupCards: data.toolchain_setup_cards ?? [],
    mergeStatus: normalizeMergeStatus(data.merge_status ?? { queue: [], ready: true }),
    projectViolations: data.project_violations ?? [],
    setupStatus: normalizeSetupStatus(data.setup_status)
  };
}
