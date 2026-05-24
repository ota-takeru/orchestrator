import { AlertTriangle, Check, FileCheck2, GitMerge, Inbox, ListChecks, RefreshCcw, Route, ServerCog, ShieldAlert, Wrench } from "lucide-react";
import type { ReactNode } from "react";
import { useEffect, useMemo, useState } from "react";
import { approveInboxItem, createChangeRequest, createFeatureRequest, loadDashboardData, loadProjects, saveEnvBinding } from "./api";
import type { DashboardData, Decision, InboxItem, MemoryRecord, RegisteredProject, SnapshotCounts, WorkQueueItem } from "./types";

const countRows: Array<{
  key: keyof SnapshotCounts;
  label: string;
  tone: "neutral" | "attention" | "blocked" | "ready";
}> = [
  { key: "open_inbox_items", label: "Inbox", tone: "attention" },
  { key: "waiting_for_human_tasks", label: "Waiting", tone: "attention" },
  { key: "blocked_tasks", label: "Blocked", tone: "blocked" },
  { key: "queued_requests", label: "Requests", tone: "neutral" },
  { key: "running_tasks", label: "Running", tone: "ready" },
  { key: "open_merge_queue", label: "Merge", tone: "ready" },
  { key: "baseline_issues", label: "Baseline", tone: "neutral" },
  { key: "running_workers", label: "Workers", tone: "ready" }
];

function App() {
  const [projects, setProjects] = useState<RegisteredProject[]>([]);
  const [selectedProjectID, setSelectedProjectID] = useState("");
  const [data, setData] = useState<DashboardData | null>(null);
  const [error, setError] = useState<string>("");
  const [loading, setLoading] = useState(true);
  const [approving, setApproving] = useState<string>("");
  const [featureText, setFeatureText] = useState("");
  const [changeText, setChangeText] = useState("");
  const [envKey, setEnvKey] = useState("");
  const [envValue, setEnvValue] = useState("");

  const selectedProject = useMemo(() => projects.find((project) => project.id === selectedProjectID), [projects, selectedProjectID]);

  const loadSelectedDashboard = async (projectID: string) => {
    setLoading(true);
    setError("");
    try {
      setData(await loadDashboardData(projectID || undefined));
    } catch (err) {
      setError(err instanceof Error ? err.message : "Dashboard load failed");
    } finally {
      setLoading(false);
    }
  };

  const refresh = async () => {
    await loadSelectedDashboard(selectedProjectID);
  };

  useEffect(() => {
    const loadInitial = async () => {
      setLoading(true);
      setError("");
      try {
        const registeredProjects = await loadProjects();
        setProjects(registeredProjects);
        const initialProjectID = registeredProjects[0]?.id ?? "";
        setSelectedProjectID(initialProjectID);
        setData(await loadDashboardData(initialProjectID || undefined));
      } catch (err) {
        try {
          setData(await loadDashboardData());
        } catch {
          setError(err instanceof Error ? err.message : "Dashboard load failed");
        }
      } finally {
        setLoading(false);
      }
    };
    void loadInitial();
  }, []);

  const nextCommand = useMemo(() => data?.snapshot.recommended_next_commands?.[0] ?? "devos request --json <TEXT>", [data]);

  const selectProject = (projectID: string) => {
    setSelectedProjectID(projectID);
    void loadSelectedDashboard(projectID);
  };

  const approve = async (item: InboxItem) => {
    setApproving(item.id);
    setError("");
    try {
      const option = item.source_type === "decision" ? firstDecisionOption(data?.decisions ?? [], item.source_id) : undefined;
      await approveInboxItem(item.id, "Approved from DevOS UI", option, selectedProjectID || undefined);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Approval failed");
    } finally {
      setApproving("");
    }
  };

  const submitFeatureRequest = async () => {
    if (!featureText.trim()) return;
    setError("");
    try {
      await createFeatureRequest(featureText, selectedProjectID || undefined);
      setFeatureText("");
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Feature request failed");
    }
  };

  const submitChangeRequest = async () => {
    if (!changeText.trim()) return;
    setError("");
    try {
      await createChangeRequest(changeText, selectedProjectID || undefined);
      setChangeText("");
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Change request failed");
    }
  };

  const submitEnvBinding = async () => {
    if (selectedProjectID) return;
    if (!envKey.trim() || !envValue) return;
    setError("");
    try {
      await saveEnvBinding(envKey, envValue);
      setEnvKey("");
      setEnvValue("");
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Environment input failed");
    }
  };

  return (
    <main className="min-h-screen bg-zinc-50 text-zinc-950">
      <header className="border-b border-zinc-200 bg-white">
        <div className="mx-auto flex max-w-7xl items-center justify-between gap-4 px-5 py-4">
          <div>
            <p className="text-xs font-medium uppercase tracking-wide text-zinc-500">DevOS</p>
            <h1 className="text-2xl font-semibold">Human Inbox</h1>
          </div>
          <button className="icon-button" onClick={refresh} disabled={loading} title="Refresh dashboard" aria-label="Refresh dashboard">
            <RefreshCcw size={18} />
          </button>
        </div>
      </header>

      <div className="mx-auto grid max-w-7xl gap-5 px-5 py-5 lg:grid-cols-[260px_1fr_340px]">
        <ProjectListSidebar projects={projects} selectedProjectID={selectedProjectID} onSelect={selectProject} />
        <section className="space-y-5">
          {error ? <ErrorBanner message={error} /> : null}
          {data ? (
            <SelectedProjectDashboard
              data={data}
              selectedProject={selectedProject}
              approving={approving}
              featureText={featureText}
              setFeatureText={setFeatureText}
              onSubmitFeature={submitFeatureRequest}
              onApprove={approve}
            />
          ) : (
            <LoadingPanel />
          )}
        </section>

        <aside className="space-y-5">
          <CommandPanel command={nextCommand} />
          <EnvironmentInputPanel
            envKey={envKey}
            envValue={envValue}
            setEnvKey={setEnvKey}
            setEnvValue={setEnvValue}
            onSubmit={submitEnvBinding}
            disabled={Boolean(selectedProjectID)}
          />
          <ChangeRequestPanel requests={data?.changeRequests ?? []} text={changeText} setText={setChangeText} onSubmit={submitChangeRequest} />
          <DependencyRiskPanel risks={data?.dependencyRisks ?? []} />
          <ToolchainSetupPanel cards={data?.toolchainSetupCards ?? []} />
          <MergeGatePanel status={data?.mergeStatus} />
          <ProjectCheckPanel violations={data?.projectViolations ?? []} />
          <TrustedArtifactsPanel artifacts={data?.trustedArtifacts ?? []} />
          <PathMappingsPanel mappings={data?.pathMappings ?? []} />
          <DecisionPanel decisions={data?.decisions ?? []} />
          <BaselinePanel issues={data?.baselineIssues ?? []} />
        </aside>
      </div>
    </main>
  );
}

function ProjectListSidebar({
  projects,
  selectedProjectID,
  onSelect
}: {
  projects: RegisteredProject[];
  selectedProjectID: string;
  onSelect: (projectID: string) => void;
}) {
  return (
    <aside className="project-sidebar">
      <div className="panel compact">
        <div className="panel-heading">
          <h2>Projects</h2>
          <ServerCog size={18} className="text-zinc-500" />
        </div>
        <ProjectSwitcher projects={projects} selectedProjectID={selectedProjectID} onSelect={onSelect} />
        <StackEmpty empty={projects.length === 0} label="No registered projects">
          <div className="project-list">
            {projects.map((project) => (
              <button
                className={`project-row ${project.id === selectedProjectID ? "selected" : ""}`}
                key={project.id}
                onClick={() => onSelect(project.id)}
                type="button"
              >
                <span>{project.display_name}</span>
                <small>
                  {project.authority_runtime}
                  {project.wsl_distro ? ` / ${project.wsl_distro}` : ""} / {project.status}
                </small>
              </button>
            ))}
          </div>
        </StackEmpty>
      </div>
    </aside>
  );
}

function ProjectSwitcher({
  projects,
  selectedProjectID,
  onSelect
}: {
  projects: RegisteredProject[];
  selectedProjectID: string;
  onSelect: (projectID: string) => void;
}) {
  return (
    <select className="project-switcher" value={selectedProjectID} onChange={(event) => onSelect(event.target.value)} disabled={projects.length === 0}>
      {projects.length === 0 ? <option value="">Single project</option> : null}
      {projects.map((project) => (
        <option key={project.id} value={project.id}>
          {project.display_name}
        </option>
      ))}
    </select>
  );
}

function SelectedProjectDashboard({
  data,
  selectedProject,
  approving,
  featureText,
  setFeatureText,
  onSubmitFeature,
  onApprove
}: {
  data: DashboardData;
  selectedProject?: RegisteredProject;
  approving: string;
  featureText: string;
  setFeatureText: (value: string) => void;
  onSubmitFeature: () => void;
  onApprove: (item: InboxItem) => void;
}) {
  return (
    <>
      {selectedProject ? <ProjectStatusPanel project={selectedProject} /> : null}
      <Summary counts={data.snapshot.counts} generatedAt={data.snapshot.generated_at} lastMergeAt={data.snapshot.last_successful_merge_at} />
      <InboxPanel items={data.snapshot.open_inbox_items} decisions={data.decisions} approving={approving} onApprove={onApprove} />
      <RequestQueuePanel requests={data.featureRequests} queueItems={data.queueItems} featureText={featureText} setFeatureText={setFeatureText} onSubmitFeature={onSubmitFeature} />
      <WorkPlanningPanel data={data} />
      <TaskPanel tasks={data.tasks} />
    </>
  );
}

function ProjectStatusPanel({ project }: { project: RegisteredProject }) {
  return (
    <section className="project-status">
      <div>
        <h2>{project.display_name}</h2>
        <p>{project.windows_display_root || project.project_root}</p>
      </div>
      <div className="badge-row">
        <span className="runtime-badge">{project.authority_runtime}</span>
        {project.wsl_distro ? <span className="runtime-badge muted">{project.wsl_distro}</span> : null}
        <span className={`status-badge status-${project.status}`}>{project.status}</span>
      </div>
    </section>
  );
}

function RequestQueuePanel({
  requests,
  queueItems,
  featureText,
  setFeatureText,
  onSubmitFeature
}: {
  requests: DashboardData["featureRequests"];
  queueItems: WorkQueueItem[];
  featureText: string;
  setFeatureText: (value: string) => void;
  onSubmitFeature: () => void;
}) {
  return (
    <section className="panel">
      <div className="panel-heading">
        <div>
          <h2>Request Queue</h2>
          <p>{requests.length} feature requests</p>
        </div>
        <ListChecks size={20} className="text-zinc-500" />
      </div>
      <div className="form-row">
        <input value={featureText} onChange={(event) => setFeatureText(event.target.value)} placeholder="Feature request" />
        <button onClick={onSubmitFeature} disabled={!featureText.trim()}>
          Add
        </button>
      </div>
      <div className="split-grid">
        <StackEmpty empty={requests.length === 0} label="No feature requests">
          {requests.slice(0, 6).map((request) => (
            <div className="stack-row" key={request.id}>
              <span>{request.title}</span>
              <small>
                {request.status} / {request.priority}
              </small>
            </div>
          ))}
        </StackEmpty>
        <StackEmpty empty={queueItems.length === 0} label="No queued work">
          {queueItems.slice(0, 6).map((item) => (
            <div className="stack-row" key={item.id}>
              <span>{item.item_type}</span>
              <small>
                {item.lane} / {item.status} / attempt {item.attempt_no}
              </small>
            </div>
          ))}
        </StackEmpty>
      </div>
    </section>
  );
}

function WorkPlanningPanel({ data }: { data: DashboardData }) {
  return (
    <section className="panel">
      <div className="panel-heading">
        <div>
          <h2>Work And Planning</h2>
          <p>{data.workStatus.worker_runs.length} worker runs</p>
        </div>
        <Wrench size={20} className="text-zinc-500" />
      </div>
      <div className="split-grid">
        <StackEmpty empty={data.workStatus.worker_runs.length === 0} label="No worker runs">
          {data.workStatus.worker_runs.slice(0, 5).map((run) => (
            <div className="stack-row" key={run.id}>
              <span>{run.lane}</span>
              <small>
                {run.mode} / {run.status}
              </small>
            </div>
          ))}
        </StackEmpty>
        <StackEmpty empty={data.planningStatus.artifacts.length === 0} label="No planning artifacts">
          {data.planningStatus.artifacts.slice(0, 5).map((artifact) => (
            <div className="stack-row" key={artifact.id}>
              <span>{artifact.artifact_type}</span>
              <small>{artifact.status}</small>
              <small>{artifact.path}</small>
            </div>
          ))}
        </StackEmpty>
      </div>
    </section>
  );
}

function TaskPanel({ tasks }: { tasks: DashboardData["tasks"] }) {
  return (
    <section className="panel">
      <div className="panel-heading">
        <div>
          <h2>Tasks</h2>
          <p>{tasks.length} canonical tasks</p>
        </div>
        <FileCheck2 size={20} className="text-zinc-500" />
      </div>
      <StackEmpty empty={tasks.length === 0} label="No tasks">
        {tasks.slice(0, 8).map((task) => (
          <div className="stack-row" key={task.id}>
            <span>{task.title}</span>
            <small>
              {task.id} / {task.status}
            </small>
          </div>
        ))}
      </StackEmpty>
    </section>
  );
}

function EnvironmentInputPanel({
  envKey,
  envValue,
  setEnvKey,
  setEnvValue,
  onSubmit,
  disabled = false
}: {
  envKey: string;
  envValue: string;
  setEnvKey: (value: string) => void;
  setEnvValue: (value: string) => void;
  onSubmit: () => void;
  disabled?: boolean;
}) {
  return (
    <section className="panel compact">
      <div className="panel-heading">
        <h2>Environment Input</h2>
        <ServerCog size={18} className="text-zinc-500" />
      </div>
      <div className="compact-form">
        <input value={envKey} onChange={(event) => setEnvKey(event.target.value)} placeholder="KEY" disabled={disabled} />
        <input value={envValue} onChange={(event) => setEnvValue(event.target.value)} placeholder="Value" type="password" disabled={disabled} />
        <button onClick={onSubmit} disabled={disabled || !envKey.trim() || !envValue}>
          Save
        </button>
      </div>
    </section>
  );
}

function ChangeRequestPanel({
  requests,
  text,
  setText,
  onSubmit
}: {
  requests: DashboardData["changeRequests"];
  text: string;
  setText: (value: string) => void;
  onSubmit: () => void;
}) {
  return (
    <section className="panel compact">
      <div className="panel-heading">
        <h2>Change Requests</h2>
        <RefreshCcw size={18} className="text-zinc-500" />
      </div>
      <div className="compact-form">
        <input value={text} onChange={(event) => setText(event.target.value)} placeholder="Change request" />
        <button onClick={onSubmit} disabled={!text.trim()}>
          Add
        </button>
      </div>
      <StackEmpty empty={requests.length === 0} label="No change requests">
        {requests.slice(0, 5).map((request) => (
          <div className="stack-row" key={request.id}>
            <span>{request.body}</span>
            <small>{request.status}</small>
          </div>
        ))}
      </StackEmpty>
    </section>
  );
}

function DependencyRiskPanel({ risks }: { risks: DashboardData["dependencyRisks"] }) {
  return (
    <section className="panel compact">
      <div className="panel-heading">
        <h2>Dependency Risk</h2>
        <ShieldAlert size={18} className="text-zinc-500" />
      </div>
      <StackEmpty empty={risks.length === 0} label="No dependency risks">
        {risks.slice(0, 5).map((risk) => (
          <div className="stack-row" key={risk.id}>
            <span>{risk.name}</span>
            <small>
              {risk.package_manager} / {risk.dependency_type} / {risk.risk}
            </small>
          </div>
        ))}
      </StackEmpty>
    </section>
  );
}

function Summary({ counts, generatedAt, lastMergeAt }: { counts: SnapshotCounts; generatedAt: string; lastMergeAt?: string }) {
  return (
    <section className="panel">
      <div className="panel-heading">
        <div>
          <h2>Autonomy Status</h2>
          <p>{formatDate(generatedAt)}</p>
        </div>
        <span className="merge-status">
          <GitMerge size={16} />
          {lastMergeAt ? formatDate(lastMergeAt) : "No merge yet"}
        </span>
      </div>
      <div className="metric-grid">
        {countRows.map((row) => (
          <div className={`metric metric-${row.tone}`} key={row.key}>
            <span>{row.label}</span>
            <strong>{counts[row.key]}</strong>
          </div>
        ))}
      </div>
    </section>
  );
}

function InboxPanel({
  items,
  decisions,
  approving,
  onApprove
}: {
  items: InboxItem[];
  decisions: Decision[];
  approving: string;
  onApprove: (item: InboxItem) => void;
}) {
  const decisionOptions = new Map(decisions.map((decision) => [decision.id, decision.options?.[0]?.id ?? ""]));
  return (
    <section className="panel">
      <div className="panel-heading">
        <div>
          <h2>Needs Attention</h2>
          <p>{items.length} open items</p>
        </div>
        <Inbox size={20} className="text-zinc-500" />
      </div>
      <div className="table-shell">
        <table>
          <thead>
            <tr>
              <th>Priority</th>
              <th>Type</th>
              <th>Title</th>
              <th>Source</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {items.length === 0 ? (
              <tr>
                <td colSpan={5} className="empty-cell">
                  No open inbox items
                </td>
              </tr>
            ) : (
              items.map((item) => (
                <tr key={item.id}>
                  <td>
                    <span className="priority">{item.priority}</span>
                  </td>
                  <td>{item.item_type}</td>
                  <td>
                    <div className="item-title">{item.title}</div>
                    <div className="item-body">{item.body}</div>
                  </td>
                  <td>{item.source_type}</td>
                  <td className="row-action">
                    {item.source_type === "human_approval" || (item.source_type === "decision" && decisionOptions.get(item.source_id ?? "")) ? (
                      <button className="icon-button small" onClick={() => onApprove(item)} disabled={approving === item.id} title="Approve" aria-label={`Approve ${item.id}`}>
                        <Check size={16} />
                      </button>
                    ) : null}
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function CommandPanel({ command }: { command: string }) {
  return (
    <section className="panel compact">
      <div className="panel-heading">
        <h2>Next Command</h2>
        <ListChecks size={18} className="text-zinc-500" />
      </div>
      <code className="command">{command}</code>
    </section>
  );
}

function DecisionPanel({ decisions }: { decisions: DashboardData["decisions"] }) {
  return (
    <section className="panel compact">
      <div className="panel-heading">
        <h2>Open Decisions</h2>
        <ShieldAlert size={18} className="text-zinc-500" />
      </div>
      <StackEmpty empty={decisions.length === 0} label="No open decisions">
        {decisions.map((decision) => (
          <div className="stack-row" key={decision.id}>
            <span>{decision.title}</span>
            <small>{decision.options?.[0]?.label ?? decision.status}</small>
          </div>
        ))}
      </StackEmpty>
    </section>
  );
}

function ToolchainSetupPanel({ cards }: { cards: DashboardData["toolchainSetupCards"] }) {
  return (
    <section className="panel compact">
      <div className="panel-heading">
        <h2>Setup Cards</h2>
        <Wrench size={18} className="text-zinc-500" />
      </div>
      <StackEmpty empty={cards.length === 0} label="No setup blockers">
        {cards.map((card) => (
          <div className="stack-row" key={card.inbox_id}>
            <span>
              {card.toolchain_key} on {card.environment_id}
            </span>
            <small>{card.required_for_merge ? "merge blocker" : card.required_for}</small>
            <small>{card.instructions[0] ?? card.message}</small>
            <code className="inline-command">{card.rerun_command}</code>
          </div>
        ))}
      </StackEmpty>
    </section>
  );
}

function MergeGatePanel({ status }: { status?: DashboardData["mergeStatus"] }) {
  const blockers = status?.blockers ?? [];
  const inboxItems = status?.blocking_inbox_items ?? [];
  return (
    <section className="panel compact">
      <div className="panel-heading">
        <h2>Merge Gate</h2>
        <GitMerge size={18} className="text-zinc-500" />
      </div>
      <div className="stack">
        <div className="stack-row">
          <span>{status?.ready ? "Ready" : "Blocked"}</span>
          <small>{status?.queue.length ?? 0} queued tasks</small>
        </div>
        {blockers.map((blocker) => (
          <div className="stack-row" key={blocker}>
            <span>{blocker}</span>
            <small>gate blocker</small>
          </div>
        ))}
        {inboxItems.map((item) => (
          <div className="stack-row" key={item.id}>
            <span>{item.title}</span>
            <small>{item.source_type}</small>
          </div>
        ))}
      </div>
    </section>
  );
}

function ProjectCheckPanel({ violations }: { violations: DashboardData["projectViolations"] }) {
  return (
    <section className="panel compact">
      <div className="panel-heading">
        <h2>Project Check</h2>
        <AlertTriangle size={18} className="text-zinc-500" />
      </div>
      <StackEmpty empty={violations.length === 0} label="No invariant violations">
        {violations.map((violation) => (
          <div className="stack-row" key={`${violation.scope}:${violation.id}:${violation.code}`}>
            <span>{violation.code}</span>
            <small>
              {violation.scope} {violation.id}
            </small>
            <small>{violation.message}</small>
          </div>
        ))}
      </StackEmpty>
    </section>
  );
}

function TrustedArtifactsPanel({ artifacts }: { artifacts: DashboardData["trustedArtifacts"] }) {
  return (
    <section className="panel compact">
      <div className="panel-heading">
        <h2>Trusted Artifacts</h2>
        <FileCheck2 size={18} className="text-zinc-500" />
      </div>
      <StackEmpty empty={artifacts.length === 0} label="No trusted artifacts">
        {artifacts.map((artifact) => (
          <div className="stack-row" key={artifact.version_id}>
            <span>
              {artifact.artifact_type} v{artifact.version}
            </span>
            <small>{artifact.approval_notes ? `${artifact.status} + notes` : artifact.status}</small>
            <small>{artifact.path}</small>
          </div>
        ))}
      </StackEmpty>
    </section>
  );
}

function PathMappingsPanel({ mappings }: { mappings: DashboardData["pathMappings"] }) {
  return (
    <section className="panel compact">
      <div className="panel-heading">
        <h2>Path Mappings</h2>
        <Route size={18} className="text-zinc-500" />
      </div>
      <StackEmpty empty={mappings.length === 0} label="No path mappings">
        {mappings.map((mapping) => (
          <div className="stack-row" key={mapping.id}>
            <span>
              {mapping.from_environment_id} to {mapping.to_environment_id}
            </span>
            <small>{mapping.mapping_mode}</small>
            <small>
              {mapping.from_root} to {mapping.to_root}
            </small>
          </div>
        ))}
      </StackEmpty>
    </section>
  );
}

function BaselinePanel({ issues }: { issues: MemoryRecord[] }) {
  return (
    <section className="panel compact">
      <div className="panel-heading">
        <h2>Baseline Issues</h2>
        <ServerCog size={18} className="text-zinc-500" />
      </div>
      <StackEmpty empty={issues.length === 0} label="No baseline issues">
        {issues.map((issue) => (
          <div className="stack-row" key={issue.id}>
            <span>{issue.key}</span>
            <small>{issue.source_id || issue.source_type}</small>
          </div>
        ))}
      </StackEmpty>
    </section>
  );
}

function StackEmpty({ empty, label, children }: { empty: boolean; label: string; children: ReactNode }) {
  if (empty) {
    return <div className="empty-stack">{label}</div>;
  }
  return <div className="stack">{children}</div>;
}

function ErrorBanner({ message }: { message: string }) {
  return (
    <div className="error-banner">
      <AlertTriangle size={18} />
      <span>{message}</span>
    </div>
  );
}

function LoadingPanel() {
  return (
    <section className="panel">
      <div className="loading-bar" />
    </section>
  );
}

function formatDate(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat("ja-JP", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  }).format(date);
}

function firstDecisionOption(decisions: Decision[], decisionID?: string) {
  if (!decisionID) {
    return undefined;
  }
  return decisions.find((decision) => decision.id === decisionID)?.options?.[0]?.id;
}

export default App;
