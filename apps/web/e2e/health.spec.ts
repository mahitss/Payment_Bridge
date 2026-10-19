import { test, expect } from "@playwright/test";

test.describe("A. Application Startup, Health, and Landing Page Truthfulness", () => {
  test("landing page displays truthful prototype branding and no false release claims", async ({
    page,
  }) => {
    await page.goto("/");

    // Verify title and main heading
    await expect(page).toHaveTitle(/ClaimBridge/i);
    await expect(page.locator("h1")).toContainText("ClaimBridge");

    // Must NOT claim unconditional "Verified Release"
    const releaseBadge = page.locator("text=Verified Release");
    await expect(releaseBadge).not.toBeVisible();

    // Must display honest Sandbox Prototype badge
    const prototypeBadge = page.locator("text=Sandbox Prototype");
    await expect(prototypeBadge).toBeVisible();

    // Must describe truth-grounded architecture without unconditional verification claims
    await expect(
      page.locator("text=using provider status records, evidence-grounded AI explanations")
    ).toBeVisible();
  });

  test("gateway probe accurately distinguishes API reachability from PayPal Sandbox authentication", async ({
    page,
  }) => {
    await page.goto("/");

    const probeButton = page.getByRole("button", { name: /Probe Gateway/i });
    await expect(probeButton).toBeVisible();

    // Trigger diagnostic probe
    await probeButton.click();

    // Wait for diagnostic probe card to appear
    const diagnosticCard = page.locator("text=System Diagnostic Probe");
    await expect(diagnosticCard).toBeVisible({ timeout: 10000 });

    // API Gateway status must be explicitly stated
    const gatewayStatus = page.locator("text=API Gateway:").locator("..");
    await expect(gatewayStatus).toBeVisible();

    // PayPal Sandbox status must be explicitly stated (never conflated with API reachability)
    const sandboxStatus = page.locator("text=PayPal Sandbox:").locator("..");
    await expect(sandboxStatus).toBeVisible();

    // Must NOT leak secrets, raw access tokens, or arbitrary JSON dump
    const rawTokenDump = page.locator("text=access_token");
    await expect(rawTokenDump).not.toBeVisible();
    const rawJsonDump = page.locator("text=Gateway Live & Sandbox Connected");
    await expect(rawJsonDump).not.toBeVisible();
  });

  test("backend /healthz and /readyz endpoints return valid JSON contracts", async ({
    request,
  }) => {
    const healthRes = await request.get("/healthz");
    expect(healthRes.status()).toBe(200);
    const healthJson = await healthRes.json();
    expect(healthJson.status).toBe("ok");
    expect(healthJson.service).toBe("claimbridge-api");

    const readyRes = await request.get("/readyz");
    // Ready endpoint returns 200 or 503 depending on database reachability
    expect([200, 503]).toContain(readyRes.status());
    const readyJson = await readyRes.json();
    expect(readyJson).toHaveProperty("database");
    expect(readyJson).toHaveProperty("status");
  });
});
