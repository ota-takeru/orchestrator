import { AlertTriangle, Check, FileCheck2, FolderOpen, GitMerge, Inbox, ListChecks, MoreHorizontal, Pencil, Plus, RefreshCcw, Route, ServerCog, ShieldAlert, Wrench } from "lucide-react";
import type { ReactNode } from "react";
import { useEffect, useMemo, useState } from "react";
import { approveArtifact, approveInboxItem, createChangeRequest, createFeatureRequest, createProject, loadDashboardData, loadProjects, loadTaskArtifacts, materializeTasks, pickProjectPath, requestDependencyApproval, reviseArtifact, reviseArtifactWithCodex, runChangeRequestAction, runSetupAction, runTaskAction, saveEnvBinding, startWork, suggestProjectPath } from "./api";
import type { ArtifactRecord, CurrentProject, DashboardData, Decision, InboxItem, MemoryRecord, ProjectPathSuggestion, ProjectRuntimeOption, RegisteredProject, SnapshotCounts, TaskArtifact, WorkQueueItem } from "./types";

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

const defaultNewProjectName = "New Project";

function App() {
  const [projects, setProjects] = useState<RegisteredProject[]>([]);
  const [currentProject, setCurrentProject] = useState<CurrentProject | undefined>();
  const [runtimeOptions, setRuntimeOptions] = useState<ProjectRuntimeOption[]>([]);
  const [defaultPathSuggestion, setDefaultPathSuggestion] = useState<ProjectPathSuggestion | undefined>();
  const [lastCreatedProject, setLastCreatedProject] = useState<RegisteredProject | undefined>();
  const [selectedProjectID, setSelectedProjectID] = useState("");
  const [data, setData] = useState<DashboardData | null>(null);
  const [error, setError] = useState<string>("");
  const [notice, setNotice] = useState<string>("");
  const [loading, setLoading] = useState(true);
  const [creatingProject, setCreatingProject] = useState(false);
  const [newProjectName, setNewProjectName] = useState(defaultNewProjectName);
  const [newProjectRoot, setNewProjectRoot] = useState("");
  const [newProjectBase, setNewProjectBase] = useState("");
  const [newProjectRootTouched, setNewProjectRootTouched] = useState(false);
  const [newProjectConcept, setNewProjectConcept] = useState("");
  const [newProjectRuntime, setNewProjectRuntime] = useState<"windows" | "wsl">("wsl");
  const [newProjectWslDistro, setNewProjectWslDistro] = useState("");
  const [openingProjectPath, setOpeningProjectPath] = useState(false);
  const [newProjectActioning, setNewProjectActioning] = useState(false);
  const [approving, setApproving] = useState<string>("");
  const [featureText, setFeatureText] = useState("");
  const [changeText, setChangeText] = useState("");
  const [envKey, setEnvKey] = useState("");
  const [envValue, setEnvValue] = useState("");
  const [dependencyName, setDependencyName] = useState("");
  const [dependencyReason, setDependencyReason] = useState("");
  const [dependencyManager, setDependencyManager] = useState("npm");
  const [dependencyType, setDependencyType] = useState("production");
  const [dependencyRisk, setDependencyRisk] = useState("medium");
  const [dependencyAlternatives, setDependencyAlternatives] = useState("");
  const [dependencyFiles, setDependencyFiles] = useState("");
  const [selectedArtifactTaskID, setSelectedArtifactTaskID] = useState("");
  const [taskArtifacts, setTaskArtifacts] = useState<TaskArtifact[]>([]);
  const [taskActioning, setTaskActioning] = useState("");
  const [setupActioning, setSetupActioning] = useState("");
  const [workActioning, setWorkActioning] = useState("");
  const [artifactActioning, setArtifactActioning] = useState("");
  const [changeActioning, setChangeActioning] = useState("");

  const selectedProject = useMemo(() => projects.find((project) => project.id === selectedProjectID), [projects, selectedProjectID]);
  const selectedProjectSummary = selectedProjectID ? (selectedProject ?? (lastCreatedProject?.id === selectedProjectID ? lastCreatedProject : undefined)) : currentProject;

  const loadSelectedDashboard = async (projectID: string) => {
    setLoading(true);
    setError("");
    setNotice("");
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
        const projectData = await loadProjects();
        setProjects(projectData.projects);
        setCurrentProject(projectData.current_project);
        setRuntimeOptions(projectData.runtime_options);
        setDefaultPathSuggestion(projectData.project_path_suggestion);
        const recommended = projectData.runtime_options.find((option) => option.recommended && option.available) ?? projectData.runtime_options.find((option) => option.available);
        if (recommended) {
          setNewProjectRuntime(recommended.authority_runtime);
          setNewProjectWslDistro(recommended.wsl_distro ?? "");
        }
        if (projectData.project_path_suggestion) {
          setNewProjectRoot(projectData.project_path_suggestion.project_root);
          setNewProjectBase(projectData.project_path_suggestion.base_path);
        }
        const initialProjectID = projectData.current_project ? "" : (projectData.projects[0]?.id ?? "");
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

  useEffect(() => {
    if (!creatingProject || newProjectRootTouched) return;
    let active = true;
    const timer = window.setTimeout(() => {
      void suggestProjectPath(newProjectName, newProjectRuntime, newProjectBase || undefined)
        .then((suggestion) => {
          if (!active) return;
          setNewProjectRoot(suggestion.project_root);
          setNewProjectBase(suggestion.base_path);
        })
        .catch(() => undefined);
    }, 120);
    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, [creatingProject, newProjectName, newProjectRuntime, newProjectBase, newProjectRootTouched]);

  const nextCommand = useMemo(() => data?.snapshot.recommended_next_commands?.[0] ?? "devos request --json <TEXT>", [data]);

  const selectProject = (projectID: string) => {
    setCreatingProject(false);
    setNotice("");
    setSelectedProjectID(projectID);
    void loadSelectedDashboard(projectID);
  };

  const openNewProject = () => {
    const recommended = runtimeOptions.find((option) => option.recommended && option.available) ?? runtimeOptions.find((option) => option.available);
    if (recommended) {
      setNewProjectRuntime(recommended.authority_runtime);
      setNewProjectWslDistro(recommended.wsl_distro ?? "");
    }
    if (defaultPathSuggestion) {
      setNewProjectRoot(defaultPathSuggestion.project_root);
      setNewProjectBase(defaultPathSuggestion.base_path);
    }
    if (!newProjectName.trim()) {
      setNewProjectName(defaultNewProjectName);
    }
    setNewProjectRootTouched(false);
    setCreatingProject(true);
    setError("");
    setNotice("");
  };

  const pickProjectRootParent = async () => {
    setOpeningProjectPath(true);
    setError("");
    try {
      const suggestion = await pickProjectPath(newProjectRoot || newProjectBase, newProjectName, newProjectRuntime);
      setNewProjectBase(suggestion.base_path);
      setNewProjectRoot(suggestion.project_root);
      setNewProjectRootTouched(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Path selection failed");
    } finally {
      setOpeningProjectPath(false);
    }
  };

  const submitNewProject = async () => {
    setNewProjectActioning(true);
    setError("");
    setNotice("");
    try {
      const result = await createProject({
        display_name: newProjectName,
        project_root: newProjectRoot,
        concept: newProjectConcept,
        authority_runtime: newProjectRuntime,
        wsl_distro: newProjectWslDistro,
        generate_initial_artifacts: true
      });

      setLastCreatedProject(result.project);
      setProjects((previous) => upsertProject(previous, result.project));
      setSelectedProjectID(result.project.id);
      setData(result.dashboard);
      setCreatingProject(false);
      setNotice(`${result.project.display_name} was created and selected.`);
      setNewProjectName(defaultNewProjectName);
      setNewProjectRoot("");
      setNewProjectBase("");
      setNewProjectRootTouched(false);
      setNewProjectConcept("");

      try {
        const projectData = await loadProjects();
        setProjects(upsertProject(projectData.projects, result.project));
        setCurrentProject(projectData.current_project);
        setRuntimeOptions(projectData.runtime_options);
        setDefaultPathSuggestion(projectData.project_path_suggestion);
        setData(await loadDashboardData(result.project.id));
      } catch (refreshErr) {
        setNotice(
          `${result.project.display_name} was created, but the live dashboard refresh failed: ${
            refreshErr instanceof Error ? refreshErr.message : "Dashboard refresh failed"
          }`
        );
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Project creation failed");
    } finally {
      setNewProjectActioning(false);
    }
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

  const submitChangeRequestAction = async (id: string, action: "analyze" | "approve") => {
    setChangeActioning(`${id}:${action}`);
    setError("");
    try {
      await runChangeRequestAction(id, action, selectedProjectID || undefined);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Change request action failed");
    } finally {
      setChangeActioning("");
    }
  };

  const submitWorkStart = async (adapter: "fake" | "real-codex") => {
    setWorkActioning(adapter);
    setError("");
    setNotice("");
    try {
      const result = await startWork(selectedProjectID || undefined, adapter);
      await refresh();
      setNotice(`${adapter === "fake" ? "Fake" : "Codex"} worker finished with ${result.execution?.length ?? 0} execution item(s).`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Work start failed");
    } finally {
      setWorkActioning("");
    }
  };

  const submitReviewArtifact = async (artifactID: string, version: number, status: "approved" | "approved_with_notes" | "rejected", notes: string) => {
    setArtifactActioning(`${status}:${artifactID}`);
    setError("");
    setNotice("");
    try {
      await approveArtifact(artifactID, version, selectedProjectID || undefined, status, notes);
      await refresh();
      setNotice(status === "rejected" ? "Artifact changes requested." : "Artifact approved.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Artifact review failed");
    } finally {
      setArtifactActioning("");
    }
  };

  const submitReviseArtifact = async (artifactID: string, content: string) => {
    setArtifactActioning(`revise:${artifactID}`);
    setError("");
    setNotice("");
    try {
      await reviseArtifact(artifactID, content, selectedProjectID || undefined);
      await refresh();
      setNotice("Artifact revision saved.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Artifact revision failed");
    } finally {
      setArtifactActioning("");
    }
  };

  const submitCodexReviseArtifact = async (artifactID: string, instruction: string) => {
    setArtifactActioning(`codex-revise:${artifactID}`);
    setError("");
    setNotice("");
    try {
      await reviseArtifactWithCodex(artifactID, instruction, selectedProjectID || undefined);
      await refresh();
      setNotice("Codex artifact revision saved.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Codex artifact revision failed");
    } finally {
      setArtifactActioning("");
    }
  };

  const submitMaterializeTasks = async () => {
    setArtifactActioning("materialize");
    setError("");
    setNotice("");
    try {
      const result = await materializeTasks(selectedProjectID || undefined);
      await refresh();
      setNotice(`${result.tasks?.length ?? 0} task(s) materialized and queued.`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Task materialize failed");
    } finally {
      setArtifactActioning("");
    }
  };

  const submitEnvBinding = async () => {
    if (!envKey.trim() || !envValue) return;
    setError("");
    try {
      await saveEnvBinding(envKey, envValue, "project", "", selectedProjectID || undefined);
      setEnvKey("");
      setEnvValue("");
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Environment input failed");
    }
  };

  const submitDependencyApproval = async () => {
    if (!dependencyName.trim() || !dependencyReason.trim()) return;
    setError("");
    try {
      await requestDependencyApproval(
        {
          name: dependencyName,
          package_manager: dependencyManager,
          dependency_type: dependencyType,
          reason: dependencyReason,
          risk: dependencyRisk,
          alternatives: dependencyAlternatives,
          files_affected: dependencyFiles
        },
        selectedProjectID || undefined
      );
      setDependencyName("");
      setDependencyReason("");
      setDependencyAlternatives("");
      setDependencyFiles("");
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Dependency approval request failed");
    }
  };

  const openTaskArtifacts = async (taskID: string) => {
    setSelectedArtifactTaskID(taskID);
    setError("");
    try {
      setTaskArtifacts(await loadTaskArtifacts(taskID, selectedProjectID || undefined));
    } catch (err) {
      setError(err instanceof Error ? err.message : "Artifact load failed");
    }
  };

  const submitTaskAction = async (taskID: string, action: "verify" | "review-approve" | "review-reject" | "merge-approve") => {
    setTaskActioning(`${taskID}:${action}`);
    setError("");
    try {
      await runTaskAction(taskID, action, selectedProjectID || undefined);
      await refresh();
      if (selectedArtifactTaskID === taskID) {
        setTaskArtifacts(await loadTaskArtifacts(taskID, selectedProjectID || undefined));
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Task action failed");
    } finally {
      setTaskActioning("");
    }
  };

  const submitSetupAction = async (actionID: string) => {
    setSetupActioning(actionID);
    setError("");
    try {
      await runSetupAction(actionID, selectedProjectID || undefined);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Setup action failed");
    } finally {
      setSetupActioning("");
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
        <ProjectListSidebar
          projects={projects}
          currentProject={currentProject}
          selectedProjectID={selectedProjectID}
          onSelect={selectProject}
          onNewProject={openNewProject}
        />
        <section className="space-y-5">
          {error ? <ErrorBanner message={error} /> : null}
          {notice ? <NoticeBanner message={notice} /> : null}
          {creatingProject ? (
            <NewProjectPanel
              name={newProjectName}
              root={newProjectRoot}
              concept={newProjectConcept}
              runtime={newProjectRuntime}
              wslDistro={newProjectWslDistro}
              runtimeOptions={runtimeOptions}
              actioning={newProjectActioning}
              setName={setNewProjectName}
              setRoot={(value) => {
                setNewProjectRoot(value);
                setNewProjectRootTouched(true);
              }}
              setConcept={setNewProjectConcept}
              setRuntime={(value) => {
                setNewProjectRuntime(value);
                setNewProjectRootTouched(false);
              }}
              setWslDistro={setNewProjectWslDistro}
              openingProjectPath={openingProjectPath}
              onBrowse={() => void pickProjectRootParent()}
              onSubmit={submitNewProject}
              onCancel={() => {
                setCreatingProject(false);
              }}
            />
          ) : data ? (
            <SelectedProjectDashboard
              data={data}
              selectedProject={selectedProjectSummary}
              approving={approving}
              featureText={featureText}
              setFeatureText={setFeatureText}
              onSubmitFeature={submitFeatureRequest}
              onApprove={approve}
              onOpenTaskArtifacts={openTaskArtifacts}
              workActioning={workActioning}
              onStartWork={submitWorkStart}
              artifactActioning={artifactActioning}
              onReviewArtifact={submitReviewArtifact}
              onReviseArtifact={submitReviseArtifact}
              onCodexReviseArtifact={submitCodexReviseArtifact}
              onMaterializeTasks={submitMaterializeTasks}
            />
          ) : (
            <LoadingPanel />
          )}
        </section>

        <aside className="space-y-5">
          <CommandPanel command={nextCommand} />
          <ProjectActivityPanel project={selectedProjectSummary} data={data} />
          <EnvironmentInputPanel
            envKey={envKey}
            envValue={envValue}
            setEnvKey={setEnvKey}
            setEnvValue={setEnvValue}
            onSubmit={submitEnvBinding}
          />
          <SetupWizardPanel setup={data?.setupStatus} actioning={setupActioning} onRunAction={submitSetupAction} />
          <ArtifactViewerPanel
            taskID={selectedArtifactTaskID}
            task={data?.tasks.find((task) => task.id === selectedArtifactTaskID)}
            artifacts={taskArtifacts}
            actioning={taskActioning}
            onAction={submitTaskAction}
          />
          <ChangeRequestPanel
            requests={data?.changeRequests ?? []}
            text={changeText}
            setText={setChangeText}
            actioning={changeActioning}
            onSubmit={submitChangeRequest}
            onAction={submitChangeRequestAction}
          />
          <DependencyRiskPanel
            risks={data?.dependencyRisks ?? []}
            dependencyName={dependencyName}
            dependencyReason={dependencyReason}
            dependencyManager={dependencyManager}
            dependencyType={dependencyType}
            dependencyRisk={dependencyRisk}
            dependencyAlternatives={dependencyAlternatives}
            dependencyFiles={dependencyFiles}
            setDependencyName={setDependencyName}
            setDependencyReason={setDependencyReason}
            setDependencyManager={setDependencyManager}
            setDependencyType={setDependencyType}
            setDependencyRisk={setDependencyRisk}
            setDependencyAlternatives={setDependencyAlternatives}
            setDependencyFiles={setDependencyFiles}
            onSubmit={submitDependencyApproval}
          />
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
  currentProject,
  selectedProjectID,
  onSelect,
  onNewProject
}: {
  projects: RegisteredProject[];
  currentProject?: CurrentProject;
  selectedProjectID: string;
  onSelect: (projectID: string) => void;
  onNewProject: () => void;
}) {
  return (
    <aside className="project-sidebar" aria-label="Projects">
      <div className="panel compact">
        <div className="panel-heading">
          <h2>Projects</h2>
          <ServerCog size={18} className="text-zinc-500" />
        </div>
        <ProjectSwitcher projects={projects} currentProject={currentProject} selectedProjectID={selectedProjectID} onSelect={onSelect} />
        <button className="new-project-button" type="button" onClick={onNewProject}>
          <Plus size={16} />
          New Project
        </button>
        {currentProject ? (
          <button className={`project-row ${selectedProjectID === "" ? "selected" : ""}`} onClick={() => onSelect("")} type="button">
            <span>{currentProject.display_name}</span>
            <small>
              Current / {currentProject.authority_runtime} / {currentProject.status}
            </small>
          </button>
        ) : null}
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
  currentProject,
  selectedProjectID,
  onSelect
}: {
  projects: RegisteredProject[];
  currentProject?: CurrentProject;
  selectedProjectID: string;
  onSelect: (projectID: string) => void;
}) {
  const disabled = projects.length === 0 && !currentProject;
  return (
    <select className="project-switcher" aria-label="Project" value={selectedProjectID} onChange={(event) => onSelect(event.target.value)} disabled={disabled}>
      {currentProject ? <option value="">Current Project</option> : null}
      {!currentProject && projects.length === 0 ? <option value="">No project</option> : null}
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
  onApprove,
  onOpenTaskArtifacts,
  workActioning,
  onStartWork,
  artifactActioning,
  onReviewArtifact,
  onReviseArtifact,
  onCodexReviseArtifact,
  onMaterializeTasks
}: {
  data: DashboardData;
  selectedProject?: RegisteredProject | CurrentProject;
  approving: string;
  featureText: string;
  setFeatureText: (value: string) => void;
  onSubmitFeature: () => void;
  onApprove: (item: InboxItem) => void;
  onOpenTaskArtifacts: (taskID: string) => void;
  workActioning: string;
  onStartWork: (adapter: "fake" | "real-codex") => void;
  artifactActioning: string;
  onReviewArtifact: (artifactID: string, version: number, status: "approved" | "approved_with_notes" | "rejected", notes: string) => void;
  onReviseArtifact: (artifactID: string, content: string) => void;
  onCodexReviseArtifact: (artifactID: string, instruction: string) => void;
  onMaterializeTasks: () => void;
}) {
  return (
    <>
      {selectedProject ? <ProjectStatusPanel project={selectedProject} /> : null}
      <ArtifactsPanel artifacts={data.artifacts} actioning={artifactActioning} onReview={onReviewArtifact} onRevise={onReviseArtifact} onCodexRevise={onCodexReviseArtifact} onMaterialize={onMaterializeTasks} />
      <Summary counts={data.snapshot.counts} generatedAt={data.snapshot.generated_at} lastMergeAt={data.snapshot.last_successful_merge_at} />
      <InboxPanel items={data.snapshot.open_inbox_items} decisions={data.decisions} approving={approving} onApprove={onApprove} />
      <RequestQueuePanel requests={data.featureRequests} queueItems={data.queueItems} featureText={featureText} setFeatureText={setFeatureText} onSubmitFeature={onSubmitFeature} />
      <WorkPlanningPanel data={data} actioning={workActioning} onStartWork={onStartWork} />
      <TaskPanel tasks={data.tasks} onOpenArtifacts={onOpenTaskArtifacts} />
    </>
  );
}

function NewProjectPanel({
  name,
  root,
  concept,
  runtime,
  wslDistro,
  runtimeOptions,
  actioning,
  setName,
  setRoot,
  setConcept,
  setRuntime,
  setWslDistro,
  openingProjectPath,
  onBrowse,
  onSubmit,
  onCancel
}: {
  name: string;
  root: string;
  concept: string;
  runtime: "windows" | "wsl";
  wslDistro: string;
  runtimeOptions: ProjectRuntimeOption[];
  actioning: boolean;
  setName: (value: string) => void;
  setRoot: (value: string) => void;
  setConcept: (value: string) => void;
  setRuntime: (value: "windows" | "wsl") => void;
  setWslDistro: (value: string) => void;
  openingProjectPath: boolean;
  onBrowse: () => void;
  onSubmit: () => void;
  onCancel: () => void;
}) {
  const availableOptions = runtimeOptions.filter((option) => option.available);
  const canSubmit = name.trim() !== "" && root.trim() !== "" && concept.trim() !== "" && availableOptions.some((option) => option.authority_runtime === runtime);
  const selectedOption = runtimeOptions.find((option) => option.authority_runtime === runtime);
  return (
    <section className="panel">
      <div className="panel-heading">
        <div>
          <h2>New Project</h2>
          <p>{selectedOption?.description ?? "Choose the runtime and initialize a local DevOS project."}</p>
        </div>
        <Plus size={20} className="text-zinc-500" />
      </div>
      <div className="new-project-name-row">
        <label>
          <span>Name</span>
          <input value={name} onChange={(event) => setName(event.target.value)} placeholder="Project name" disabled={actioning} />
        </label>
      </div>
      <label className="project-root-label">
        <span>Project root</span>
        <div className="path-input-row">
          <textarea className="project-root-field" value={root} onChange={(event) => setRoot(event.target.value)} placeholder="Project root" disabled={actioning} rows={2} />
          <button className="icon-text-button" type="button" onClick={onBrowse} disabled={actioning || openingProjectPath}>
            <FolderOpen size={16} />
            {openingProjectPath ? "Selecting" : "Browse"}
          </button>
        </div>
      </label>
      <div className="runtime-choice-row">
        {runtimeOptions.map((option) => (
          <button
            className={`runtime-choice ${option.authority_runtime === runtime ? "selected" : ""}`}
            type="button"
            key={option.authority_runtime}
            disabled={actioning || !option.available}
            onClick={() => {
              setRuntime(option.authority_runtime);
              setWslDistro(option.wsl_distro ?? "");
            }}
          >
            <span>{option.label}</span>
            <small>
              {option.recommended ? "Detected / recommended" : option.detected ? "Detected" : "Unavailable"}
              {option.wsl_distro ? ` / ${option.wsl_distro}` : ""}
            </small>
          </button>
        ))}
      </div>
      {runtime === "wsl" ? (
        <label className="compact-label">
          <span>WSL distro</span>
          <input value={wslDistro} onChange={(event) => setWslDistro(event.target.value)} placeholder="Ubuntu" disabled={actioning} />
        </label>
      ) : null}
      <label className="compact-label">
        <span>Concept</span>
        <textarea value={concept} onChange={(event) => setConcept(event.target.value)} placeholder="What do you want this project to become?" disabled={actioning} rows={5} />
      </label>
      <div className="toolbar-row">
        <button type="button" onClick={onSubmit} disabled={!canSubmit || actioning}>
          {actioning ? "Creating" : "Create project"}
        </button>
        <button className="secondary-button no-margin" type="button" onClick={onCancel} disabled={actioning}>
          Cancel
        </button>
      </div>
    </section>
  );
}

function ProjectStatusPanel({ project }: { project: RegisteredProject | CurrentProject }) {
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

function WorkPlanningPanel({
  data,
  actioning,
  onStartWork
}: {
  data: DashboardData;
  actioning: string;
  onStartWork: (adapter: "fake" | "real-codex") => void;
}) {
  return (
    <section className="panel">
      <div className="panel-heading">
        <div>
          <h2>Work And Planning</h2>
          <p>{data.workStatus.worker_runs.length} worker runs</p>
        </div>
        <Wrench size={20} className="text-zinc-500" />
      </div>
      <div className="toolbar-row">
        <button className="secondary-button" type="button" onClick={() => onStartWork("fake")} disabled={actioning !== ""}>
          {actioning === "fake" ? "Running fake" : "Run fake worker"}
        </button>
        <button className="secondary-button" type="button" onClick={() => onStartWork("real-codex")} disabled={actioning !== ""}>
          {actioning === "real-codex" ? "Running Codex" : "Run Codex worker"}
        </button>
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

function TaskPanel({ tasks, onOpenArtifacts }: { tasks: DashboardData["tasks"]; onOpenArtifacts: (taskID: string) => void }) {
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
            <button className="secondary-button" type="button" onClick={() => onOpenArtifacts(task.id)}>
              Artifacts
            </button>
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

function SetupWizardPanel({
  setup,
  actioning,
  onRunAction
}: {
  setup?: DashboardData["setupStatus"];
  actioning: string;
  onRunAction: (actionID: string) => void;
}) {
  const steps = setup
    ? [
        { label: "Project root", done: Boolean(setup.project_root) },
        { label: "Git repository", done: setup.git_repository },
        { label: ".env.local ignored", done: setup.gitignore_env_local },
        { label: "Verification commands", done: setup.required_verification_configured },
        { label: "Protected paths", done: setup.protected_paths.length > 0 },
        { label: "Env bindings", done: setup.environment_bindings.length > 0 },
        { label: "Toolchain setup", done: setup.toolchain_setup_cards.length === 0 },
        { label: "Git clean", done: setup.git_clean }
      ]
    : [];
  return (
    <section className="panel compact">
      <div className="panel-heading">
        <h2>Setup Wizard</h2>
        <Wrench size={18} className="text-zinc-500" />
      </div>
      {!setup ? (
        <div className="empty-stack">Setup status unavailable</div>
      ) : (
        <div className="stack">
          {steps.map((step) => (
            <div className="setup-step" key={step.label}>
              <span className={step.done ? "step-dot done" : "step-dot"} />
              <span>{step.label}</span>
            </div>
          ))}
          <StackEmpty empty={(setup.blockers ?? []).length === 0} label="Project is ready for guarded operation">
            {(setup.blockers ?? []).map((blocker) => (
              <div className="stack-row" key={blocker}>
                <span>{blocker}</span>
                <small>setup blocker</small>
              </div>
            ))}
          </StackEmpty>
          <StackEmpty empty={(setup.actions ?? []).length === 0} label="No setup actions">
            {(setup.actions ?? []).map((action) => (
              <div className="stack-row" key={action.id}>
                <span>{action.label}</span>
                <small>{action.enabled ? "ready" : action.reason}</small>
                <code className="inline-command">{action.command}</code>
                <button className="secondary-button" type="button" onClick={() => onRunAction(action.id)} disabled={!action.enabled || actioning === action.id}>
                  {actioning === action.id ? "Running" : "Run"}
                </button>
              </div>
            ))}
          </StackEmpty>
        </div>
      )}
    </section>
  );
}

function ArtifactViewerPanel({
  taskID,
  task,
  artifacts,
  actioning,
  onAction
}: {
  taskID: string;
  task?: DashboardData["tasks"][number];
  artifacts: TaskArtifact[];
  actioning: string;
  onAction: (taskID: string, action: "verify" | "review-approve" | "review-reject" | "merge-approve") => void;
}) {
  return (
    <section className="panel compact">
      <div className="panel-heading">
        <h2>Diff & Artifacts</h2>
        <FileCheck2 size={18} className="text-zinc-500" />
      </div>
      {task ? (
        <div className="artifact-actions">
          <span>
            {task.id} / {task.status}
          </span>
          <button className="secondary-button" type="button" onClick={() => onAction(task.id, "verify")} disabled={task.status !== "verifying" || actioning === `${task.id}:verify`}>
            Re-run verify
          </button>
          <button className="secondary-button" type="button" onClick={() => onAction(task.id, "review-approve")} disabled={task.status !== "ready_for_human_review" || actioning === `${task.id}:review-approve`}>
            Approve
          </button>
          <button className="secondary-button" type="button" onClick={() => onAction(task.id, "review-reject")} disabled={task.status !== "ready_for_human_review" || actioning === `${task.id}:review-reject`}>
            Request changes
          </button>
          <button
            className="secondary-button"
            type="button"
            onClick={() => onAction(task.id, "merge-approve")}
            disabled={(task.status !== "ready_for_human_review" && task.status !== "approved_for_merge") || actioning === `${task.id}:merge-approve`}
          >
            Merge approve
          </button>
        </div>
      ) : null}
      <StackEmpty empty={!taskID || artifacts.length === 0} label={taskID ? "No artifacts for selected task" : "Select a task"}>
        {artifacts.slice(0, 12).map((artifact) => (
          <div className="stack-row" key={artifact.id}>
            <span>
              {artifact.artifact_type} / {artifact.artifact_key}
            </span>
            <small>
              {artifact.run_id} / {artifact.run_status}
            </small>
            <small>{artifact.path}</small>
            {artifact.content ? <pre className="artifact-content">{artifact.content.slice(0, 4000)}</pre> : null}
          </div>
        ))}
      </StackEmpty>
    </section>
  );
}

function ChangeRequestPanel({
  requests,
  text,
  setText,
  actioning,
  onSubmit,
  onAction
}: {
  requests: DashboardData["changeRequests"];
  text: string;
  setText: (value: string) => void;
  onSubmit: () => void;
  actioning: string;
  onAction: (id: string, action: "analyze" | "approve") => void;
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
            <div className="toolbar-row">
              <button className="secondary-button" type="button" onClick={() => onAction(request.id, "analyze")} disabled={request.status !== "proposed" || actioning !== ""}>
                {actioning === `${request.id}:analyze` ? "Analyzing" : "Analyze"}
              </button>
              <button className="secondary-button" type="button" onClick={() => onAction(request.id, "approve")} disabled={request.status !== "analyzed" || actioning !== ""}>
                {actioning === `${request.id}:approve` ? "Approving" : "Approve"}
              </button>
            </div>
          </div>
        ))}
      </StackEmpty>
    </section>
  );
}

function DependencyRiskPanel({
  risks,
  dependencyName,
  dependencyReason,
  dependencyManager,
  dependencyType,
  dependencyRisk,
  dependencyAlternatives,
  dependencyFiles,
  setDependencyName,
  setDependencyReason,
  setDependencyManager,
  setDependencyType,
  setDependencyRisk,
  setDependencyAlternatives,
  setDependencyFiles,
  onSubmit
}: {
  risks: DashboardData["dependencyRisks"];
  dependencyName: string;
  dependencyReason: string;
  dependencyManager: string;
  dependencyType: string;
  dependencyRisk: string;
  dependencyAlternatives: string;
  dependencyFiles: string;
  setDependencyName: (value: string) => void;
  setDependencyReason: (value: string) => void;
  setDependencyManager: (value: string) => void;
  setDependencyType: (value: string) => void;
  setDependencyRisk: (value: string) => void;
  setDependencyAlternatives: (value: string) => void;
  setDependencyFiles: (value: string) => void;
  onSubmit: () => void;
}) {
  return (
    <section className="panel compact">
      <div className="panel-heading">
        <h2>Dependency Risk</h2>
        <ShieldAlert size={18} className="text-zinc-500" />
      </div>
      <div className="compact-form">
        <input value={dependencyName} onChange={(event) => setDependencyName(event.target.value)} placeholder="package name" />
        <select aria-label="Package manager" value={dependencyManager} onChange={(event) => setDependencyManager(event.target.value)}>
          <option value="npm">npm</option>
          <option value="pnpm">pnpm</option>
          <option value="go">go</option>
          <option value="yarn">yarn</option>
          <option value="cargo">cargo</option>
          <option value="other">other</option>
        </select>
        <select aria-label="Dependency type" value={dependencyType} onChange={(event) => setDependencyType(event.target.value)}>
          <option value="production">production</option>
          <option value="development">development</option>
          <option value="tool">tool</option>
        </select>
        <select aria-label="Dependency risk" value={dependencyRisk} onChange={(event) => setDependencyRisk(event.target.value)}>
          <option value="low">low</option>
          <option value="medium">medium</option>
          <option value="high">high</option>
          <option value="critical">critical</option>
        </select>
        <input value={dependencyReason} onChange={(event) => setDependencyReason(event.target.value)} placeholder="reason" />
        <input value={dependencyAlternatives} onChange={(event) => setDependencyAlternatives(event.target.value)} placeholder="alternatives" />
        <input value={dependencyFiles} onChange={(event) => setDependencyFiles(event.target.value)} placeholder="files affected" />
        <button onClick={onSubmit} disabled={!dependencyName.trim() || !dependencyReason.trim()}>
          Request
        </button>
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
      <div className="table-shell" tabIndex={0} aria-label="Open inbox items">
        <table>
          <thead>
            <tr>
              <th>Priority</th>
              <th>Type</th>
              <th>Title</th>
              <th>Source</th>
              <th>Action</th>
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
                    <div className="item-body">Recommended: {recommendedInboxAction(item, decisions)}</div>
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

function ProjectActivityPanel({ project, data }: { project?: RegisteredProject | CurrentProject; data: DashboardData | null }) {
  return (
    <section className="panel compact">
      <div className="panel-heading">
        <h2>Project Activity</h2>
        <Route size={18} className="text-zinc-500" />
      </div>
      {project ? (
        <div className="stack">
          <div className="stack-row">
            <span>{project.display_name}</span>
            <small>
              {project.authority_runtime} / {project.status}
            </small>
            <small>{project.project_root}</small>
          </div>
          <div className="activity-grid">
            <div>
              <span>{data?.artifacts.length ?? 0}</span>
              <small>Artifacts</small>
            </div>
            <div>
              <span>{data?.trustedArtifacts.length ?? 0}</span>
              <small>Approved</small>
            </div>
            <div>
              <span>{data?.tasks.length ?? 0}</span>
              <small>Tasks</small>
            </div>
            <div>
              <span>{data?.queueItems.length ?? 0}</span>
              <small>Queue</small>
            </div>
            <div>
              <span>{data?.workStatus.worker_runs.length ?? 0}</span>
              <small>Workers</small>
            </div>
          </div>
        </div>
      ) : (
        <div className="empty-stack">No selected project</div>
      )}
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

function ArtifactsPanel({
  artifacts,
  actioning,
  onReview,
  onRevise,
  onCodexRevise,
  onMaterialize
}: {
  artifacts: DashboardData["artifacts"];
  actioning: string;
  onReview: (artifactID: string, version: number, status: "approved" | "approved_with_notes" | "rejected", notes: string) => void;
  onRevise: (artifactID: string, content: string) => void;
  onCodexRevise: (artifactID: string, instruction: string) => void;
  onMaterialize: () => void;
}) {
  const [reviewNotes, setReviewNotes] = useState<Record<string, string>>({});
  const [revisionRequests, setRevisionRequests] = useState<Record<string, boolean>>({});
  const [moreMenus, setMoreMenus] = useState<Record<string, boolean>>({});
  const [editingArtifacts, setEditingArtifacts] = useState<Record<string, boolean>>({});
  const [revisionContent, setRevisionContent] = useState<Record<string, string>>({});
  const pendingCount = artifacts.filter((artifact) => artifact.latest_version && artifact.approved_version !== artifact.latest_version && artifact.status !== "approved").length;
  return (
    <section className="panel">
      <div className="panel-heading">
        <div>
          <h2>Artifacts</h2>
          <p>{pendingCount > 0 ? `${pendingCount} waiting for review` : `${artifacts.length} artifacts`}</p>
        </div>
        <FileCheck2 size={18} className="text-zinc-500" />
      </div>
      <div className="toolbar-row">
        <button className="secondary-button" type="button" onClick={onMaterialize} disabled={actioning !== ""}>
          {actioning === "materialize" ? "Materializing" : "Materialize tasks"}
        </button>
      </div>
      <StackEmpty empty={artifacts.length === 0} label="No artifacts">
        {artifacts.map((artifact) => {
          const canReview = artifact.latest_version ? artifact.approved_version !== artifact.latest_version && artifact.status !== "approved" : false;
          const canRevise = Boolean(artifact.latest_version && artifact.content);
          const notes = reviewNotes[artifact.artifact_id] ?? "";
          const trimmedNotes = notes.trim();
          const approveStatus = trimmedNotes ? "approved_with_notes" : "approved";
          const isEditing = editingArtifacts[artifact.artifact_id] ?? false;
          const draftContent = revisionContent[artifact.artifact_id] ?? artifact.content ?? "";
          const hasRevisionChange = draftContent !== (artifact.content ?? "");
          const requestOpen = revisionRequests[artifact.artifact_id] ?? false;
          const moreOpen = moreMenus[artifact.artifact_id] ?? false;
          const isCodexRevising = actioning === `codex-revise:${artifact.artifact_id}`;
          return (
            <div className={`artifact-review-card ${isCodexRevising ? "is-busy" : ""}`} key={artifact.artifact_id} aria-busy={isCodexRevising}>
              <div className="artifact-review-body">
                <div className="artifact-review-header">
                  <div>
                    <span>{artifact.artifact_type}</span>
                    <small>
                      {artifact.status} / latest v{artifact.latest_version || 0} / approved v{artifact.approved_version || 0}
                    </small>
                    <small>{artifact.path}</small>
                  </div>
                </div>
                <ArtifactContentPreview
                  artifact={artifact}
                  canEdit={canRevise && actioning === ""}
                  onEdit={() => {
                    setRevisionContent((previous) => ({ ...previous, [artifact.artifact_id]: previous[artifact.artifact_id] ?? artifact.content ?? "" }));
                    setEditingArtifacts((previous) => ({ ...previous, [artifact.artifact_id]: true }));
                  }}
                />
                {isEditing ? (
                  <label className="artifact-revision-editor">
                    <span>Revision content</span>
                    <textarea
                      value={draftContent}
                      onChange={(event) => setRevisionContent((previous) => ({ ...previous, [artifact.artifact_id]: event.target.value }))}
                      rows={10}
                      disabled={actioning !== ""}
                    />
                  </label>
                ) : null}
                {isEditing ? (
                  <div className="artifact-review-actions">
                    <button
                      className="secondary-button no-margin"
                      type="button"
                      onClick={() => setEditingArtifacts((previous) => ({ ...previous, [artifact.artifact_id]: false }))}
                      disabled={actioning !== ""}
                    >
                      Close editor
                    </button>
                    <button
                      className="secondary-button no-margin"
                      type="button"
                      onClick={() => onRevise(artifact.artifact_id, draftContent)}
                      disabled={!canRevise || !draftContent.trim() || !hasRevisionChange || actioning !== ""}
                    >
                      {actioning === `revise:${artifact.artifact_id}` ? "Saving" : "Save manual revision"}
                    </button>
                  </div>
                ) : null}
                {requestOpen ? (
                  <label className="artifact-review-notes">
                    <span>What should change?</span>
                    <textarea
                      value={notes}
                      onChange={(event) => setReviewNotes((previous) => ({ ...previous, [artifact.artifact_id]: event.target.value }))}
                      placeholder="Tell Codex what to change before approval"
                      rows={3}
                      disabled={actioning !== ""}
                    />
                  </label>
                ) : null}
                <div className="artifact-review-actions">
                  <button
                    className="secondary-button no-margin"
                    type="button"
                    onClick={() => onReview(artifact.artifact_id, artifact.latest_version || 1, approveStatus, trimmedNotes || "Approved from UI")}
                    disabled={!canReview || actioning !== ""}
                  >
                    {actioning === `${approveStatus}:${artifact.artifact_id}` ? "Approving" : trimmedNotes ? "Approve with notes" : "Approve latest"}
                  </button>
                  <button
                    className="secondary-button no-margin"
                    type="button"
                    onClick={() => setRevisionRequests((previous) => ({ ...previous, [artifact.artifact_id]: !requestOpen }))}
                    disabled={!canRevise || actioning !== ""}
                    aria-expanded={requestOpen}
                  >
                    {requestOpen ? "Close revision request" : "Request revision"}
                  </button>
                  {requestOpen ? (
                    <button
                      className="secondary-button no-margin"
                      type="button"
                      onClick={() => onCodexRevise(artifact.artifact_id, trimmedNotes)}
                      disabled={!canRevise || !trimmedNotes || actioning !== ""}
                    >
                      {isCodexRevising ? "Revising" : "Revise with Codex"}
                    </button>
                  ) : null}
                  <div className="artifact-more">
                    <button
                      className="icon-button small"
                      type="button"
                      onClick={() => setMoreMenus((previous) => ({ ...previous, [artifact.artifact_id]: !moreOpen }))}
                      disabled={actioning !== ""}
                      title="More artifact actions"
                      aria-label={`More actions for ${artifact.artifact_type}`}
                      aria-expanded={moreOpen}
                    >
                      <MoreHorizontal size={16} />
                    </button>
                    {moreOpen ? (
                      <div className="artifact-more-menu">
                        <button
                          className="secondary-button no-margin"
                          type="button"
                          onClick={() => onReview(artifact.artifact_id, artifact.latest_version || 1, "rejected", trimmedNotes)}
                          disabled={!canReview || !trimmedNotes || actioning !== ""}
                        >
                          {actioning === `rejected:${artifact.artifact_id}` ? "Marking" : "Mark as rejected"}
                        </button>
                      </div>
                    ) : null}
                  </div>
                </div>
              </div>
              {isCodexRevising ? (
                <div className="artifact-busy-overlay" role="status" aria-live="polite">
                  <div className="artifact-spinner" />
                  <strong>Codex is revising {artifact.artifact_type}</strong>
                  <span>The new version will appear here after it is saved.</span>
                </div>
              ) : null}
            </div>
          );
        })}
      </StackEmpty>
    </section>
  );
}

function ArtifactContentPreview({ artifact, canEdit, onEdit }: { artifact: ArtifactRecord; canEdit: boolean; onEdit: () => void }) {
  if (!artifact.content) {
    return <div className="empty-stack">Content preview unavailable</div>;
  }
  const content = artifact.content.slice(0, 6000);
  const editButton = (
    <button className="artifact-edit-button" type="button" onClick={onEdit} disabled={!canEdit} title="Edit manually" aria-label={`Edit ${artifact.artifact_type} manually`}>
      <Pencil size={14} />
    </button>
  );
  if (artifact.path?.toLowerCase().endsWith(".md")) {
    return (
      <div className="artifact-preview-shell">
        {editButton}
        <div className="markdown-preview">{renderMarkdown(content)}</div>
      </div>
    );
  }
  return (
    <div className="artifact-preview-shell">
      {editButton}
      <pre className="artifact-content artifact-content-review">{content}</pre>
    </div>
  );
}

function renderMarkdown(content: string): ReactNode[] {
  const lines = content.replace(/\r\n/g, "\n").split("\n");
  const nodes: ReactNode[] = [];
  let paragraph: string[] = [];
  let listItems: string[] = [];
  let codeLines: string[] = [];
  let inCode = false;

  const flushParagraph = () => {
    if (paragraph.length === 0) return;
    nodes.push(<p key={`p-${nodes.length}`}>{renderInline(paragraph.join(" "))}</p>);
    paragraph = [];
  };
  const flushList = () => {
    if (listItems.length === 0) return;
    nodes.push(
      <ul key={`ul-${nodes.length}`}>
        {listItems.map((item, index) => (
          <li key={`${index}-${item}`}>{renderInline(item)}</li>
        ))}
      </ul>
    );
    listItems = [];
  };
  const flushCode = () => {
    nodes.push(
      <pre className="markdown-code" key={`code-${nodes.length}`}>
        {codeLines.join("\n")}
      </pre>
    );
    codeLines = [];
  };

  lines.forEach((line) => {
    if (line.trim().startsWith("```")) {
      if (inCode) {
        flushCode();
        inCode = false;
      } else {
        flushParagraph();
        flushList();
        inCode = true;
      }
      return;
    }
    if (inCode) {
      codeLines.push(line);
      return;
    }
    if (!line.trim()) {
      flushParagraph();
      flushList();
      return;
    }
    const heading = /^(#{1,6})\s+(.+)$/.exec(line);
    if (heading) {
      flushParagraph();
      flushList();
      nodes.push(renderMarkdownHeading(heading[1].length, heading[2], nodes.length));
      return;
    }
    const unordered = /^\s*[-*]\s+(.+)$/.exec(line);
    const ordered = /^\s*\d+\.\s+(.+)$/.exec(line);
    if (unordered || ordered) {
      flushParagraph();
      listItems.push((unordered ?? ordered)?.[1] ?? line.trim());
      return;
    }
    flushList();
    paragraph.push(line.trim());
  });

  if (inCode) flushCode();
  flushParagraph();
  flushList();
  return nodes;
}

function renderMarkdownHeading(level: number, text: string, key: number) {
  if (level <= 1) return <h3 key={`h-${key}`}>{renderInline(text)}</h3>;
  if (level === 2) return <h4 key={`h-${key}`}>{renderInline(text)}</h4>;
  return <h5 key={`h-${key}`}>{renderInline(text)}</h5>;
}

function renderInline(text: string): ReactNode[] {
  return text.split(/(`[^`]+`)/g).map((part, index) => {
    if (part.startsWith("`") && part.endsWith("`")) {
      return <code key={`${index}-${part}`}>{part.slice(1, -1)}</code>;
    }
    return <span key={`${index}-${part}`}>{part}</span>;
  });
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

function NoticeBanner({ message }: { message: string }) {
  return (
    <div className="notice-banner">
      <Check size={18} />
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

function recommendedInboxAction(item: InboxItem, decisions: Decision[]) {
  if (item.source_type === "decision") {
    const option = decisions.find((decision) => decision.id === item.source_id)?.options?.[0]?.label;
    return option ? `choose ${option}` : "open the related decision";
  }
  if (item.source_type === "human_approval") {
    return "review evidence, then approve or request changes";
  }
  if (item.item_type.includes("setup") || item.source_type.includes("toolchain")) {
    return "complete setup and rerun doctor";
  }
  return "inspect the linked task/run artifact";
}

function upsertProject(projects: RegisteredProject[], project: RegisteredProject) {
  const next = projects.filter((item) => item.id !== project.id);
  next.unshift(project);
  return next;
}

export default App;
