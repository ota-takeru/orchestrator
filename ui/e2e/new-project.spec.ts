import AxeBuilder from "@axe-core/playwright";
import { expect, type Page, type TestInfo, test } from "@playwright/test";
import path from "node:path";

async function createProjectFromUI(page: Page, testInfo: TestInfo, name: string, concept: string) {
  const projectRoot = path.normalize(testInfo.outputPath(name.toLowerCase().replaceAll(" ", "-")));

  await page.goto("/");
  await page.getByRole("button", { name: "New Project" }).click();
  await page.getByLabel("Name").fill(name);
  await page.getByLabel("Project root").fill(projectRoot);
  await page.getByPlaceholder("What do you want this project to become?").fill(concept);
  await page.getByRole("button", { name: "Create project" }).click();
  await expect(page.getByText(`${name} was created and selected.`)).toBeVisible({ timeout: 30_000 });

  return projectRoot;
}

async function moveToQueuedImplementation(page: Page, testInfo: TestInfo, name: string) {
  const projectRoot = await createProjectFromUI(
    page,
    testInfo,
    name,
    "A tiny app used to exercise every visible workflow transition button."
  );
  await page.getByRole("button", { name: "Approve all generated plan" }).click();
  await expect(page.getByText("4 artifact(s) approved. Next: create an implementation task.")).toBeVisible();
  await page.getByRole("button", { name: "Create implementation task" }).click();
  await expect(page.getByText("1 task(s) materialized and queued. Next: run a worker.")).toBeVisible();
  return projectRoot;
}

async function runSimulationToReview(page: Page) {
  await page.getByRole("button", { name: "Run simulation" }).click();
  await expect(page.getByText("Fake worker finished with 1 execution item(s).")).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText("TASK-001 / ready_for_human_review")).toBeVisible();
}

test("new project form defaults the name and project root", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "New Project" }).click();

  await expect(page.getByLabel("Name")).toHaveValue("New Project");
  await expect(page.getByLabel("Project root")).toHaveValue(/new-project/i);

  await page.getByLabel("Name").fill("Daily Notes");
  await expect(page.getByLabel("Project root")).toHaveValue(/daily-notes/i);
});

test("creating a project selects it and replaces the creation form with its dashboard", async ({ page }, testInfo) => {
  const projectRoot = path.normalize(testInfo.outputPath("created-project"));

  await page.goto("/");
  await page.getByRole("button", { name: "New Project" }).click();
  await page.getByLabel("Name").fill("UI Created Project");
  await page.getByLabel("Project root").fill(projectRoot);
  await page.getByPlaceholder("What do you want this project to become?").fill("A project created by the UI end-to-end check.");
  await page.getByRole("button", { name: "Create project" }).click();

  await expect(page.getByText("UI Created Project was created and selected.")).toBeVisible({ timeout: 30_000 });
  await expect(page.getByRole("heading", { name: "UI Created Project" })).toBeVisible();
  await expect(page.getByLabel("Project root")).toHaveCount(0);
  await expect(page.getByRole("heading", { name: "Project Activity" })).toBeVisible();
  const activityPanel = page.locator("section", { has: page.getByRole("heading", { name: "Project Activity" }) });
  await expect(activityPanel.getByText(projectRoot)).toBeVisible();
  await expect(page.getByRole("heading", { name: "Artifacts", exact: true })).toBeVisible();
  await expect(page.getByText("4 waiting for review")).toBeVisible();
  const prdCard = page.locator(".artifact-review-card", { hasText: "prd" });
  await expect(prdCard).toContainText("PRD");
  await prdCard.getByRole("button", { name: "Review content" }).click();
  await expect(prdCard.locator(".markdown-preview").getByRole("heading", { name: "PRD" })).toBeVisible();
  await expect(prdCard.locator(".markdown-preview")).toContainText("A project created by the UI end-to-end check.");
  await expect(page.locator(".error-banner")).toHaveCount(0);
});

test("artifact review supports revision requests and manual edits", async ({ page }, testInfo) => {
  const projectRoot = path.normalize(testInfo.outputPath("review-project"));

  await page.goto("/");
  await page.getByRole("button", { name: "New Project" }).click();
  await page.getByLabel("Name").fill("UI Review Project");
  await page.getByLabel("Project root").fill(projectRoot);
  await page.getByPlaceholder("What do you want this project to become?").fill("A project used to review generated markdown artifacts.");
  await page.getByRole("button", { name: "Create project" }).click();
  await expect(page.getByText("UI Review Project was created and selected.")).toBeVisible({ timeout: 30_000 });

  const prdCard = page.locator(".artifact-review-card", { hasText: "prd" });
  await prdCard.getByRole("button", { name: "Review content" }).click();
  await expect(prdCard.locator(".markdown-preview").getByRole("heading", { name: "PRD" })).toBeVisible();
  await expect(prdCard.getByRole("button", { name: "Request revision" })).toBeEnabled();
  await expect(prdCard.getByLabel("What should change?")).toHaveCount(0);
  await prdCard.getByRole("button", { name: "Request revision" }).click();
  await expect(prdCard.getByLabel("What should change?")).toBeEnabled();
  await expect(prdCard.getByRole("button", { name: "Revise with Codex" })).toBeDisabled();
  await prdCard.getByLabel("What should change?").fill("Add a clearer success metric before approval.");
  await expect(prdCard.getByRole("button", { name: "Revise with Codex" })).toBeEnabled();
  let finishRevision!: () => void;
  await page.route("**/revise-with-codex", async (route) => {
    await new Promise<void>((resolve) => {
      finishRevision = resolve;
    });
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ classification: "succeeded" })
    });
  });
  const revisionResponse = page.waitForResponse((response) => response.url().includes("revise-with-codex") && response.request().method() === "POST");
  await prdCard.getByRole("button", { name: "Revise with Codex" }).click();
  await expect(prdCard.getByRole("status")).toContainText("Codex is revising prd");
  finishRevision();
  await revisionResponse;
  await expect(page.getByText("Codex artifact revision saved.")).toBeVisible();
  await page.unroute("**/revise-with-codex");
  await prdCard.getByRole("button", { name: /More actions for prd/ }).click();
  await prdCard.getByRole("button", { name: "Mark as rejected" }).click();
  await expect(page.getByText("Artifact changes requested.")).toBeVisible();
  await expect(prdCard.getByText("rejected / latest v1 / approved v0")).toBeVisible();

  await prdCard.getByRole("button", { name: /Edit prd manually/ }).click();
  await prdCard.getByLabel("Revision content").fill("# PRD\n\nSecond draft with a measurable success metric.");
  await prdCard.getByRole("button", { name: "Save manual revision" }).click();
  await expect(page.getByText("Artifact revision saved.")).toBeVisible();
  await expect(prdCard.getByText("proposed / latest v2 / approved v0")).toBeVisible();
  await expect(prdCard.locator(".markdown-preview")).toContainText("Second draft with a measurable success metric.");
  await prdCard.getByLabel("What should change?").fill("Looks good with the metric.");
  await prdCard.getByRole("button", { name: "Approve with notes" }).click();
  await expect(page.getByText("Artifact approved.")).toBeVisible();
  await expect(prdCard.getByText("approved_with_notes / latest v2 / approved v2")).toBeVisible();
  await expect(prdCard.getByLabel("What should change?")).toBeEnabled();
});

test("project setup actions execute from the UI after creation", async ({ page }, testInfo) => {
  const projectRoot = path.normalize(testInfo.outputPath("execution-project"));

  await page.goto("/");
  await page.getByRole("button", { name: "New Project" }).click();
  await page.getByLabel("Name").fill("UI Execution Project");
  await page.getByLabel("Project root").fill(projectRoot);
  await page.getByPlaceholder("What do you want this project to become?").fill("A project that exercises artifact approval, materialization, and fake worker execution.");
  await page.getByRole("button", { name: "Create project" }).click();
  await expect(page.getByText("UI Execution Project was created and selected.")).toBeVisible({ timeout: 30_000 });

  await page.getByRole("button", { name: "Approve all generated plan" }).click();
  await expect(page.getByText("4 artifact(s) approved. Next: create an implementation task.")).toBeVisible();
  await expect(page.locator('button:has-text("Approve latest"):not(:disabled)')).toHaveCount(0);
  await page.getByRole("button", { name: "Create implementation task" }).click();
  await expect(page.getByText("1 task(s) materialized and queued. Next: run a worker.")).toBeVisible();
  await expect(page.getByRole("heading", { name: "Implementation queued" })).toBeVisible();
  await expect(page.getByText("TASK-001 / ready")).toBeVisible();
  await expect(page.locator(".artifact-summary-panel")).toContainText("4 approved / 4 total");
  await expect(page.locator(".artifact-summary-panel").getByRole("button", { name: "View artifacts" })).toBeVisible();

  let finishWork!: () => void;
  await page.route("**/work/start", async (route) => {
    await new Promise<void>((resolve) => {
      finishWork = resolve;
    });
    await route.fallback();
  }, { times: 1 });
  const workResponse = page.waitForResponse((response) => response.url().includes("/work/start") && response.request().method() === "POST");
  await page.getByRole("button", { name: "Run simulation" }).click();
  await expect(page.locator(".ready-run-panel").getByRole("status")).toContainText("Running simulation");
  finishWork();
  await workResponse;
  await expect(page.getByText("Fake worker finished with 1 execution item(s).")).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText("TASK-001 / ready_for_human_review")).toBeVisible();
  await expect(page.getByRole("heading", { name: "Review implementation" })).toBeVisible();
  await page.getByRole("button", { name: "Review evidence" }).click();
  await expect(page.locator("#task-artifacts-viewer").getByRole("heading", { name: "Diff & Artifacts" })).toBeVisible();
  await expect(page.locator("#task-artifacts-viewer")).toContainText("diff.patch");
  await expect(page.getByRole("button", { name: "Approve implementation" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Approve for merge" })).toHaveCount(0);
  await page.getByRole("button", { name: "Approve implementation" }).click();
  await expect(page.getByRole("button", { name: "Approve for merge" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Approve implementation" })).toHaveCount(0);
  await page.getByRole("button", { name: "Open implementation evidence" }).click();
  await expect(page.locator("#task-artifacts-viewer")).toContainText("fake-verification");
  await expect(page.locator(".error-banner")).toHaveCount(0);
});

test("workflow buttons stay exclusive and actionable across implementation approval states", async ({ page }, testInfo) => {
  await moveToQueuedImplementation(page, testInfo, "UI Button Matrix Project");

  await expect(page.getByRole("button", { name: "Build with Codex" })).toHaveCount(1);
  await expect(page.getByRole("button", { name: "Run simulation" })).toHaveCount(1);
  await expect(page.getByRole("button", { name: "Create implementation task" })).toHaveCount(0);

  let capturedAdapter = "";
  await page.route("**/work/start", async (route) => {
    const body = route.request().postDataJSON() as { adapter?: string };
    capturedAdapter = body.adapter ?? "";
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ items: [] })
    });
  }, { times: 1 });
  await page.getByRole("button", { name: "Build with Codex" }).click();
  await expect(page.getByText("Codex worker checked the queue, but no ready execution work was found.")).toBeVisible();
  expect(capturedAdapter).toBe("real-codex");
  await page.unroute("**/work/start");
  await expect(page.getByRole("button", { name: "Run simulation" })).toHaveCount(1);

  await runSimulationToReview(page);
  await expect(page.getByRole("button", { name: "Approve implementation" })).toHaveCount(1);
  await expect(page.getByRole("button", { name: "Approve for merge" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Merge to main" })).toHaveCount(0);

  await page.getByRole("button", { name: "Review evidence" }).click();
  await expect(page.locator("#task-artifacts-viewer")).toContainText("Implementation diff");
  await expect(page.getByRole("button", { name: "Review evidence" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Approve implementation" })).toHaveCount(1);
  await page.getByRole("button", { name: "Approve implementation" }).click();

  await expect(page.getByRole("button", { name: "Approve implementation" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Approve for merge" })).toHaveCount(1);
  await page.getByRole("button", { name: "Approve for merge" }).click();
  await expect(page.locator("small").filter({ hasText: "TASK-001 / queued_for_merge" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Approve for merge" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Merge to main" })).toHaveCount(1);

  await page.getByRole("button", { name: "Merge to main" }).click();
  await expect(page.getByText("Merge succeeded for TASK-001.")).toBeVisible({ timeout: 30_000 });
  await expect(page.locator("small").filter({ hasText: "TASK-001 / merged" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Merge to main" })).toHaveCount(0);
  await expect(page.locator(".error-banner")).toHaveCount(0);
});

test("review rejection button does not expose merge actions from the wrong state", async ({ page }, testInfo) => {
  await moveToQueuedImplementation(page, testInfo, "UI Rejection Project");
  await runSimulationToReview(page);

  await page.getByRole("button", { name: "Review evidence" }).click();
  await expect(page.getByRole("button", { name: "Request changes" })).toHaveCount(1);
  await page.getByRole("button", { name: "Request changes" }).click();

  await expect(page.locator("small").filter({ hasText: "TASK-001 / needs_decision" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Approve implementation" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Approve for merge" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Merge to main" })).toHaveCount(0);
  await expect(page.locator(".error-banner")).toHaveCount(0);
});

test("artifact review buttons toggle without creating duplicate workflow actions", async ({ page }, testInfo) => {
  await createProjectFromUI(
    page,
    testInfo,
    "UI Artifact Buttons Project",
    "A tiny app used to exercise artifact review controls."
  );

  const prdCard = page.locator(".artifact-review-card", { hasText: "prd" });
  await expect(prdCard.getByRole("button", { name: "Review content" })).toBeVisible();
  await prdCard.getByRole("button", { name: "Review content" }).click();
  await expect(prdCard.getByRole("button", { name: "Hide content" })).toBeVisible();
  await prdCard.getByRole("button", { name: "Hide content" }).click();
  await expect(prdCard.getByRole("button", { name: "Review content" })).toBeVisible();

  await expect(page.getByRole("button", { name: "Approve all generated plan" })).toHaveCount(1);
  await prdCard.getByRole("button", { name: "Approve latest" }).click();
  await expect(page.getByText("Artifact approved.")).toBeVisible();
  await expect(prdCard.getByText("approved / latest v1 / approved v1")).toBeVisible();
  await expect(prdCard.getByRole("button", { name: "Approve latest" })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Create implementation task" })).toHaveCount(0);

  await page.getByRole("button", { name: "Approve all generated plan" }).click();
  await expect(page.getByRole("button", { name: "Approve all generated plan" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Create implementation task" })).toHaveCount(1);
  await page.getByRole("button", { name: "Create implementation task" }).click();

  const artifactsPanel = page.locator("section", { has: page.getByRole("heading", { name: "Artifacts", exact: true }) });
  await expect(artifactsPanel.getByRole("button", { name: "View artifacts" })).toBeVisible();
  await artifactsPanel.getByRole("button", { name: "View artifacts" }).click();
  await expect(artifactsPanel.getByRole("button", { name: "Hide artifacts" })).toBeVisible();
  await artifactsPanel.getByRole("button", { name: "Hide artifacts" }).click();
  await expect(artifactsPanel.getByRole("button", { name: "View artifacts" })).toBeVisible();
  await expect(page.locator(".error-banner")).toHaveCount(0);
});

test("dashboard has no critical or serious automated accessibility violations", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "New Project" }).click();

  const results = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
    .analyze();

  const blockingViolations = results.violations.filter((violation) => violation.impact === "critical" || violation.impact === "serious");
  expect(blockingViolations).toEqual([]);
});
