import { Brain, Check, ShieldAlert, X } from "lucide-react";
import type { ApprovalPacket, UnderstandingSnapshot } from "../types";

type UnderstandingPanelProps = {
  snapshots: UnderstandingSnapshot[];
  packets: ApprovalPacket[];
  actioning: string;
  onApprovePacket: (packetID: string, option: "approve_recommended" | "request_changes" | "cancel") => void;
};

export function UnderstandingPanel({ snapshots, packets, actioning, onApprovePacket }: UnderstandingPanelProps) {
  const latestSnapshot = [...snapshots].sort((a, b) => b.created_at.localeCompare(a.created_at))[0];
  const openPacket = packets.find((packet) => packet.status === "open");
  const latestPacket = openPacket ?? [...packets].sort((a, b) => b.created_at.localeCompare(a.created_at))[0];

  if (!latestSnapshot && !latestPacket) {
    return null;
  }

  const snapshot = latestSnapshot;
  const packet = latestPacket;
  const scope = packet?.summary.proposed_scope;
  const risk = packet?.risk_level ?? snapshot?.risk.level ?? "L1";
  const openQuestions = packet?.summary.open_questions ?? snapshot?.open_questions ?? [];
  const assumptions = packet?.summary.assumptions ?? snapshot?.assumptions ?? [];

  return (
    <section className="panel understanding-panel">
      <div className="panel-heading">
        <div>
          <h2>Understanding Review</h2>
          <p>{packet?.title ?? snapshot?.interpreted_goal[0] ?? "Current interpretation"}</p>
        </div>
        <Brain size={20} className="text-zinc-500" />
      </div>

      <div className="understanding-header">
        <span className={`risk-badge risk-${risk.toLowerCase()}`}>{risk}</span>
        <span className="status-badge">{packet?.status ?? snapshot?.status}</span>
        <span className="runtime-badge muted">{packet?.summary.next_action ?? snapshot?.recommended_go_mode}</span>
      </div>

      <div className="understanding-grid">
        <div>
          <h3>Goal</h3>
          <ul>{(snapshot?.interpreted_goal ?? [packet?.summary.one_liner ?? ""]).filter(Boolean).map((item) => <li key={item}>{item}</li>)}</ul>
        </div>
        <div>
          <h3>Scope</h3>
          <ul>{(scope?.included ?? []).map((item) => <li key={item}>{item}</li>)}</ul>
        </div>
        <div>
          <h3>Non-goals</h3>
          <ul>{(snapshot?.non_goals ?? scope?.excluded ?? []).map((item) => <li key={item}>{item}</li>)}</ul>
        </div>
        <div>
          <h3>Affected</h3>
          <ul>{formatAffected(packet?.summary.existing_alignment ?? snapshot?.affected_context).map((item) => <li key={item}>{item}</li>)}</ul>
        </div>
      </div>

      <div className="understanding-grid compact">
        <div>
          <h3>Assumptions</h3>
          <ul>{assumptions.length ? assumptions.map((item) => <li key={item.id}>{item.text}</li>) : <li>None recorded</li>}</ul>
        </div>
        <div>
          <h3>Open Questions</h3>
          <ul>{openQuestions.length ? openQuestions.map((item) => <li key={item.id}>{item.question}</li>) : <li>None blocking</li>}</ul>
        </div>
        <div>
          <h3>Risk</h3>
          <ul>{(packet?.summary.risk.reasons ?? snapshot?.risk.reasons ?? []).map((item) => <li key={item}>{item}</li>)}</ul>
        </div>
      </div>

      {packet?.status === "open" ? (
        <div className="understanding-actions">
          <span className="merge-status">
            <ShieldAlert size={16} />
            {packet.summary.recommendation.reason}
          </span>
          <button className="icon-button" type="button" onClick={() => onApprovePacket(packet.id, "approve_recommended")} disabled={actioning === packet.id} title="Approve recommended" aria-label="Approve recommended">
            <Check size={17} />
          </button>
          <button className="icon-button" type="button" onClick={() => onApprovePacket(packet.id, "request_changes")} disabled={actioning === packet.id} title="Request changes" aria-label="Request changes">
            <X size={17} />
          </button>
        </div>
      ) : null}
    </section>
  );
}

function formatAffected(context?: { artifacts?: string[]; files?: string[]; workflows?: string[]; human_inbox?: string[] }) {
  if (!context) return ["None recorded"];
  const items = [...(context.artifacts ?? []), ...(context.files ?? []), ...(context.workflows ?? []), ...(context.human_inbox ?? [])];
  return items.length ? items : ["None recorded"];
}
