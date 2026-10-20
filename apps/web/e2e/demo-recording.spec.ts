import { test, expect } from "@playwright/test";
import * as fs from "fs";
import * as path from "path";

test.describe.serial("G. Deterministic Offline Walkthrough and Demo Recording", () => {
  test("records complete offline evaluator demo walkthrough (1080p)", async ({
    browser,
  }, testInfo) => {
    test.setTimeout(90000);

    const outputDir = path.resolve(process.cwd(), "test-results", "demo-recording");
    fs.mkdirSync(outputDir, { recursive: true });

    // Launch controlled 1920x1080 browser context with recording explicitly enabled
    const context = await browser.newContext({
      viewport: { width: 1920, height: 1080 },
      recordVideo: {
        dir: outputDir,
        size: { width: 1920, height: 1080 },
      },
      ignoreHTTPSErrors: true,
    });

    const page = await context.newPage();

    try {
      // =======================================================================
      // Scene A — Landing Page & Truthful Diagnostic Probe
      // =======================================================================
      await page.goto("/");
      await expect(page).toHaveTitle(/ClaimBridge/i);
      await expect(page.locator("h1")).toContainText("ClaimBridge");

      // Verify honest Sandbox Prototype branding and architectural value proposition
      await expect(page.locator("text=Sandbox Prototype")).toBeVisible();
      await expect(
        page.locator("text=using provider status records, evidence-grounded AI explanations")
      ).toBeVisible();

      // Pacing pause for viewer orientation
      await page.waitForTimeout(1400);

      // Trigger diagnostic probe
      const probeButton = page.getByRole("button", { name: /Probe Gateway/i });
      await expect(probeButton).toBeVisible();
      await probeButton.click();

      // Verify diagnostic probe card and truthful status partitioning
      const diagnosticCard = page.locator("text=System Diagnostic Probe");
      await expect(diagnosticCard).toBeVisible({ timeout: 10000 });
      await expect(page.locator("text=API Gateway:").locator("..")).toBeVisible();
      await expect(page.locator("text=PayPal Sandbox:").locator("..")).toBeVisible();

      // Ensure no false success or credential leaks
      await expect(page.locator("text=access_token")).not.toBeVisible();
      await expect(page.locator("text=Gateway Live & Sandbox Connected")).not.toBeVisible();

      // Pacing pause to read probe state
      await page.waitForTimeout(1600);

      // =======================================================================
      // Scene B — Operator Authentication & Payout Intake
      // =======================================================================
      await page.goto("/login");
      await expect(page.locator("h1")).toContainText("Sign in to ClaimBridge");

      // Sign in as operator
      await page.fill("#email", "operator@claimbridge.internal");
      await page.fill("#password", "OperatorPass123!");
      await page.waitForTimeout(600);
      await page.click('button[type="submit"]');

      // Await entry into workspace
      await expect(page).toHaveURL(/\/app/);
      await expect(page.locator("header")).toContainText("operator@claimbridge.internal");
      await page.waitForTimeout(1000);

      // Navigate to New Payout Case form
      await page.goto("/app/payouts/new");
      await expect(page.locator("h1")).toContainText("Create Payout Settlement Case");

      // Fill out case form with a unique $350.00 case requiring dual custody
      const uniqueRef = `DEMO-EVAL-${Date.now().toString().slice(-6)}`;
      await page.fill("#case-reference", uniqueRef);
      await page.fill("#settlement-title", "Commercial Property Damage Adjudication #8921");
      await page.fill("#recipient-email", "claimant-demo@sandbox.example.com");
      await page.fill("#recipient-name", "Alice Walker");
      await page.fill("#payout-amount", "350.00");
      await page.fill(
        "#case-description",
        "Commercial property flood claim submitted for dual-custody settlement adjudication."
      );

      // Verify policy rule triggers: dual custody mandated for > $250.00
      await expect(page.locator("text=DUAL-CUSTODY MANDATED")).toBeVisible();
      await page.waitForTimeout(1200);

      // Submit case
      await page.click('button[type="submit"]:has-text("Create Payout Case")');

      // Assert case created and redirected
      await expect(page).toHaveURL(/\/app\/payouts\//, { timeout: 10000 });
      await expect(page.locator("h1")).toContainText(uniqueRef);
      await expect(page.locator("text=$350.00")).toBeVisible();
      await expect(page.locator("text=Dual-Custody Required")).toBeVisible();
      await page.waitForTimeout(1500);

      // =======================================================================
      // Scene C — Dual-Custody Approval & Separation of Duties
      // =======================================================================
      // Initiate payout operation to enter dual-custody approval flow
      const initiateBtn = page.getByRole("button", {
        name: /Initiate Sandbox Payout Operation/i,
      });
      await expect(initiateBtn).toBeVisible({ timeout: 5000 });
      await initiateBtn.click();

      // Verify self-approval is strictly blocked for the initiating operator
      const selfApprovalText = page.locator("text=Self-approval blocked by policy");
      await expect(selfApprovalText).toBeVisible({ timeout: 10000 });

      const approveButton = page.getByRole("button", { name: /Approve Operation/i });
      if (await approveButton.isVisible()) {
        await expect(approveButton).toBeDisabled();
      }
      await page.waitForTimeout(1500);

      // Sign out using genuine header button
      const signOutBtn = page.getByRole("button", { name: "Sign Out of ClaimBridge" });
      await expect(signOutBtn).toBeVisible();
      await signOutBtn.click();
      await expect(page).toHaveURL(/\/login/);
      await page.waitForTimeout(800);

      // Sign in as distinct compliance officer
      await page.fill("#email", "officer@claimbridge.internal");
      await page.fill("#password", "OfficerPass123!");
      await page.click('button[type="submit"]');
      await expect(page).toHaveURL(/\/app/);
      await expect(page.locator("header")).toContainText("officer@claimbridge.internal");

      // Navigate to Payouts list and open the newly created case
      await page.goto("/app/payouts");
      const caseLink = page.locator(`a:has-text("${uniqueRef}")`);
      await expect(caseLink).toBeVisible({ timeout: 10000 });
      await caseLink.click();
      await expect(page.locator("h1")).toContainText(uniqueRef);

      // Officer approval button is enabled
      const officerApproveBtn = page.getByRole("button", {
        name: /Approve Operation \(Distinct Officer\)/i,
      });
      await expect(officerApproveBtn).toBeVisible();
      await expect(officerApproveBtn).toBeEnabled();
      await page.waitForTimeout(1000);

      // Click to open confirmation modal
      await officerApproveBtn.click();
      const confirmModal = page.locator("h3:has-text('Authorize Dual-Custody Approval')");
      await expect(confirmModal).toBeVisible();
      await page.waitForTimeout(600);
      await page.getByRole("button", { name: "Confirm Approval" }).click();

      // Verify dual-custody approval accepted
      await expect(
        page.locator("text=Dual-custody approval recorded successfully.")
      ).toBeVisible({ timeout: 10000 });
      await expect(page.locator("text=Approved by officer")).toBeVisible({ timeout: 10000 });

      // Verify operation is APPROVED but NOT automatically submitted to PayPal
      await expect(
        page.getByRole("button", { name: /Submit to PayPal Sandbox/i })
      ).toBeVisible();
      await expect(page.locator("text=Internal: SUBMITTED")).not.toBeVisible();
      await expect(page.locator("text=Internal: DRAFT")).toBeVisible();
      await page.waitForTimeout(1500);

      // =======================================================================
      // Scene D — Synthetic Fixture Provenance & Mutation Protection
      // =======================================================================
      await page.goto("/app/payouts");
      await expect(page.locator("h1")).toContainText("Payout Cases & Investigation");

      // Locate seeded fixture DEMO-W9821
      const fixtureRow = page.locator("tr", { hasText: "DEMO-W9821" });
      await expect(fixtureRow).toBeVisible({ timeout: 10000 });
      await expect(fixtureRow.locator("text=SIMULATED")).toBeVisible();
      await page.waitForTimeout(1000);

      // Open DEMO-W9821 detail page
      const fixtureLink = page.locator('a:has-text("DEMO-W9821")');
      await fixtureLink.click();
      await expect(page).toHaveURL(/\/app\/payouts\/.+/, { timeout: 10000 });

      // Verify prominent synthetic alert banner
      const banner = page.locator("text=SIMULATED DEMO DATA — NOT A LIVE PAYPAL TRANSACTION");
      await expect(banner.first()).toBeVisible({ timeout: 10000 });
      await expect(
        page.locator("text=This case and its operations are local evaluation fixtures.").first()
      ).toBeVisible();
      await expect(page.locator("text=SIMULATED FIXTURE").first()).toBeVisible();

      // Verify external mutation controls are safely disabled on synthetic fixtures
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
      await page.waitForTimeout(1500);

      // =======================================================================
      // Scene E & F — Offline Investigation Evidence & Final Honest State
      // =======================================================================
      // Scroll to AI Investigation card
      const aiEngine = page.locator("section", { hasText: "AI Payout Investigation Engine" });
      await expect(aiEngine.first()).toBeVisible({ timeout: 10000 });
      await aiEngine.first().scrollIntoViewIfNeeded();

      // Verify Simulated Evaluation Fixture indicator
      await expect(aiEngine.locator("text=Simulated Evaluation Fixture").first()).toBeVisible();

      // Verify pre-seeded synthetic findings
      const preSeededFinding = page.locator(
        "text=Simulated scenario: recipient has not claimed the transfer"
      );
      await expect(preSeededFinding.first()).toBeVisible();

      // Verify grounded citations
      await expect(page.locator("text=Evidence:").first()).toBeVisible();
      await expect(page.locator("text=SIMULATED-DEMO-BATCH-W9821").first()).toBeVisible();

      // Final pause on grounded evidence state
      await page.waitForTimeout(2000);
    } finally {
      // Close page and context to flush video to disk
      await page.close();
      await context.close();
    }

    // =======================================================================
    // Video Artifact Verification & Finalization
    // =======================================================================
    const video = page.video();
    expect(video).not.toBeNull();
    const videoPath = await video!.path();

    expect(fs.existsSync(videoPath), `Recorded video not found at ${videoPath}`).toBe(true);
    const stats = fs.statSync(videoPath);
    expect(stats.size, `Recorded video file is empty (${stats.size} bytes)`).toBeGreaterThan(10000);

    // Save as canonical video artifact
    const canonicalPath = path.join(outputDir, "claimbridge-demo-walkthrough.webm");
    fs.copyFileSync(videoPath, canonicalPath);

    expect(fs.existsSync(canonicalPath)).toBe(true);
    const canonicalStats = fs.statSync(canonicalPath);
    expect(canonicalStats.size).toBeGreaterThan(10000);

    // Attach to Playwright test report
    testInfo.attachments.push({
      name: "claimbridge-demo-walkthrough",
      path: canonicalPath,
      contentType: "video/webm",
    });

    console.log(
      `[Demo Recording] Successfully generated video artifact: ${canonicalPath} (${canonicalStats.size} bytes)`
    );
  });
});
