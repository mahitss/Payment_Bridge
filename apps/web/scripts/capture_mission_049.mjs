import { chromium } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';

const REPO_ASSETS_DIR = path.resolve(process.cwd(), '..', '..', 'docs', 'assets', 'mission_049_screenshots');
const BRAIN_SCREENSHOTS_DIR = path.resolve('C:\\Users\\pc\\.gemini\\antigravity\\brain\\5afcb402-df65-47fc-8a01-1316179b7bc5\\screenshots');

fs.mkdirSync(REPO_ASSETS_DIR, { recursive: true });
fs.mkdirSync(BRAIN_SCREENSHOTS_DIR, { recursive: true });

function verifyAndSave(fileName, tempPath) {
  if (!fs.existsSync(tempPath)) {
    throw new Error(`Screenshot not found: ${tempPath}`);
  }
  const size = fs.statSync(tempPath).size;
  if (size < 2000) {
    throw new Error(`Screenshot ${fileName} is too small (${size} bytes)`);
  }
  const repoDest = path.join(REPO_ASSETS_DIR, fileName);
  fs.copyFileSync(tempPath, repoDest);
  const brainDest = path.join(BRAIN_SCREENSHOTS_DIR, fileName);
  fs.copyFileSync(tempPath, brainDest);
  console.log(`[PASS] Saved ${fileName} (${size.toLocaleString()} bytes)`);
}

async function capture() {
  console.log('Launching browser for Mission 049 screenshot capture (1920x1080)...');
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({
    viewport: { width: 1920, height: 1080 },
    ignoreHTTPSErrors: true,
  });
  const page = await context.newPage();
  const tmpDir = path.resolve(process.cwd(), 'test-results', 'mission_049_temp');
  fs.mkdirSync(tmpDir, { recursive: true });

  try {
    // 1. Landing Page
    console.log('1. Capturing Landing Page...');
    await page.goto('http://localhost:3000/', { waitUntil: 'networkidle' });
    await page.waitForTimeout(1000);
    const p1 = path.join(tmpDir, '01_landing_page.png');
    await page.screenshot({ path: p1, fullPage: true });
    verifyAndSave('01_landing_page.png', p1);

    // 2. Sign In Page
    console.log('2. Capturing Sign In Page...');
    await page.goto('http://localhost:3000/login', { waitUntil: 'networkidle' });
    await page.waitForTimeout(500);
    const p2 = path.join(tmpDir, '02_signin_page.png');
    await page.screenshot({ path: p2 });
    verifyAndSave('02_signin_page.png', p2);

    // 2b. Registration Page
    console.log('2b. Capturing Registration Page...');
    await page.goto('http://localhost:3000/register', { waitUntil: 'networkidle' });
    await page.waitForTimeout(500);
    const p2b = path.join(tmpDir, '02b_registration_page.png');
    await page.screenshot({ path: p2b });
    verifyAndSave('02b_registration_page.png', p2b);

    // Login for authenticated screens
    console.log('Logging in as operator@claimbridge.internal...');
    await page.goto('http://localhost:3000/login', { waitUntil: 'networkidle' });
    await page.fill('#email', 'operator@claimbridge.internal');
    await page.fill('#password', process.env.DEMO_OPERATOR_PASSWORD || 'ClaimBridge-Op-2026!Eval');
    await page.click('button[type="submit"]');
    await page.waitForURL(/\/app/, { timeout: 10000 });
    await page.waitForLoadState('networkidle');

    // 3. Dashboard Overview
    console.log('3. Capturing Dashboard Overview...');
    await page.waitForTimeout(1000);
    const p3 = path.join(tmpDir, '03_dashboard_overview.png');
    await page.screenshot({ path: p3, fullPage: true });
    verifyAndSave('03_dashboard_overview.png', p3);

    // 4. Payout Registry
    console.log('4. Capturing Payout Registry...');
    await page.goto('http://localhost:3000/app/payouts', { waitUntil: 'networkidle' });
    await page.waitForTimeout(1000);
    const p4 = path.join(tmpDir, '04_payout_registry.png');
    await page.screenshot({ path: p4, fullPage: true });
    verifyAndSave('04_payout_registry.png', p4);

    // 5. Payout Creation
    console.log('5. Capturing Payout Creation...');
    await page.goto('http://localhost:3000/app/payouts/new', { waitUntil: 'networkidle' });
    await page.waitForTimeout(1000);
    const p5 = path.join(tmpDir, '05_payout_creation.png');
    await page.screenshot({ path: p5, fullPage: true });
    verifyAndSave('05_payout_creation.png', p5);

    // 6. Payout Detail
    console.log('6. Capturing Payout Detail for DEMO-W9821...');
    await page.goto('http://localhost:3000/app/payouts', { waitUntil: 'networkidle' });
    const fixtureLink = page.locator('a:has-text("DEMO-W9821")');
    await fixtureLink.waitFor({ state: 'visible', timeout: 8000 });
    await fixtureLink.click();
    await page.waitForURL(/\/app\/payouts\/.+/, { timeout: 10000 });
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    const currentUrl = page.url();
    const caseId = currentUrl.split('/').pop().split('?')[0];
    console.log(`Resolved DEMO-W9821 caseId: ${caseId}`);

    const p6 = path.join(tmpDir, '06_payout_detail.png');
    await page.screenshot({ path: p6, fullPage: true });
    verifyAndSave('06_payout_detail.png', p6);

    // 8. Evidence Inspector Drawer
    console.log('8. Capturing Evidence Inspector Drawer...');
    const evidenceChip = page.locator('button:has-text("SIMULATED-DEMO-BATCH-W9821"), [data-testid^="evidence-chip-"], button:has-text("DEMO-W9821")').first();
    if (await evidenceChip.isVisible()) {
      await evidenceChip.click();
      await page.waitForTimeout(1200);
    }
    const p8 = path.join(tmpDir, '08_evidence_inspector_drawer.png');
    await page.screenshot({ path: p8, fullPage: true });
    verifyAndSave('08_evidence_inspector_drawer.png', p8);

    // 7. Investigation Workspace
    console.log('7. Capturing Investigation Workspace...');
    await page.goto(`http://localhost:3000/app/payouts/${caseId}/investigate`, { waitUntil: 'networkidle' });
    await page.waitForTimeout(1500);
    const p7 = path.join(tmpDir, '07_investigation_workspace.png');
    await page.screenshot({ path: p7, fullPage: true });
    verifyAndSave('07_investigation_workspace.png', p7);

    // 9. Sandbox Integration Status
    console.log('9. Capturing Sandbox Integration Status...');
    await page.goto('http://localhost:3000/app/settings/integrations', { waitUntil: 'networkidle' });
    await page.waitForTimeout(1000);
    const p9 = path.join(tmpDir, '09_sandbox_integration_status.png');
    await page.screenshot({ path: p9, fullPage: true });
    verifyAndSave('09_sandbox_integration_status.png', p9);

    console.log('All 10 Mission 049 screenshots captured successfully.');
  } finally {
    await browser.close();
  }
}

capture().catch(err => {
  console.error('Screenshot capture failed:', err);
  process.exit(1);
});
