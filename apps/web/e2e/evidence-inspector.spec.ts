import { test, expect } from "@playwright/test";

test.describe("Mission 026: Grounded Evidence Inspector", () => {
  test.beforeEach(async ({ page }) => {
    // Log in as operator
    await page.goto("/login");
    await page.fill("#email", "operator@claimbridge.internal");
    await page.fill("#password", "OperatorPass123!");
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/app/, { timeout: 15000 });
  });

  test("1. Evidence chip click opens drawer with safe allowlisted fields, provenance, and REFERENCE VALIDATED badge", async ({
    page,
  }) => {
    // Mock investigation with findings citing DEMO-W9821
    await page.route("**/api/v1/payout-cases/*/investigations", async (route) => {
      if (route.request().method() === "GET") {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({
            investigations: [
              {
                id: "inv-inspect-001",
                payout_case_id: "demo-w9821-case-id",
                organization_id: "demo-org-id",
                requested_by_user_id: "operator-user-id",
                provider: "ollama-native",
                model: "qwen3.5:4b",
                prompt_version: "claimbridge-investigation-v1.0",
                status: "COMPLETED",
                summary: "Evidence grounded inspection test finding.",
                confidence: "HIGH",
                requires_human_review: false,
                observed_facts: ["Synthetic batch record verified"],
                findings: [
                  {
                    explanation: "Grounded finding referencing DEMO-W9821 case record",
                    evidence_references: ["demo-w9821-case-id"],
                  },
                ],
                recommended_next_steps: [
                  {
                    action: "CHECK_PROVIDER_STATUS",
                    reason: "Verify status",
                    requires_human_review: false,
                    deterministic_policy_permitted: true,
                  },
                ],
                missing_information: [],
                risk_flags: [],
                limitations: [],
                raw_response: {
                  transport: "ollama_native_chat",
                  inference_duration_ms: 45000,
                },
                created_at: new Date().toISOString(),
              },
            ],
          }),
        });
      } else {
        await route.continue();
      }
    });

    // Mock evidence resolution endpoint
    await page.route("**/api/v1/payout-cases/*/evidence/demo-w9821-case-id", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          reference_id: "demo-w9821-case-id",
          source_type: "PAYOUT_CASE",
          display_label: "Payout Case (DEMO-W9821)",
          fields: [
            { name: "Case Reference", value: "DEMO-W9821" },
            { name: "Title", value: "Synthetic Warranty Claim DEMO-W9821" },
            { name: "Status", value: "SUBMITTED" },
            { name: "Amount", value: "$125.00 USD" },
            { name: "Recipient (Masked)", value: "c***t@example.com" },
            { name: "Dual Approval Required", value: "false" },
            { name: "Risk Score", value: "0.15" },
            { name: "Created At", value: "2026-10-25T01:00:00Z" },
          ],
          recorded_at: "2026-10-25T01:00:00Z",
          is_synthetic_fixture: true,
          resolution_status: "RESOLVED",
        }),
      });
    });

    await page.goto("/app/payouts");
    const fixtureLink = page.locator('a:has-text("DEMO-W9821")');
    await expect(fixtureLink).toBeVisible({ timeout: 10000 });
    await fixtureLink.click();
    await expect(page).toHaveURL(/\/app\/payouts\/.+/, { timeout: 15000 });

    // Verify REFERENCE VALIDATED wording on finding card
    const validatedBadge = page.locator('[data-testid="finding-validated-badge"]').first();
    await expect(validatedBadge).toBeVisible();
    await expect(validatedBadge).toHaveText("REFERENCE VALIDATED");

    // Click evidence reference chip
    const evidenceChip = page.locator('[data-testid="evidence-chip-demo-w9821-case-id"]');
    await expect(evidenceChip).toBeVisible();
    await evidenceChip.click();

    // Inspector drawer should be open
    const drawer = page.locator('[data-testid="evidence-inspector-drawer"]');
    await expect(drawer).toBeVisible();

    // Check Reference ID pill
    const refPill = page.locator('[data-testid="evidence-inspector-ref-id"]');
    await expect(refPill).toBeVisible();
    await expect(refPill).toHaveText("demo-w9821-case-id");

    // Check Source Type badge
    const sourceType = page.locator('[data-testid="evidence-source-type"]');
    await expect(sourceType).toBeVisible();
    await expect(sourceType).toHaveText("PAYOUT_CASE");

    // Check Resolution status badge
    const resolutionBadge = page.locator('[data-testid="evidence-resolution-status"]');
    await expect(resolutionBadge).toBeVisible();
    await expect(resolutionBadge).toHaveText("REFERENCE VALIDATED");

    // Check Provenance badge
    const provenanceBadge = page.locator('[data-testid="evidence-provenance"]');
    await expect(provenanceBadge).toBeVisible();
    await expect(provenanceBadge).toHaveText("Simulated Demo Fixture (Local Data)");

    // Check Grounding vs Fact distinction caveat
    const caveat = page.locator('[data-testid="evidence-grounding-caveat"]');
    await expect(caveat).toBeVisible();
    await expect(caveat).toContainText("Citation Grounding Boundary");

    // Check Safe Fields
    const fieldsCard = page.locator('[data-testid="evidence-fields"]');
    await expect(fieldsCard).toBeVisible();
    await expect(fieldsCard).toContainText("Case Reference");
    await expect(fieldsCard).toContainText("DEMO-W9821");
    await expect(fieldsCard).toContainText("Recipient (Masked)");
    await expect(fieldsCard).toContainText("c***t@example.com");

    // Check close via close button and verify focus restoration to triggering chip
    const closeBtn = page.locator('[data-testid="evidence-inspector-close"]');
    await expect(closeBtn).toBeVisible();
    await closeBtn.click();
    await expect(drawer).not.toBeVisible();
    await expect(evidenceChip).toBeFocused();
  });

  test("2. Keyboard accessibility: Escape key closes the inspector drawer", async ({
    page,
  }) => {
    // Mock investigation
    await page.route("**/api/v1/payout-cases/*/investigations", async (route) => {
      if (route.request().method() === "GET") {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({
            investigations: [
              {
                id: "inv-inspect-002",
                payout_case_id: "demo-w9821-case-id",
                organization_id: "demo-org-id",
                requested_by_user_id: "operator-user-id",
                provider: "ollama-native",
                model: "qwen3.5:4b",
                prompt_version: "claimbridge-investigation-v1.0",
                status: "COMPLETED",
                summary: "Keyboard accessibility test finding.",
                confidence: "HIGH",
                requires_human_review: false,
                observed_facts: [],
                findings: [
                  {
                    explanation: "Keyboard test finding",
                    evidence_references: ["demo-w9821-case-id"],
                  },
                ],
                recommended_next_steps: [],
                missing_information: [],
                risk_flags: [],
                limitations: [],
                created_at: new Date().toISOString(),
              },
            ],
          }),
        });
      } else {
        await route.continue();
      }
    });

    await page.route("**/api/v1/payout-cases/*/evidence/*", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          reference_id: "demo-w9821-case-id",
          source_type: "PAYOUT_CASE",
          display_label: "Payout Case (DEMO-W9821)",
          fields: [{ name: "Case Reference", value: "DEMO-W9821" }],
          is_synthetic_fixture: true,
          resolution_status: "RESOLVED",
        }),
      });
    });

    await page.goto("/app/payouts");
    const fixtureLink = page.locator('a:has-text("DEMO-W9821")');
    await expect(fixtureLink).toBeVisible();
    await fixtureLink.click();
    await expect(page).toHaveURL(/\/app\/payouts\/.+/);

    const evidenceChip = page.locator('[data-testid="evidence-chip-demo-w9821-case-id"]');
    await expect(evidenceChip).toBeVisible();
    await evidenceChip.click();

    const drawer = page.locator('[data-testid="evidence-inspector-drawer"]');
    await expect(drawer).toBeVisible();

    // Press Escape to dismiss and verify focus restoration to triggering chip
    await page.keyboard.press("Escape");
    await expect(drawer).not.toBeVisible();
    await expect(evidenceChip).toBeFocused();
  });

  test("3. Unavailable or ungrounded citation displays clear error state without leaking information", async ({
    page,
  }) => {
    await page.route("**/api/v1/payout-cases/*/investigations", async (route) => {
      if (route.request().method() === "GET") {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({
            investigations: [
              {
                id: "inv-inspect-003",
                payout_case_id: "demo-w9821-case-id",
                organization_id: "demo-org-id",
                requested_by_user_id: "operator-user-id",
                provider: "ollama-native",
                model: "qwen3.5:4b",
                prompt_version: "claimbridge-investigation-v1.0",
                status: "COMPLETED",
                summary: "Unavailable citation test.",
                confidence: "LOW",
                requires_human_review: true,
                observed_facts: [],
                findings: [
                  {
                    explanation: "Finding citing unavailable reference",
                    evidence_references: ["non-existent-ref-id"],
                  },
                ],
                recommended_next_steps: [],
                missing_information: [],
                risk_flags: [],
                limitations: [],
                created_at: new Date().toISOString(),
              },
            ],
          }),
        });
      } else {
        await route.continue();
      }
    });

    // Mock 404 for non-existent-ref-id
    await page.route("**/api/v1/payout-cases/*/evidence/non-existent-ref-id", async (route) => {
      await route.fulfill({
        status: 404,
        contentType: "application/json",
        body: JSON.stringify({
          reference_id: "non-existent-ref-id",
          resolution_status: "UNAVAILABLE",
          error: "evidence reference not found",
          code: "EVIDENCE_NOT_FOUND",
        }),
      });
    });

    await page.goto("/app/payouts");
    const fixtureLink = page.locator('a:has-text("DEMO-W9821")');
    await expect(fixtureLink).toBeVisible();
    await fixtureLink.click();
    await expect(page).toHaveURL(/\/app\/payouts\/.+/);

    const badChip = page.locator('[data-testid="evidence-chip-non-existent-ref-id"]');
    await expect(badChip).toBeVisible();
    await badChip.click();

    // Drawer should show error state
    const drawer = page.locator('[data-testid="evidence-inspector-drawer"]');
    await expect(drawer).toBeVisible();

    const errorAlert = page.locator('[data-testid="evidence-error"]');
    await expect(errorAlert).toBeVisible();
    await expect(errorAlert).toContainText("Citation Unavailable or Inaccessible");
    await expect(errorAlert).toContainText("evidence reference not found");
  });

  test("4. Focus management: Opening places focus inside dialog, Tab and Shift+Tab trap focus within drawer", async ({
    page,
  }) => {
    await page.route("**/api/v1/payout-cases/*/investigations", async (route) => {
      if (route.request().method() === "GET") {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({
            investigations: [
              {
                id: "inv-inspect-004",
                payout_case_id: "demo-w9821-case-id",
                organization_id: "demo-org-id",
                requested_by_user_id: "operator-user-id",
                provider: "ollama-native",
                model: "qwen3.5:4b",
                prompt_version: "claimbridge-investigation-v1.0",
                status: "COMPLETED",
                summary: "Focus trap regression test.",
                confidence: "HIGH",
                requires_human_review: false,
                observed_facts: [],
                findings: [
                  {
                    explanation: "Keyboard focus trap verification finding",
                    evidence_references: ["demo-w9821-case-id"],
                  },
                ],
                recommended_next_steps: [],
                missing_information: [],
                risk_flags: [],
                limitations: [],
                created_at: new Date().toISOString(),
              },
            ],
          }),
        });
      } else {
        await route.continue();
      }
    });

    await page.route("**/api/v1/payout-cases/*/evidence/*", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          reference_id: "demo-w9821-case-id",
          source_type: "PAYOUT_CASE",
          display_label: "Payout Case (DEMO-W9821)",
          fields: [{ name: "Case Reference", value: "DEMO-W9821" }],
          is_synthetic_fixture: true,
          resolution_status: "RESOLVED",
        }),
      });
    });

    await page.goto("/app/payouts");
    const fixtureLink = page.locator('a:has-text("DEMO-W9821")');
    await expect(fixtureLink).toBeVisible();
    await fixtureLink.click();
    await expect(page).toHaveURL(/\/app\/payouts\/.+/);

    const evidenceChip = page.locator('[data-testid="evidence-chip-demo-w9821-case-id"]');
    await expect(evidenceChip).toBeVisible();

    // Focus chip with keyboard and activate with Enter
    await evidenceChip.focus();
    await expect(evidenceChip).toBeFocused();
    await page.keyboard.press("Enter");

    const drawer = page.locator('[data-testid="evidence-inspector-drawer"]');
    await expect(drawer).toBeVisible();

    // 1. Verify initial focus is placed on the close button inside the dialog
    const closeBtn = page.locator('[data-testid="evidence-inspector-close"]');
    await expect(closeBtn).toBeFocused();

    // 2. Press Tab -> moves forward to footer close button
    const footerCloseBtn = page.locator('[data-testid="evidence-inspector-footer-close"]');
    await page.keyboard.press("Tab");
    await expect(footerCloseBtn).toBeFocused();

    // 3. Press Tab on last element -> wraps forwards back to header close button
    await page.keyboard.press("Tab");
    await expect(closeBtn).toBeFocused();

    // 4. Press Shift+Tab on first element -> wraps backwards to footer close button
    await page.keyboard.press("Shift+Tab");
    await expect(footerCloseBtn).toBeFocused();

    // 5. Press Shift+Tab again -> returns to header close button
    await page.keyboard.press("Shift+Tab");
    await expect(closeBtn).toBeFocused();

    // 6. Dismiss with Escape and verify focus returns to triggering chip
    await page.keyboard.press("Escape");
    await expect(drawer).not.toBeVisible();
    await expect(evidenceChip).toBeFocused();
  });

  test("5. Focus restoration: Dismissing via footer close button returns focus to triggering chip", async ({
    page,
  }) => {
    await page.route("**/api/v1/payout-cases/*/investigations", async (route) => {
      if (route.request().method() === "GET") {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({
            investigations: [
              {
                id: "inv-inspect-005",
                payout_case_id: "demo-w9821-case-id",
                organization_id: "demo-org-id",
                requested_by_user_id: "operator-user-id",
                provider: "ollama-native",
                model: "qwen3.5:4b",
                prompt_version: "claimbridge-investigation-v1.0",
                status: "COMPLETED",
                summary: "Footer close dismissal test.",
                confidence: "HIGH",
                requires_human_review: false,
                observed_facts: [],
                findings: [
                  {
                    explanation: "Footer close dismissal finding",
                    evidence_references: ["demo-w9821-case-id"],
                  },
                ],
                recommended_next_steps: [],
                missing_information: [],
                risk_flags: [],
                limitations: [],
                created_at: new Date().toISOString(),
              },
            ],
          }),
        });
      } else {
        await route.continue();
      }
    });

    await page.route("**/api/v1/payout-cases/*/evidence/*", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          reference_id: "demo-w9821-case-id",
          source_type: "PAYOUT_CASE",
          display_label: "Payout Case (DEMO-W9821)",
          fields: [{ name: "Case Reference", value: "DEMO-W9821" }],
          is_synthetic_fixture: true,
          resolution_status: "RESOLVED",
        }),
      });
    });

    await page.goto("/app/payouts");
    const fixtureLink = page.locator('a:has-text("DEMO-W9821")');
    await expect(fixtureLink).toBeVisible();
    await fixtureLink.click();
    await expect(page).toHaveURL(/\/app\/payouts\/.+/);

    const evidenceChip = page.locator('[data-testid="evidence-chip-demo-w9821-case-id"]');
    await expect(evidenceChip).toBeVisible();
    await evidenceChip.click();

    const drawer = page.locator('[data-testid="evidence-inspector-drawer"]');
    await expect(drawer).toBeVisible();

    // Tab to footer close button and press Enter
    await page.keyboard.press("Tab");
    const footerCloseBtn = page.locator('[data-testid="evidence-inspector-footer-close"]');
    await expect(footerCloseBtn).toBeFocused();
    await page.keyboard.press("Enter");

    await expect(drawer).not.toBeVisible();
    await expect(evidenceChip).toBeFocused();
  });

  test("6. Error state focus trap: Cycles through Retry Verification and restores focus on dismissal", async ({
    page,
  }) => {
    await page.route("**/api/v1/payout-cases/*/investigations", async (route) => {
      if (route.request().method() === "GET") {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({
            investigations: [
              {
                id: "inv-inspect-006",
                payout_case_id: "demo-w9821-case-id",
                organization_id: "demo-org-id",
                requested_by_user_id: "operator-user-id",
                provider: "ollama-native",
                model: "qwen3.5:4b",
                prompt_version: "claimbridge-investigation-v1.0",
                status: "COMPLETED",
                summary: "Error state focus trap test.",
                confidence: "LOW",
                requires_human_review: true,
                observed_facts: [],
                findings: [
                  {
                    explanation: "Error state citation test",
                    evidence_references: ["non-existent-ref-id"],
                  },
                ],
                recommended_next_steps: [],
                missing_information: [],
                risk_flags: [],
                limitations: [],
                created_at: new Date().toISOString(),
              },
            ],
          }),
        });
      } else {
        await route.continue();
      }
    });

    await page.route("**/api/v1/payout-cases/*/evidence/non-existent-ref-id", async (route) => {
      await route.fulfill({
        status: 404,
        contentType: "application/json",
        body: JSON.stringify({
          reference_id: "non-existent-ref-id",
          resolution_status: "UNAVAILABLE",
          error: "evidence reference not found",
          code: "EVIDENCE_NOT_FOUND",
        }),
      });
    });

    await page.goto("/app/payouts");
    const fixtureLink = page.locator('a:has-text("DEMO-W9821")');
    await expect(fixtureLink).toBeVisible();
    await fixtureLink.click();
    await expect(page).toHaveURL(/\/app\/payouts\/.+/);

    const badChip = page.locator('[data-testid="evidence-chip-non-existent-ref-id"]');
    await expect(badChip).toBeVisible();
    await badChip.click();

    const drawer = page.locator('[data-testid="evidence-inspector-drawer"]');
    await expect(drawer).toBeVisible();

    const closeBtn = page.locator('[data-testid="evidence-inspector-close"]');
    await expect(closeBtn).toBeFocused();

    // Tab -> Retry Verification button
    await page.keyboard.press("Tab");
    const retryBtn = page.locator('button:has-text("Retry Verification")');
    await expect(retryBtn).toBeFocused();

    // Tab -> Footer Close Inspector button
    await page.keyboard.press("Tab");
    const footerCloseBtn = page.locator('[data-testid="evidence-inspector-footer-close"]');
    await expect(footerCloseBtn).toBeFocused();

    // Tab -> wraps back to Close button
    await page.keyboard.press("Tab");
    await expect(closeBtn).toBeFocused();

    // Shift+Tab -> wraps backwards to Footer Close Inspector button
    await page.keyboard.press("Shift+Tab");
    await expect(footerCloseBtn).toBeFocused();

    // Escape -> closes and restores focus to badChip
    await page.keyboard.press("Escape");
    await expect(drawer).not.toBeVisible();
    await expect(badChip).toBeFocused();
  });
});
