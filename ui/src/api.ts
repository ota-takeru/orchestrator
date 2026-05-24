import type { DashboardData, Decision, HumanInboxSnapshot, MemoryRecord } from "./types";

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

export async function loadDashboardData(): Promise<DashboardData> {
  const [snapshot, decisionBody, memoryBody] = await Promise.all([
    getJSON<HumanInboxSnapshot>("/api/ui/snapshot?limit=12"),
    getJSON<{ decisions: Decision[] }>("/api/decisions?status=open"),
    getJSON<{ memories: MemoryRecord[] }>("/api/memory?type=baseline_issue")
  ]);

  return {
    snapshot,
    decisions: decisionBody.decisions,
    baselineIssues: memoryBody.memories
  };
}

export async function approveInboxItem(id: string, notes: string): Promise<void> {
  const response = await fetch(`/api/inbox/${encodeURIComponent(id)}/approve`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json"
    },
    body: JSON.stringify({ notes })
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `${response.status} ${response.statusText}`);
  }
}
