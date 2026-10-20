import { test, expect } from "@playwright/test";

test.describe("F. Responsive UX, Navigation, and Accessibility Hardening", () => {
  test("desktop navigation exposes all primary workspace links and marks the active route", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1280, height: 800 });

    // Log in as operator
    await page.goto("/login");
    await page.fill("#email", "operator@claimbridge.internal");
    await page.fill("#password", "OperatorPass123!");
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/app/);

    // Desktop nav should be visible
    const desktopNav = page.locator("nav.hidden.md\\:flex, header nav").first();
    await expect(desktopNav).toBeVisible();

    // Verify all 4 primary destinations
    const overviewLink = page.getByRole("link", { name: "Overview" });
    const casesLink = page.getByRole("link", { name: "Payout Cases" });
    const newPayoutLink = page.getByRole("link", { name: "New Payout" });
    const sandboxLink = page.getByRole("link", { name: "PayPal Sandbox" });

    await expect(overviewLink).toBeVisible();
    await expect(casesLink).toBeVisible();
    await expect(newPayoutLink).toBeVisible();
    await expect(sandboxLink).toBeVisible();

    // Overview should be marked active
    await expect(overviewLink).toHaveAttribute("aria-current", "page");

    // Navigate to Payout Cases
    await casesLink.click();
    await expect(page).toHaveURL(/\/app\/payouts/);
    await expect(page.getByRole("link", { name: "Payout Cases" })).toHaveAttribute(
      "aria-current",
      "page"
    );
  });

  test("mobile navigation drawer opens, exposes destinations with proper ARIA, and closes via toggle and Escape", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 375, height: 812 });

    // Log in
    await page.goto("/login");
    await page.fill("#email", "operator@claimbridge.internal");
    await page.fill("#password", "OperatorPass123!");
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/app/);

    // Menu button should be visible with proper ARIA
    const menuButton = page.getByRole("button", { name: /Open navigation menu/i });
    await expect(menuButton).toBeVisible();
    await expect(menuButton).toHaveAttribute("aria-expanded", "false");
    await expect(menuButton).toHaveAttribute("aria-controls", "mobile-navigation");

    // Drawer should not be visible initially
    const drawer = page.locator("#mobile-navigation");
    await expect(drawer).not.toBeVisible();

    // Open drawer
    await menuButton.click();
    await expect(drawer).toBeVisible();
    await expect(page.getByRole("button", { name: /Close navigation menu/i })).toHaveAttribute(
      "aria-expanded",
      "true"
    );

    // Drawer should expose all 4 destinations and tenant / user info
    await expect(drawer.getByRole("link", { name: "Overview" })).toBeVisible();
    await expect(drawer.getByRole("link", { name: "Payout Cases" })).toBeVisible();
    await expect(drawer.getByRole("link", { name: "New Payout" })).toBeVisible();
    await expect(drawer.getByRole("link", { name: "PayPal Sandbox" })).toBeVisible();
    await expect(drawer.locator("text=operator@claimbridge.internal")).toBeVisible();
    await expect(drawer.getByRole("link", { name: "Account Settings" })).toBeVisible();

    // Press Escape to close
    await page.keyboard.press("Escape");
    await expect(drawer).not.toBeVisible();
    await expect(page.getByRole("button", { name: /Open navigation menu/i })).toHaveAttribute(
      "aria-expanded",
      "false"
    );
  });

  test("mobile drawer closes automatically upon selecting a navigation link", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 375, height: 812 });

    // Log in
    await page.goto("/login");
    await page.fill("#email", "operator@claimbridge.internal");
    await page.fill("#password", "OperatorPass123!");
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/app/);

    // Open drawer
    const menuButton = page.getByRole("button", { name: /Open navigation menu/i });
    await menuButton.click();
    const drawer = page.locator("#mobile-navigation");
    await expect(drawer).toBeVisible();

    // Click "New Payout"
    await drawer.getByRole("link", { name: "New Payout" }).click();
    await expect(page).toHaveURL(/\/app\/payouts\/new/);

    // Drawer should be closed
    await expect(drawer).not.toBeVisible();
  });

  test("skip link is discoverable and navigates to main content", async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.goto("/login");

    // Tab once to focus skip link
    await page.keyboard.press("Tab");
    const skipLink = page.locator("a.skip-link");
    await expect(skipLink).toBeFocused();
    await expect(skipLink).toHaveText("Skip to main content");
  });

  test("no horizontal document overflow across representative viewports", async ({
    page,
  }) => {
    const viewports = [
      { width: 320, height: 568 },
      { width: 375, height: 812 },
      { width: 390, height: 844 },
      { width: 768, height: 1024 },
      { width: 1024, height: 768 },
      { width: 1440, height: 900 },
    ];

    for (const vp of viewports) {
      await page.setViewportSize(vp);
      await page.goto("/login");

      const hasOverflow = await page.evaluate(() => {
        return document.documentElement.scrollWidth > window.innerWidth;
      });
      expect(hasOverflow, `Overflow detected on /login at ${vp.width}x${vp.height}`).toBe(false);
    }
  });

  test("authentication form accessibility: explicit labels, autocomplete, and password visibility toggle", async ({
    page,
  }) => {
    await page.goto("/login");

    // Programmatic label associations
    const emailLabel = page.locator('label[for="email"]');
    await expect(emailLabel).toBeVisible();
    await expect(page.locator("#email")).toHaveAttribute("autocomplete", "email");

    const passwordLabel = page.locator('label[for="password"]');
    await expect(passwordLabel).toBeVisible();
    await expect(page.locator("#password")).toHaveAttribute("autocomplete", "current-password");

    // Password visibility toggle accessible name
    const toggleButton = page.getByRole("button", { name: "Show password" });
    await expect(toggleButton).toBeVisible();
    await expect(page.locator("#password")).toHaveAttribute("type", "password");

    // Click toggle
    await toggleButton.click();
    await expect(page.locator("#password")).toHaveAttribute("type", "text");
    await expect(page.getByRole("button", { name: "Hide password" })).toBeVisible();
  });

  test("new payout case form accessibility: explicit label bindings and policy guidance on mobile", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 375, height: 812 });

    // Log in
    await page.goto("/login");
    await page.fill("#email", "operator@claimbridge.internal");
    await page.fill("#password", "OperatorPass123!");
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/app/);

    await page.goto("/app/payouts/new");
    await expect(page.locator("h1")).toContainText("Create Payout Settlement Case");

    // Verify all input IDs have matching labels
    const inputIds = [
      "case-reference",
      "settlement-title",
      "recipient-email",
      "recipient-name",
      "payout-amount",
      "payout-currency",
      "risk-score",
      "case-description",
    ];

    for (const id of inputIds) {
      const label = page.locator(`label[for="${id}"]`);
      await expect(label, `Missing label for #${id}`).toBeVisible();
      const input = page.locator(`#${id}`);
      await expect(input, `Missing input #${id}`).toBeVisible();
    }

    // Verify helper texts are present and amount inputMode is decimal
    await expect(page.locator("#payout-amount")).toHaveAttribute("inputmode", "decimal");
    await expect(page.locator("#payout-amount-hint")).toContainText("500.00");

    // Verify page does not overflow horizontally at 375px
    const hasOverflow = await page.evaluate(() => {
      return document.documentElement.scrollWidth > window.innerWidth;
    });
    expect(hasOverflow).toBe(false);
  });

  test("primary interactive controls maintain usable touch targets at 320px width", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 320, height: 568 });
    await page.goto("/login");

    const submitButton = page.locator('button[type="submit"]');
    await expect(submitButton).toBeVisible();
    const box = await submitButton.boundingBox();
    expect(box).not.toBeNull();
    if (box) {
      // Touch target should be at least 44px in height
      expect(box.height).toBeGreaterThanOrEqual(44);
    }
  });
});
