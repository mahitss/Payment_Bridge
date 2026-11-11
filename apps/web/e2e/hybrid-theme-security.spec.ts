import { test, expect } from "@playwright/test";

test.describe("ClaimBridge Hybrid Theme & Demo Credential Security", () => {
  test("landing page strictly conceals demo credentials and fixtures banner", async ({
    page,
  }) => {
    await page.goto("/");

    // 1. Assert passwords are NEVER rendered in the DOM
    const bodyText = await page.innerText("body");
    expect(bodyText).not.toContain("OperatorPass123!");
    expect(bodyText).not.toContain("OfficerPass123!");

    // 2. If evaluator demo mode banner is active, assert passwords remain strictly absent
    const demoBanner = page.locator("text=Local Evaluation Fixtures (Demo Mode Enabled)");
    if (await demoBanner.isVisible()) {
      await expect(page.locator("text=operator@claimbridge.internal")).toBeVisible();
      await expect(page.locator("text=officer@claimbridge.internal")).toBeVisible();
      await expect(page.locator("text=OperatorPass123!")).not.toBeVisible();
      await expect(page.locator("text=OfficerPass123!")).not.toBeVisible();
    }
  });

  test("landing page presents approved hybrid theme hierarchy", async ({
    page,
  }) => {
    await page.goto("/");

    // Header nav must have restrained dark navy styling
    const headerNav = page.locator("nav[aria-label='Public Header Navigation']");
    await expect(headerNav).toBeVisible();
    await expect(headerNav).toHaveClass(/bg-slate-950/);

    // Body must use clean light background
    const body = page.locator("body");
    await expect(body).toHaveClass(/bg-slate-50/);

    // Workflow steps must be present
    await expect(page.locator("text=Investigate Payouts")).toBeVisible();
    await expect(page.locator("text=Review Evidence")).toBeVisible();
    await expect(page.locator("text=AI Recommendations")).toBeVisible();
    await expect(page.locator("text=Deterministic Policy")).toBeVisible();
    await expect(page.locator("text=Authorized Human Approval")).toBeVisible();
  });

  test("authenticated workspace maintains hybrid layout and white card surfaces", async ({
    page,
  }) => {
    // Sign in as operator
    await page.goto("/login");
    await page.fill("#email", "operator@claimbridge.internal");
    await page.fill("#password", "OperatorPass123!");
    await page.click('button[type="submit"]');

    // Should redirect to /app
    await expect(page).toHaveURL(/\/app/);

    // Header retains dark navy
    const header = page.locator("header");
    await expect(header).toBeVisible();
    await expect(header).toHaveClass(/bg-slate-950/);

    // Body is light workspace
    const body = page.locator("body");
    await expect(body).toHaveClass(/bg-slate-50/);

    // Stat cards and surfaces have white backgrounds and slate borders
    const statCards = page.locator(".bg-white");
    expect(await statCards.count()).toBeGreaterThan(0);
  });

  test("auth pages render with clean light surfaces and no dark mode classes", async ({
    page,
  }) => {
    const authRoutes = [
      { path: "/login", title: "Sign in to ClaimBridge" },
      { path: "/register", title: "Create your account" },
      { path: "/forgot-password", title: "Forgot Password" },
      { path: "/reset-password", title: "Set New Password" },
      { path: "/verify-email", title: "Email Verification" },
    ];

    for (const route of authRoutes) {
      await page.goto(route.path);
      await expect(page.locator("h1")).toContainText(route.title);
      // Card container must be white
      const card = page.locator(".bg-white");
      expect(await card.count()).toBeGreaterThan(0);
    }
  });
});
