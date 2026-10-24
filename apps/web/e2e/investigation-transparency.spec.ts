import { test, expect } from "@playwright/test";

test.describe("Mission 025: AI Investigation Runtime Transparency & UX Polish", () => {
  test.beforeEach(async ({ page }) => {
    // Log in as operator
    await page.goto("/login");
    await page.fill("#email", "operator@claimbridge.internal");
    await page.fill("#password", "OperatorPass123!");
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/app/);
  });

  test("1. Native Ollama label, ollama_native_chat transport, and 60.6s duration display", async ({
    page,
  }) => {
    // Intercept investigation list to return a Native Ollama investigation result
    await page.route("**/api/v1/payout-cases/*/investigations", async (route) => {
      if (route.request().method() === "GET") {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({
            investigations: [
              {
                id: "inv-native-001",
                payout_case_id: "demo-w9821-case-id",
                organization_id: "demo-org-id",
                requested_by_user_id: "operator-user-id",
                provider: "ollama-openai-compatible",
                model: "qwen3.5:4b",
                prompt_version: "claimbridge-investigation-v1.0",
                status: "COMPLETED",
                summary: "Payout lifecycle analyzed via native bounded Ollama chat.",
                confidence: "HIGH",
                requires_human_review: false,
                observed_facts: ["Synthetic batch processed in test double"],
                findings: [
                  {
                    explanation: "Native inference confirmed unclaimed payout status",
                    evidence_references: ["demo-w9821-case-id"],
                  },
                ],
                recommended_next_steps: [
                  {
                    action: "CHECK_PROVIDER_STATUS",
                    reason: "Verify settlement status with provider",
                    requires_human_review: false,
                    deterministic_policy_permitted: true,
                  },
                ],
                missing_information: [],
                risk_flags: [],
                limitations: ["Based on snapshot at time of native execution"],
                raw_response: {
                  provider: "ollama-openai-compatible",
                  transport: "ollama_native_chat",
                  model: "qwen3.5:4b",
                  done_reason: "stop",
                  inference_duration_ms: 60600,
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

    await page.goto("/app/payouts");
    const fixtureLink = page.locator('a:has-text("DEMO-W9821")');
    await expect(fixtureLink).toBeVisible({ timeout: 10000 });
    await fixtureLink.click();
    await expect(page).toHaveURL(/\/app\/payouts\/.+/, { timeout: 15000 });

    // Header badges
    const headerTransportBadge = page.locator('[data-testid="header-transport-badge"]');
    await expect(headerTransportBadge).toBeVisible();
    await expect(headerTransportBadge).toHaveText("Ollama Native Chat");

    const headerDurationBadge = page.locator('[data-testid="header-duration-badge"]');
    await expect(headerDurationBadge).toBeVisible();
    await expect(headerDurationBadge).toHaveText("Inference: 60.6s (60,600ms)");

    // Bottom telemetry bar
    const transportInfo = page.locator('[data-testid="investigation-transport"]');
    await expect(transportInfo).toBeVisible();
    await expect(transportInfo).toHaveText("Transport: Native Ollama Chat");

    const engineInfo = page.locator('[data-testid="investigation-engine"]');
    await expect(engineInfo).toBeVisible();
    await expect(engineInfo).toHaveText("Engine: ollama-openai-compatible (qwen3.5:4b)");

    const durationInfo = page.locator('[data-testid="investigation-duration"]');
    await expect(durationInfo).toBeVisible();
    await expect(durationInfo).toHaveText("Duration: 60.6s (60,600ms)");

    // Raw telemetry accordion
    const toggleButton = page.locator('[data-testid="toggle-raw-telemetry"]');
    await expect(toggleButton).toBeVisible();
    await toggleButton.click();
    const rawPre = page.locator('[data-testid="raw-telemetry-json"]');
    await expect(rawPre).toBeVisible();
    await expect(rawPre).toContainText('"transport": "ollama_native_chat"');
    await expect(rawPre).toContainText('"inference_duration_ms": 60600');
  });

  test("2. OpenAI-compatible transport display and millisecond duration", async ({
    page,
  }) => {
    // Intercept investigation list to return an OpenAI-compatible result
    await page.route("**/api/v1/payout-cases/*/investigations", async (route) => {
      if (route.request().method() === "GET") {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({
            investigations: [
              {
                id: "inv-openai-002",
                payout_case_id: "demo-w9821-case-id",
                organization_id: "demo-org-id",
                requested_by_user_id: "operator-user-id",
                provider: "ollama-openai-compatible",
                model: "llama3.2",
                prompt_version: "claimbridge-investigation-v1.0",
                status: "COMPLETED",
                summary: "Payout lifecycle analyzed via OpenAI-compatible endpoint.",
                confidence: "HIGH",
                requires_human_review: false,
                observed_facts: ["Snapshot reconciled"],
                findings: [
                  {
                    explanation: "OpenAI-compatible chat completed analysis",
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
                limitations: ["Standard snapshot"],
                raw_response: {
                  provider: "ollama-openai-compatible",
                  transport: "openai_compatible_chat",
                  model: "llama3.2",
                  finish_reason: "stop",
                  inference_duration_ms: 1250,
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

    await page.goto("/app/payouts");
    const fixtureLink = page.locator('a:has-text("DEMO-W9821")');
    await expect(fixtureLink).toBeVisible({ timeout: 10000 });
    await fixtureLink.click();
    await expect(page).toHaveURL(/\/app\/payouts\/.+/, { timeout: 15000 });

    // Header badges
    const headerTransportBadge = page.locator('[data-testid="header-transport-badge"]');
    await expect(headerTransportBadge).toBeVisible();
    await expect(headerTransportBadge).toHaveText("OpenAI Compatible");

    const headerDurationBadge = page.locator('[data-testid="header-duration-badge"]');
    await expect(headerDurationBadge).toBeVisible();
    await expect(headerDurationBadge).toHaveText("Inference: 1.3s (1,250ms)");

    // Bottom telemetry bar
    const transportInfo = page.locator('[data-testid="investigation-transport"]');
    await expect(transportInfo).toBeVisible();
    await expect(transportInfo).toHaveText("Transport: OpenAI-Compatible Chat");

    const durationInfo = page.locator('[data-testid="investigation-duration"]');
    await expect(durationInfo).toBeVisible();
    await expect(durationInfo).toHaveText("Duration: 1.3s (1,250ms)");
  });

  test("3. Historical records without transport or duration display truthful fallback without invented numbers", async ({
    page,
  }) => {
    // Intercept investigation list to return a historical record (no transport, no duration)
    await page.route("**/api/v1/payout-cases/*/investigations", async (route) => {
      if (route.request().method() === "GET") {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({
            investigations: [
              {
                id: "inv-historical-003",
                payout_case_id: "demo-w9821-case-id",
                organization_id: "demo-org-id",
                requested_by_user_id: "operator-user-id",
                provider: "ollama-openai-compatible",
                model: "llama3.2",
                prompt_version: "claimbridge-investigation-v1.0",
                status: "COMPLETED",
                summary: "Historical investigation without transport metadata.",
                confidence: "HIGH",
                requires_human_review: false,
                observed_facts: ["Historical snapshot"],
                findings: [
                  {
                    explanation: "Historical report loaded",
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
                // Notice: raw_response has no transport and no inference_duration_ms
                raw_response: {
                  provider: "ollama-openai-compatible",
                  model: "llama3.2",
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

    await page.goto("/app/payouts");
    const fixtureLink = page.locator('a:has-text("DEMO-W9821")');
    await expect(fixtureLink).toBeVisible({ timeout: 10000 });
    await fixtureLink.click();
    await expect(page).toHaveURL(/\/app\/payouts\/.+/, { timeout: 15000 });

    // Must show Historical / Untracked badge
    const headerTransportBadge = page.locator('[data-testid="header-transport-badge"]');
    await expect(headerTransportBadge).toBeVisible();
    await expect(headerTransportBadge).toHaveText("Historical / Untracked");

    // Header duration badge must NOT be present
    await expect(page.locator('[data-testid="header-duration-badge"]')).not.toBeVisible();

    // Bottom telemetry bar
    const transportInfo = page.locator('[data-testid="investigation-transport"]');
    await expect(transportInfo).toBeVisible();
    await expect(transportInfo).toHaveText("Transport: Standard Provider (Historical / Untracked Transport)");

    // Duration must show truthful historical fallback, never invented values
    const historicalDuration = page.locator('[data-testid="investigation-duration-historical"]');
    await expect(historicalDuration).toBeVisible();
    await expect(historicalDuration).toHaveText("Duration: Not recorded (historical)");
    await expect(page.locator('[data-testid="investigation-duration"]')).not.toBeVisible();
  });

  test("4. In-progress experience: elapsed timer start, stop, accessible role='status', hardware copy, advisory disclaimer, and duplicate prevention", async ({
    page,
  }) => {
    let postRequestCount = 0;

    // Intercept POST to simulate local CPU inference latency (1.5 seconds)
    await page.route("**/api/v1/payout-cases/*/investigations", async (route) => {
      if (route.request().method() === "POST") {
        postRequestCount++;
        // Delay response to inspect pending UI state
        await new Promise((resolve) => setTimeout(resolve, 1500));
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({
            id: "inv-new-004",
            payout_case_id: "demo-w9821-case-id",
            organization_id: "demo-org-id",
            requested_by_user_id: "operator-user-id",
            provider: "ollama-openai-compatible",
            model: "qwen3.5:4b",
            prompt_version: "claimbridge-investigation-v1.0",
            status: "COMPLETED",
            summary: "Freshly completed investigation.",
            confidence: "HIGH",
            requires_human_review: false,
            observed_facts: ["Observed live"],
            findings: [
              {
                explanation: "Freshly analyzed with timing",
                evidence_references: ["demo-w9821-case-id"],
              },
            ],
            recommended_next_steps: [
              {
                action: "CHECK_PROVIDER_STATUS",
                reason: "Check",
                requires_human_review: false,
                deterministic_policy_permitted: true,
              },
            ],
            missing_information: [],
            risk_flags: [],
            limitations: [],
            raw_response: {
              provider: "ollama-openai-compatible",
              transport: "ollama_native_chat",
              model: "qwen3.5:4b",
              done_reason: "stop",
              inference_duration_ms: 1500,
            },
            created_at: new Date().toISOString(),
          }),
        });
      } else {
        await route.continue();
      }
    });

    await page.goto("/app/payouts");
    const fixtureLink = page.locator('a:has-text("DEMO-W9821")');
    await expect(fixtureLink).toBeVisible({ timeout: 10000 });
    await fixtureLink.click();
    await expect(page).toHaveURL(/\/app\/payouts\/.+/, { timeout: 15000 });

    const runButton = page.getByRole("button", { name: "Run Investigation" });
    await expect(runButton).toBeVisible();
    await expect(runButton).toBeEnabled();

    // Trigger investigation
    await runButton.click();

    // 1. Progress panel is immediately rendered with role="status" and polite live region
    const pendingPanel = page.locator('[data-testid="investigation-pending-panel"]');
    await expect(pendingPanel).toBeVisible({ timeout: 2000 });
    await expect(pendingPanel).toHaveAttribute("role", "status");
    await expect(pendingPanel).toHaveAttribute("aria-live", "polite");

    // 2. Elapsed timer is running
    const timer = page.locator('[data-testid="elapsed-timer"]');
    await expect(timer).toBeVisible();
    await expect(timer).toContainText("Elapsed:");

    // 3. Explanatory copy that local CPU inference may take over a minute and varies by hardware
    await expect(
      page.locator("text=On CPU-only development hardware, processing local model weights may take over a minute")
    ).toBeVisible();

    // 4. Explicit advisory statement: does not execute or cancel payout
    await expect(
      page.locator("text=AI ADVISORY SCOPE: This investigation is strictly forensic and advisory. It will not submit, approve, execute, or cancel any financial payout.")
    ).toBeVisible();

    // 5. Actions are disabled while pending to prevent duplicate submissions
    await expect(page.getByRole("button", { name: "Investigating..." })).toBeDisabled();

    // Attempting to click disabled button must not trigger a second request
    await page.getByRole("button", { name: "Investigating..." }).click({ force: true }).catch(() => {});
    expect(postRequestCount).toBe(1);

    // 6. Wait for request to complete
    const successBanner = page.locator('[data-testid="investigation-success-banner"]');
    await expect(successBanner).toBeVisible({ timeout: 10000 });
    await expect(successBanner).toHaveText("Investigation completed with grounded evidence.");

    // 7. Pending panel is cleanly unmounted and timer stopped
    await expect(pendingPanel).not.toBeVisible();

    // Completed investigation displays the new result with native transport & duration
    await expect(page.locator('[data-testid="investigation-transport"]')).toHaveText("Transport: Native Ollama Chat");
    await expect(page.locator('[data-testid="investigation-duration"]')).toHaveText("Duration: 1.5s (1,500ms)");
  });

  test("5. Failed investigation request stops timer, restores button state, and presents accessible error", async ({
    page,
  }) => {
    // Intercept POST to simulate AI offline 503 error
    await page.route("**/api/v1/payout-cases/*/investigations", async (route) => {
      if (route.request().method() === "POST") {
        await new Promise((resolve) => setTimeout(resolve, 800));
        await route.fulfill({
          status: 503,
          contentType: "application/json",
          body: JSON.stringify({
            error: "Investigation assistant is currently offline or unreachable. Core payout operations remain fully functional.",
            code: "AI_UNAVAILABLE",
          }),
        });
      } else {
        await route.continue();
      }
    });

    await page.goto("/app/payouts");
    const fixtureLink = page.locator('a:has-text("DEMO-W9821")');
    await expect(fixtureLink).toBeVisible({ timeout: 10000 });
    await fixtureLink.click();
    await expect(page).toHaveURL(/\/app\/payouts\/.+/, { timeout: 15000 });

    const runButton = page.getByRole("button", { name: "Run Investigation" });
    await runButton.click();

    // Pending panel is displayed
    const pendingPanel = page.locator('[data-testid="investigation-pending-panel"]');
    await expect(pendingPanel).toBeVisible({ timeout: 2000 });

    // Wait for failure
    const errorBanner = page.locator('[data-testid="investigation-error-banner"]');
    await expect(errorBanner).toBeVisible({ timeout: 10000 });
    await expect(errorBanner).toHaveAttribute("role", "alert");
    await expect(errorBanner).toContainText("Investigation assistant is currently offline or unreachable.");

    // Pending panel unmounted and timer stopped
    await expect(pendingPanel).not.toBeVisible();

    // Button restored to operable state
    await expect(runButton).toBeEnabled();
  });
});
