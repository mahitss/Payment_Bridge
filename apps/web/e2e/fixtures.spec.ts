import { test, expect } from "@playwright/test";

test.describe("C. Synthetic Fixture Integrity and Offline Inspection", () => {
  test.beforeEach(async ({ page }) => {
    // Log in as operator
    await page.goto("/login");
    await page.fill("#email", "operator@claimbridge.internal");
    await page.fill("#password", "OperatorPass123!");
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/app/);
  });

  test("seeded synthetic fixture DEMO-W9821 is listed with SIMULATED badge", async ({
    page,
  }) => {
    await page.goto("/app/payouts");

    // Check table for DEMO-W9821
    const fixtureRow = page.locator("tr", { hasText: "DEMO-W9821" });
    await expect(fixtureRow).toBeVisible({ timeout: 10000 });

    // Must have SIMULATED badge
    await expect(fixtureRow.locator("text=SIMULATED")).toBeVisible();
  });

  test("synthetic fixture detail page displays prominent provenance banner and disables external controls", async ({
    page,
  }) => {
    await page.goto("/app/payouts");

    // Click into DEMO-W9821
    const fixtureLink = page.locator('a:has-text("DEMO-W9821")');
    await expect(fixtureLink).toBeVisible({ timeout: 10000 });
    await fixtureLink.click();
    await expect(page).toHaveURL(/\/app\/payouts\/.+/, { timeout: 15000 });

    // Verify prominent provenance alert banner
    const banner = page.locator("text=SIMULATED DEMO DATA — NOT A LIVE PAYPAL TRANSACTION");
    await expect(banner).toBeVisible({ timeout: 10000 });

    // Verify explanation text
    await expect(
      page.locator("text=This case and its operations are local evaluation fixtures.")
    ).toBeVisible();

    // Verify SIMULATED FIXTURE badge next to reference
    await expect(page.locator("text=SIMULATED FIXTURE")).toBeVisible();

    // Verify external controls are disabled for synthetic fixtures
    const syncButton = page.getByRole("button", { name: "Sync with PayPal" });
    if (await syncButton.isVisible()) {
      await expect(syncButton).toBeDisabled();
      await expect(syncButton).toHaveAttribute("title", "Disabled: Synthetic Demo Fixture");
    }

    const submitButton = page.getByRole("button", { name: "Submit to PayPal Sandbox" });
    if (await submitButton.isVisible()) {
      await expect(submitButton).toBeDisabled();
      await expect(submitButton).toHaveAttribute("title", "Disabled: Synthetic Demo Fixture");
    }

    const cancelButton = page.getByRole("button", { name: "Cancel Payout" });
    if (await cancelButton.isVisible()) {
      await expect(cancelButton).toBeDisabled();
      await expect(cancelButton).toHaveAttribute("title", "Disabled: Synthetic Demo Fixture");
    }
  });

  test("persisted synthetic AI investigation report is viewable offline without Ollama", async ({
    page,
  }) => {
    await page.goto("/app/payouts");
    const fixtureLink = page.locator('a:has-text("DEMO-W9821")');
    await expect(fixtureLink).toBeVisible({ timeout: 10000 });
    await fixtureLink.click();
    await expect(page).toHaveURL(/\/app\/payouts\/.+/, { timeout: 15000 });

    // Investigation engine card should have the Simulated Evaluation Fixture indicator
    const aiEngine = page.locator("section", { hasText: "AI Payout Investigation Engine" });
    await expect(aiEngine).toBeVisible({ timeout: 10000 });
    await expect(aiEngine.locator("text=Simulated Evaluation Fixture")).toBeVisible();

    // Verify pre-seeded investigation details are displayed
    // The seeder seeds a synthetic investigation with finding "Simulated scenario: recipient has not claimed the transfer within the observation window."
    const preSeededFinding = page.locator("text=Simulated scenario: recipient has not claimed the transfer");
    await expect(preSeededFinding).toBeVisible();

    // Evidence citations must be visible and properly grounded
    await expect(page.locator("text=Evidence:")).toBeVisible();
  });
});
