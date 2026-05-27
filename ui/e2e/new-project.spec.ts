import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";
import path from "node:path";

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
  const prdPreview = page.locator(".markdown-preview", { hasText: "PRD" });
  await expect(prdPreview).toBeVisible();
  await expect(prdPreview.getByRole("heading", { name: "PRD" })).toBeVisible();
  await expect(prdPreview).toContainText("A project created by the UI end-to-end check.");
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

  for (let i = 0; i < 4; i += 1) {
    const approveButton = page.locator('button:has-text("Approve latest"):not(:disabled)').first();
    await expect(approveButton).toBeVisible();
    await approveButton.click();
    await expect(page.getByText("Artifact approved.")).toBeVisible();
  }

  await expect(page.locator('button:has-text("Approve latest"):not(:disabled)')).toHaveCount(0);
  await page.getByRole("button", { name: "Materialize tasks" }).click();
  await expect(page.getByText("1 task(s) materialized and queued. Next: run a worker.")).toBeVisible();
  await expect(page.getByRole("heading", { name: "Ready to run" })).toBeVisible();
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
  await page.getByRole("button", { name: "Run fake worker" }).click();
  await expect(page.locator(".ready-run-panel").getByRole("status")).toContainText("Running fake worker");
  finishWork();
  await workResponse;
  await expect(page.getByText("Fake worker finished with 1 execution item(s).")).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText("TASK-001 / ready_for_human_review")).toBeVisible();
  await expect(page.getByRole("button", { name: "Run fake worker" })).toBeDisabled();
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
