import { existsSync, mkdirSync } from 'node:fs';
import { resolve } from 'node:path';
import { expect, test } from '@playwright/test';

test.use({ locale: 'en-US', viewport: { width: 1280, height: 640 } });

test('capture social preview image', async ({ page }) => {
  const htmlPath = resolve(process.cwd(), '..', 'docs', 'screenshots', 'social-preview.html');
  test.skip(
    !existsSync(htmlPath),
    `docs/screenshots/social-preview.html does not exist yet at ${htmlPath}; run this after it has been created.`,
  );
  const outDir = resolve(process.cwd(), '..', 'docs', 'screenshots');
  mkdirSync(outDir, { recursive: true });
  await page.goto(`file://${htmlPath}`);
  await expect(page.locator('body')).toBeVisible();
  await page.screenshot({ path: resolve(outDir, 'social-preview.png'), fullPage: false });
});
