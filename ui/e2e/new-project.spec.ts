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
  const prdPreview = page.locator(".artifact-content-review", { hasText: "# PRD" });
  await expect(prdPreview).toBeVisible();
  await expect(prdPreview).toContainText("A project created by the UI end-to-end check.");
  await expect(page.locator(".error-banner")).toHaveCount(0);
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
  await expect(page.getByText("1 task(s) materialized and queued.")).toBeVisible();
  await expect(page.getByText("TASK-001 / ready")).toBeVisible();

  await page.getByRole("button", { name: "Run fake worker" }).click();
  await expect(page.getByText("Fake worker finished with 1 execution item(s).")).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText("TASK-001 / ready_for_human_review")).toBeVisible();
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
