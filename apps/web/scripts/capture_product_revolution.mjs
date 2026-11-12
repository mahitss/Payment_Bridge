import { chromium } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';

// Output directories
const TEST_RESULTS_DIR = path.resolve(process.cwd(), 'test-results', 'mission_045_1_screenshots');
const REPO_ASSETS_DIR = path.resolve(process.cwd(), '..', '..', 'docs', 'assets', 'mission_045_1_screenshots');
const BRAIN_SCREENSHOTS_DIR = path.resolve('C:\\Users\\pc\\.gemini\\antigravity\\brain\\5afcb402-df65-47fc-8a01-1316179b7bc5\\screenshots');

fs.mkdirSync(TEST_RESULTS_DIR, { recursive: true });
fs.mkdirSync(REPO_ASSETS_DIR, { recursive: true });
fs.mkdirSync(BRAIN_SCREENSHOTS_DIR, { recursive: true });

function verifyAndSave(fileName) {
  const src = path.join(TEST_RESULTS_DIR, fileName);
  if (!fs.existsSync(src)) {
    throw new Error(`[Capture Error] Screenshot was not created: ${src}`);
  }
  const size = fs.statSync(src).size;
  if (size < 2000) {
    throw new Error(`[Capture Error] Screenshot ${fileName} is too small (${size} bytes)`);
  }
  // Copy to repo docs assets
  const destRepo = path.join(REPO_ASSETS_DIR, fileName);
  fs.copyFileSync(src, destRepo);
  // Copy to brain artifact directory
  const destBrain = path.join(BRAIN_SCREENSHOTS_DIR, fileName);
  fs.copyFileSync(src, destBrain);
  console.log(`  -> Verified & saved ${fileName} (${size.toLocaleString()} bytes)`);
}

async function run() {
  console.log('[Capture] Launching Chromium 1920x1080...');
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({
    viewport: { width: 1920, height: 1080 },
    ignoreHTTPSErrors: true,
  });
  const page = await context.newPage();

  try {
    // 1. Landing Page
    console.log('[Step 1] Capturing Landing Page...');
    await page.goto('http://localhost:3000/', { waitUntil: 'networkidle' });
    await page.waitForSelector('h1', { state: 'visible', timeout: 8000 });
    // Trigger gateway probe so live status is shown
    const probeBtn = page.getByRole('button', { name: /Probe Gateway/i });
    if (await probeBtn.isVisible()) {
      await probeBtn.click();
      await page.waitForTimeout(1500);
    }
    await page.screenshot({ path: path.join(TEST_RESULTS_DIR, '01_landing_page.png'), fullPage: true });
    verifyAndSave('01_landing_page.png');

    // 2. Sign-in Page
    console.log('[Step 2a] Capturing Sign-in Page...');
    await page.goto('http://localhost:3000/login', { waitUntil: 'networkidle' });
    await page.waitForSelector('#email', { state: 'visible', timeout: 8000 });
    await page.screenshot({ path: path.join(TEST_RESULTS_DIR, '02_signin_page.png'), fullPage: true });
    verifyAndSave('02_signin_page.png');

    // 2b. Registration Page
    console.log('[Step 2b] Capturing Registration Page...');
    await page.goto('http://localhost:3000/register', { waitUntil: 'networkidle' });
    await page.waitForSelector('input[name="fullName"], input[type="text"]', { state: 'visible', timeout: 8000 });
    await page.screenshot({ path: path.join(TEST_RESULTS_DIR, '02b_registration_page.png'), fullPage: true });
    verifyAndSave('02b_registration_page.png');

    // Authenticate as Operator
    console.log('[Auth] Authenticating as operator@claimbridge.internal...');
    await page.goto('http://localhost:3000/login', { waitUntil: 'networkidle' });
    await page.fill('#email', 'operator@claimbridge.internal');
    const opPassword = process.env.DEMO_OPERATOR_PASSWORD || 'ClaimBridge-Op-2026!Eval';
    await page.fill('#password', opPassword);
    await page.click('button[type="submit"]');
    await page.waitForURL(/\/app/, { timeout: 10000 });
    await page.waitForLoadState('networkidle');

    // 3. Dashboard
    console.log('[Step 3] Capturing Dashboard...');
    await page.waitForSelector('text=Total Payout Cases', { state: 'visible', timeout: 10000 });
    await page.screenshot({ path: path.join(TEST_RESULTS_DIR, '03_dashboard.png'), fullPage: true });
    verifyAndSave('03_dashboard.png');

    // 4. Payout Registry
    console.log('[Step 4] Capturing Payout Registry...');
    await page.goto('http://localhost:3000/app/payouts', { waitUntil: 'networkidle' });
    await page.waitForSelector('table', { state: 'visible', timeout: 8000 });
    await page.screenshot({ path: path.join(TEST_RESULTS_DIR, '04_payout_registry.png'), fullPage: true });
    verifyAndSave('04_payout_registry.png');

    // 5. Payout Creation
    console.log('[Step 5] Capturing Payout Creation...');
    await page.goto('http://localhost:3000/app/payouts/new', { waitUntil: 'networkidle' });
    await page.waitForSelector('input[placeholder="150.00"]', { state: 'visible', timeout: 8000 });
    // Fill in a dual-custody amount ($350.00) to showcase live policy engine evaluation
    await page.fill('input[placeholder="150.00"]', '350.00');
    await page.waitForTimeout(500);
    await page.screenshot({ path: path.join(TEST_RESULTS_DIR, '05_payout_creation.png'), fullPage: true });
    verifyAndSave('05_payout_creation.png');

    // 6. Payout Detail
    console.log('[Step 6] Capturing Payout Detail (DEMO-W9821)...');
    await page.goto('http://localhost:3000/app/payouts/2701e127-5dfd-4ac4-8b88-693efa68df48', { waitUntil: 'networkidle' });
    await page.waitForSelector('h1:has-text("DEMO-W9821")', { state: 'visible', timeout: 8000 });
    await page.screenshot({ path: path.join(TEST_RESULTS_DIR, '06_payout_detail.png'), fullPage: true });
    verifyAndSave('06_payout_detail.png');

    // 7. Investigation Workspace
    console.log('[Step 7] Capturing Investigation Workspace...');
    await page.goto('http://localhost:3000/app/payouts/2701e127-5dfd-4ac4-8b88-693efa68df48/investigate', { waitUntil: 'networkidle' });
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1000);
    await page.screenshot({ path: path.join(TEST_RESULTS_DIR, '07_investigation_workspace.png'), fullPage: true });
    verifyAndSave('07_investigation_workspace.png');

    console.log('\n[Success] All 7 fresh screenshots captured, verified, and saved!');
  } finally {
    await browser.close();
  }
}

run().catch((err) => {
  console.error('[Error] Capture failed:', err);
  process.exit(1);
});
