import { test, expect } from "@playwright/test";
import { DEMO_OPERATOR_PASSWORD, DEMO_OFFICER_PASSWORD } from "./test-credentials";

test.describe("B. Authentication and Demo Accounts", () => {
  test("unauthenticated access to protected routes redirects to login", async ({
    page,
  }) => {
    await page.goto("/app/payouts");
    // Should be redirected to /login with returnTo query parameter
    await expect(page).toHaveURL(/\/login\?returnTo=(%2F|\/)app(\/|%2F)payouts/, { timeout: 10000 });
    await expect(page.locator("h1")).toContainText("Sign in to ClaimBridge");
  });

  test("invalid credentials display clear error message", async ({ page }) => {
    await page.goto("/login");

    await page.fill("#email", "nonexistent@claimbridge.internal");
    await page.fill("#password", "WrongPassword123!");
    await page.click('button[type="submit"]');

    // Verify error is shown
    const errorAlert = page.locator("text=Invalid email or password");
    await expect(errorAlert).toBeVisible({ timeout: 5000 });
  });

  test("seeded operator can log in and access demo workspace", async ({ page }) => {
    await page.goto("/login");

    await page.fill("#email", "operator@claimbridge.internal");
    await page.fill("#password", DEMO_OPERATOR_PASSWORD);
    await page.click('button[type="submit"]');

    // Should redirect to /app
    await expect(page).toHaveURL(/\/app/, { timeout: 15000 });

    // Verify operator email is displayed in header
    await expect(page.locator("header")).toContainText("operator@claimbridge.internal");

    // Verify tenant badge
    await expect(page.locator("header")).toContainText("PayPal AI Hackathon Adjudications");
  });

  test("seeded compliance officer can log in and access demo workspace", async ({
    page,
  }) => {
    await page.goto("/login");

    await page.fill("#email", "officer@claimbridge.internal");
    await page.fill("#password", DEMO_OFFICER_PASSWORD);
    await page.click('button[type="submit"]');

    // Should redirect to /app
    await expect(page).toHaveURL(/\/app/, { timeout: 15000 });

    // Verify officer email is displayed in header
    await expect(page.locator("header")).toContainText("officer@claimbridge.internal");

    // Verify tenant badge shows demo organization
    await expect(page.locator("header")).toContainText("PayPal AI Hackathon Adjudications");
  });

  test("public landing page strictly conceals internal evaluator fixture banner and accounts", async ({
    page,
  }) => {
    await page.goto("/");

    // Evaluator fixture panel and account email display must be absent from public landing page
    const demoBanner = page.locator("text=Local Evaluation Fixtures (Demo Mode Enabled)");
    await expect(demoBanner).not.toBeVisible();
    await expect(page.locator("text=operator@claimbridge.internal")).not.toBeVisible();
    await expect(page.locator("text=officer@claimbridge.internal")).not.toBeVisible();
  });
});
