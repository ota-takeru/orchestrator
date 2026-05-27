# UI Quality Workflow

この文書は、React UI作業を速く、壊れにくく進めるための運用ガイドです。製品仕様ではなく、Codexや人間がUI実装を確認するときの作業手順として扱います。

確認日: 2026-05-27

参照:

- Storybook accessibility addon: https://storybook.js.org/docs/writing-tests/accessibility-testing
- Storybook Vitest addon overview: https://storybook.js.org/docs/writing-tests/integrations/vitest-addon
- Playwright best practices: https://playwright.dev/docs/best-practices
- Playwright MCP: https://playwright.dev/docs/getting-started-mcp
- Playwright MCP capabilities: https://playwright.dev/mcp/capabilities
- Chrome DevTools MCP for agents: https://developer.chrome.com/docs/devtools/agents/get-started
- Chrome DevTools MCP CLI: https://github.com/ChromeDevTools/chrome-devtools-mcp/blob/main/docs/cli.md
- axe-core Playwright package: https://www.npmjs.com/package/@axe-core/playwright

## Installed Tooling

UI package devDependencies include:

- Storybook: `storybook`, `@storybook/react-vite`, `@storybook/addon-a11y`, `@storybook/test-runner`
- Playwright: `@playwright/test`
- Accessibility engine: `axe-core`, `@axe-core/playwright`
- MCP servers: `@playwright/mcp`, `chrome-devtools-mcp`
- Local orchestration helpers: `concurrently`, `wait-on`

`ui/pnpm-workspace.yaml` explicitly allows the build scripts required by the installed native helper packages.

## Commands

Run from the repository root unless noted.

- Type/lint baseline: `corepack pnpm --dir ui test`
- UI build: `corepack pnpm --dir ui build`
- Interactive component workbench: `corepack pnpm --dir ui storybook`
- Static Storybook build: `corepack pnpm --dir ui storybook:build`
- Storybook interaction/a11y runner: `corepack pnpm --dir ui storybook:test`
- E2E and page-level a11y: `corepack pnpm --dir ui e2e`
- Headed E2E debug: `corepack pnpm --dir ui e2e:headed`
- Playwright UI mode: `corepack pnpm --dir ui e2e:ui`
- Playwright HTML report: `corepack pnpm --dir ui e2e:report`
- Playwright MCP server: `corepack pnpm --dir ui mcp:playwright`
- Chrome DevTools MCP server: `corepack pnpm --dir ui mcp:devtools`

## Role Split

Use Storybook first when changing a component, panel, visual state, loading state, or empty/error state. Stories should isolate the UI from the live Orchestrator API by using small typed fixtures or fetch mocks.

Use Playwright E2E when behavior crosses API, routing, app boot, browser layout, or project creation workflow boundaries. Prefer tests that exercise one human workflow rather than screenshots of every panel.

Use axe-core in two places:

- Storybook addon for fast component-level feedback while designing.
- Playwright `@axe-core/playwright` for page-level checks after the app is booted through `devos serve --ui`.

Use Playwright MCP for AI-assisted browser operations that are mostly user-flow driven: navigate, click, fill, inspect accessibility snapshots, and produce locators.

Use Chrome DevTools MCP when debugging browser internals: console errors, network requests, performance traces, storage, rendering, or situations where DevTools-level evidence is more useful than DOM-level interaction.

## Testing Practices

Playwright tests should use user-facing locators first:

- Prefer `getByRole`, `getByLabel`, `getByText`, `getByPlaceholder`, and `getByAltText`.
- Use `data-testid` only for controls with no stable accessible name.
- Avoid CSS selector chains and XPath unless debugging a temporary issue.
- Use web-first assertions such as `await expect(locator).toBeVisible()` and `await expect(locator).toHaveValue(...)`.
- Avoid fixed sleeps. Wait on visible user state, URL, network outcome, or a locator assertion.
- Keep traces at `on-first-retry`; turn on full traces locally only during investigation.

Accessibility tests should start strict for new isolated stories and pragmatic for existing app-wide stories:

- New component stories should use `parameters.a11y.test = "error"` once their baseline is clean.
- Existing full-app stories may use `"todo"` while violations are triaged.
- E2E page checks should fail on critical/serious WCAG violations and leave moderate/minor issues as review items unless a task specifically targets accessibility cleanup.

## MCP Configuration

For MCP-capable clients, prefer using the repository-pinned package versions through Corepack instead of `npx @latest`.

Example:

```json
{
  "mcpServers": {
    "playwright": {
      "command": "corepack",
      "args": [
        "pnpm",
        "--dir",
        "ui",
        "exec",
        "playwright-mcp",
        "--browser=chrome",
        "--caps=devtools",
        "--isolated",
        "--viewport-size=1365x900"
      ]
    },
    "chrome-devtools": {
      "command": "corepack",
      "args": [
        "pnpm",
        "--dir",
        "ui",
        "exec",
        "chrome-devtools-mcp",
        "--isolated",
        "--viewport=1365x900",
        "--no-usage-statistics"
      ]
    }
  }
}
```

Security notes:

- Keep MCP browser sessions isolated by default.
- Do not load `.env` or secret-bearing local sessions into MCP browsers.
- Use Playwright MCP unsafe code execution only with trusted prompts and trusted pages.
- Use Chrome DevTools MCP with `--no-usage-statistics` in this project by default.

## Recommended UI Work Loop

1. Start with `corepack pnpm --dir ui storybook` and create or update a story for the state being changed.
2. Check layout manually in Storybook and run the a11y panel.
3. Add or update a Playwright test only when the behavior spans real app boot, API, or workflow state.
4. Run `corepack pnpm --dir ui test`, `corepack pnpm --dir ui storybook:build`, and `corepack pnpm --dir ui e2e` for UI-sensitive work.
5. For hard failures, use `corepack pnpm --dir ui e2e:ui` or `e2e:headed`, then inspect the Playwright report.
6. Use Playwright MCP for fast agent-driven interaction checks. Switch to Chrome DevTools MCP for console, network, and performance evidence.

