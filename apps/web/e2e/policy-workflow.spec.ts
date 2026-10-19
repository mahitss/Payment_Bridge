import { test, expect } from "@playwright/test";

test.describe.serial("D. Policy Enforcement and Separation of Duties Workflow", () => {
  const caseRef = `TEST-E2E-${Date.now()}`;

  test("operator creates $350 case requiring dual approval, self-approval is blocked", async ({
    page,
  }) => {
    // 1. Log in as operator
    await page.goto("/login");
    await page.fill("#email", "operator@claimbridge.internal");
    await page.fill("#password", "OperatorPass123!");
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/app/);

    // 2. Navigate to New Payout Case
    await page.goto("/app/payouts/new");
    await expect(page.locator("h1")).toContainText("Create Payout Settlement Case");

    // 3. Fill in case form with $350.00 (which exceeds the $250.00 dual-custody threshold)
    await page.fill('input[placeholder*="Auto-generated"]', caseRef);
    await page.fill('input[placeholder*="Policy #9924-A"]', "E2E Property Damage Adjudication");
    await page.fill('input[placeholder*="sandbox-claimant"]', "claimant-e2e@example.com");
    await page.fill('input[placeholder*="Sarah Connor"]', "Alice Walker");
    await page.fill('input[placeholder="150.00"]', "350.00");

    // Dual-approval badge should reflect true
    await expect(page.locator("text=DUAL-CUSTODY MANDATED")).toBeVisible();

    // 4. Submit form
    await page.click('button[type="submit"]:has-text("Create Payout Case")');

    // 5. Should redirect to case detail page
    await expect(page).toHaveURL(/\/app\/payouts\//, { timeout: 10000 });
    await expect(page.locator("h1")).toContainText(caseRef);

    // 6. Verify case amount is $350.00 and dual-custody is required
    await expect(page.locator("text=$350.00")).toBeVisible();
    await expect(page.locator("text=Dual-Custody Required")).toBeVisible();

    // 7. Initiate payout operation to enter dual-custody approval flow
    const initiateBtn = page.getByRole("button", {
      name: /Initiate Sandbox Payout Operation/i,
    });
    await expect(initiateBtn).toBeVisible({ timeout: 5000 });
    await initiateBtn.click();

    // 8. Verify self-approval is strictly BLOCKED for the operator
    const selfApprovalText = page.locator("text=Self-approval blocked by policy");
    await expect(selfApprovalText).toBeVisible({ timeout: 10000 });

    const approveButton = page.getByRole("button", {
      name: /Approve Operation/i,
    });
    if (await approveButton.isVisible()) {
      await expect(approveButton).toBeDisabled();
    }
  });

  test("distinct compliance officer can approve the operation, without auto-submitting", async ({
    page,
  }) => {
    // 1. Log in as officer
    await page.goto("/login");
    await page.fill("#email", "officer@claimbridge.internal");
    await page.fill("#password", "OfficerPass123!");
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/app/);

    // 2. Navigate to payouts list and find our created case
    await page.goto("/app/payouts");
    const caseLink = page.locator(`a:has-text("${caseRef}")`);
    await expect(caseLink).toBeVisible({ timeout: 10000 });
    await caseLink.click();

    await expect(page.locator("h1")).toContainText(caseRef);

    // 3. Officer is NOT the initiator, so Approve button should be enabled
    const approveButton = page.getByRole("button", {
      name: /Approve Operation \(Distinct Officer\)/i,
    });
    await expect(approveButton).toBeVisible();
    await expect(approveButton).toBeEnabled();

    // 4. Click approve to open confirmation modal
    await approveButton.click();

    // 5. Confirm in modal
    const confirmModal = page.locator("h3:has-text('Authorize Dual-Custody Approval')");
    await expect(confirmModal).toBeVisible();
    await page.getByRole("button", { name: "Confirm Approval" }).click();

    // 6. Verify success banner
    await expect(
      page.locator("text=Dual-custody approval recorded successfully.")
    ).toBeVisible({ timeout: 10000 });

    // 7. Verify operation is APPROVED and NOT automatically submitted to PayPal
    await expect(page.locator("text=Approved by officer")).toBeVisible({ timeout: 10000 });
    await expect(
      page.getByRole("button", { name: /Submit to PayPal Sandbox/i })
    ).toBeVisible();
    await expect(page.locator("text=Internal: SUBMITTED")).not.toBeVisible();
    await expect(page.locator("text=Internal: DRAFT")).toBeVisible();
  });

  test("requesting AI investigation when Ollama is offline returns honest failure without corrupting state", async ({
    page,
  }) => {
    // Log in as officer
    await page.goto("/login");
    await page.fill("#email", "officer@claimbridge.internal");
    await page.fill("#password", "OfficerPass123!");
    await page.click('button[type="submit"]');

    // Navigate to created case
    await page.goto("/app/payouts");
    const caseLink = page.locator(`a:has-text("${caseRef}")`);
    await expect(caseLink).toBeVisible({ timeout: 10000 });
    await caseLink.click();

    // Wait for case detail page to load
    await expect(page.locator("h1")).toContainText(caseRef);

    // Click "Run Investigation"
    const runBtn = page.getByRole("button", { name: /Run Investigation/i });
    await expect(runBtn).toBeVisible({ timeout: 10000 });
    await runBtn.click();

    // If Ollama is offline in this environment, it must report honest unavailable state
    // (or if mock is present, it will complete - either way, case state must remain DRAFT)
    const offlineMsg = page.locator(
      "text=Investigation assistant is currently offline or unreachable"
    );
    const successMsg = page.locator("text=Investigation completed");

    await expect(offlineMsg.or(successMsg)).toBeVisible({ timeout: 15000 });

    // Core case status must NOT be corrupted
    await expect(page.locator("text=Internal: DRAFT")).toBeVisible();
  });
});
