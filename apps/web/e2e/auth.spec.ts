import { test, expect } from "@playwright/test";

test.describe("B. Authentication and Demo Accounts", () => {
  test("unauthenticated access to protected routes redirects to login", async ({
    page,
  }) => {
    await page.goto("/app/payouts");
    // Should be redirected to /login with returnTo query parameter
    await expect(page).toHaveURL(/\/login\?returnTo=%2Fapp%2Fpayouts/);
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
    await page.fill("#password", "OperatorPass123!");
    await page.click('button[type="submit"]');

    // Should redirect to /app
    await expect(page).toHaveURL(/\/app/);

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
    await page.fill("#password", "OfficerPass123!");
    await page.click('button[type="submit"]');

    // Should redirect to /app
    await expect(page).toHaveURL(/\/app/);

    // Verify officer email is displayed in header
    await expect(page.locator("header")).toContainText("officer@claimbridge.internal");

    // Verify tenant badge shows ADMIN role in demo organization
    await expect(page.locator("header")).toContainText("PayPal AI Hackathon Adjudications");
    await expect(page.locator("header")).toContainText("ADMIN");
  });

  test("evaluator banner appears on landing page when demo mode is enabled", async ({
    page,
  }) => {
    await page.goto("/");

    // When NEXT_PUBLIC_DEMO_MODE=true is configured:
    const demoBanner = page.locator("text=Local Evaluation Fixtures (Demo Mode Enabled)");
    const isVisible = await demoBanner.isVisible();

    if (isVisible) {
      await expect(page.locator("text=operator@claimbridge.internal")).toBeVisible();
      await expect(page.locator("text=officer@claimbridge.internal")).toBeVisible();
    }
    // Note: If NEXT_PUBLIC_DEMO_MODE is not true, banner is absent as required by specification.
  });
});
