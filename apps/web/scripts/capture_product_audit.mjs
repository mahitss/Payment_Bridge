import { chromium } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';

const SCREENSHOT_DIR = path.resolve(process.cwd(), 'test-results', 'mission_040_9_screenshots');
fs.mkdirSync(SCREENSHOT_DIR, { recursive: true });

async function run() {
  console.log('[Product Walkthrough] Starting headless Chromium...');
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({
    viewport: { width: 1920, height: 1080 },
    ignoreHTTPSErrors: true,
  });
  const page = await context.newPage();

  try {
    // 1. Landing Page & Diagnostic Probe
    console.log('[Product Walkthrough] Step 1: Landing Page & Diagnostic Probe');
    await page.goto('http://localhost:3000/', { waitUntil: 'networkidle' });
    const probeBtn = page.getByRole('button', { name: /Probe Gateway/i });
    if (await probeBtn.isVisible()) {
      await probeBtn.click();
      await page.waitForTimeout(2000);
    }
    await page.screenshot({ path: path.join(SCREENSHOT_DIR, '01_landing_diagnostic_probe.png'), fullPage: true });
    console.log('  -> Captured 01_landing_diagnostic_probe.png');

    // 2. Authentication & Dashboard
    console.log('[Product Walkthrough] Step 2: Authentication & Dashboard');
    await page.goto('http://localhost:3000/login', { waitUntil: 'networkidle' });
    await page.fill('#email', 'operator@claimbridge.internal');
    await page.fill('#password', 'OperatorPass123!');
    await page.click('button[type="submit"]');
    await page.waitForURL(/\/app/, { timeout: 10000 });
    await page.waitForTimeout(2000);
    await page.screenshot({ path: path.join(SCREENSHOT_DIR, '02_dashboard_overview.png'), fullPage: true });
    console.log('  -> Captured 02_dashboard_overview.png');

    // 3. Payouts List
    console.log('[Product Walkthrough] Step 3: Payouts List');
    await page.goto('http://localhost:3000/app/payouts', { waitUntil: 'networkidle' });
    await page.waitForTimeout(1500);
    await page.screenshot({ path: path.join(SCREENSHOT_DIR, '03_payouts_case_list.png'), fullPage: true });
    console.log('  -> Captured 03_payouts_case_list.png');

    // 4. Investigation Case Detail (DEMO-W9821)
    console.log('[Product Walkthrough] Step 4: Investigation Case Detail (DEMO-W9821)');
    await page.goto('http://localhost:3000/app/payouts/2701e127-5dfd-4ac4-8b88-693efa68df48', { waitUntil: 'networkidle' });
    await page.waitForTimeout(2000);
    await page.screenshot({ path: path.join(SCREENSHOT_DIR, '04_investigation_case_detail.png'), fullPage: false });
    console.log('  -> Captured 04_investigation_case_detail.png');

    // 5. AI Findings & Grounded Evidence
    console.log('[Product Walkthrough] Step 5: AI Findings & Grounded Evidence');
    const aiSection = page.locator('text=AI-Assisted Investigation').or(page.locator('text=Investigation Findings')).first();
    if (await aiSection.isVisible()) {
      await aiSection.scrollIntoViewIfNeeded();
      await page.waitForTimeout(1000);
    }
    await page.screenshot({ path: path.join(SCREENSHOT_DIR, '05_ai_findings_and_evidence.png'), fullPage: false });
    console.log('  -> Captured 05_ai_findings_and_evidence.png');

    // 6. Policy Decision & Recommended Remediation
    console.log('[Product Walkthrough] Step 6: Policy Decision & Remediation');
    const policySection = page.locator('text=Deterministic Policy').or(page.locator('text=Policy Evaluation')).first();
    if (await policySection.isVisible()) {
      await policySection.scrollIntoViewIfNeeded();
      await page.waitForTimeout(1000);
    }
    await page.screenshot({ path: path.join(SCREENSHOT_DIR, '06_policy_decision_remediation.png'), fullPage: false });
    console.log('  -> Captured 06_policy_decision_remediation.png');

    // 7. Dual-Approval Workflow (DEMO-M4421)
    console.log('[Product Walkthrough] Step 7: Dual-Approval Workflow (DEMO-M4421)');
    await page.goto('http://localhost:3000/app/payouts/a068e86e-a7af-46aa-8d57-c51db38987a3', { waitUntil: 'networkidle' });
    await page.waitForTimeout(2000);
    await page.screenshot({ path: path.join(SCREENSHOT_DIR, '07_approval_workflow_dual_custody.png'), fullPage: false });
    console.log('  -> Captured 07_approval_workflow_dual_custody.png');

    // 8. Immutable Audit Trail Evidence
    console.log('[Product Walkthrough] Step 8: Audit Trail Evidence');
    const auditSection = page.locator('text=Audit Trail').or(page.locator('text=Audit Evidence')).or(page.locator('text=Event History')).first();
    if (await auditSection.isVisible()) {
      await auditSection.scrollIntoViewIfNeeded();
      await page.waitForTimeout(1000);
    }
    await page.screenshot({ path: path.join(SCREENSHOT_DIR, '08_audit_trail_evidence.png'), fullPage: false });
    console.log('  -> Captured 08_audit_trail_evidence.png');

    // 9. Sandbox Integration Status Page
    console.log('[Product Walkthrough] Step 9: Sandbox Integration Status Page');
    await page.goto('http://localhost:3000/app/settings/integrations', { waitUntil: 'networkidle' });
    await page.waitForTimeout(2000);
    await page.screenshot({ path: path.join(SCREENSHOT_DIR, '09_sandbox_integration_status.png'), fullPage: true });
    console.log('  -> Captured 09_sandbox_integration_status.png');

    console.log('\n[SUCCESS] All 9 walkthrough screenshots successfully captured!');
  } catch (err) {
    console.error('[Product Walkthrough: FAILED]', err);
    throw err;
  } finally {
    await browser.close();
  }
}

run().catch(() => process.exit(1));
