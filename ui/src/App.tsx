import { AlertTriangle, Check, FileCheck2, GitMerge, Inbox, ListChecks, RefreshCcw, ServerCog, ShieldAlert } from "lucide-react";
import type { ReactNode } from "react";
import { useEffect, useMemo, useState } from "react";
import { approveInboxItem, loadDashboardData } from "./api";
import type { DashboardData, Decision, InboxItem, MemoryRecord, SnapshotCounts } from "./types";

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
  const [data, setData] = useState<DashboardData | null>(null);
  const [error, setError] = useState<string>("");
  const [loading, setLoading] = useState(true);
  const [approving, setApproving] = useState<string>("");

  const refresh = async () => {
    setLoading(true);
    setError("");
    try {
      setData(await loadDashboardData());
    } catch (err) {
      setError(err instanceof Error ? err.message : "Dashboard load failed");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void refresh();
  }, []);

  const nextCommand = useMemo(() => data?.snapshot.recommended_next_commands?.[0] ?? "devos request --json <TEXT>", [data]);

  const approve = async (item: InboxItem) => {
    setApproving(item.id);
    setError("");
    try {
      const option = item.source_type === "decision" ? firstDecisionOption(data?.decisions ?? [], item.source_id) : undefined;
      await approveInboxItem(item.id, "Approved from DevOS UI", option);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Approval failed");
    } finally {
      setApproving("");
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

      <div className="mx-auto grid max-w-7xl gap-5 px-5 py-5 lg:grid-cols-[1fr_340px]">
        <section className="space-y-5">
          {error ? <ErrorBanner message={error} /> : null}
          {data ? (
            <>
              <Summary counts={data.snapshot.counts} generatedAt={data.snapshot.generated_at} lastMergeAt={data.snapshot.last_successful_merge_at} />
              <InboxPanel items={data.snapshot.open_inbox_items} decisions={data.decisions} approving={approving} onApprove={approve} />
            </>
          ) : (
            <LoadingPanel />
          )}
        </section>

        <aside className="space-y-5">
          <CommandPanel command={nextCommand} />
          <TrustedArtifactsPanel artifacts={data?.trustedArtifacts ?? []} />
          <DecisionPanel decisions={data?.decisions ?? []} />
          <BaselinePanel issues={data?.baselineIssues ?? []} />
        </aside>
      </div>
    </main>
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
