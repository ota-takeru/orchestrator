import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

test("new project form defaults the name and project root", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "New Project" }).click();

  await expect(page.getByLabel("Name")).toHaveValue("New Project");
  await expect(page.getByLabel("Project root")).toHaveValue(/new-project/i);

  await page.getByLabel("Name").fill("Daily Notes");
  await expect(page.getByLabel("Project root")).toHaveValue(/daily-notes/i);
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
