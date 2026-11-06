#!/usr/bin/env node
// Exports gloss-tray-icon.svg to app/tray.png, the menu bar's template image:
// 36 pixels for its 18 points, black and transparent. Chrome draws it, as it
// does for the baked icons (CHROME overrides the path); the app needs neither,
// the result is checked in.
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const out = path.resolve(here, '../../app/tray.png');
const chrome = process.env.CHROME || [
  '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
  '/Applications/Chromium.app/Contents/MacOS/Chromium',
  '/usr/bin/google-chrome',
  '/usr/bin/chromium'
].find((p) => fs.existsSync(p));
if (!chrome) { console.error('Chrome not found. Set CHROME to the browser binary.'); process.exit(1); }

const size = 36;
const svg = fs.readFileSync(path.join(here, 'gloss-tray-icon.svg'));
const src = `data:image/svg+xml;base64,${svg.toString('base64')}`;
const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'gloss-tray-'));
const page = path.join(dir, 'export.html');
fs.writeFileSync(page, `<!doctype html><pre id="out"></pre><script>
const img = new Image();
img.onload = () => {
  const c = document.createElement('canvas');
  c.width = c.height = ${size};
  c.getContext('2d').drawImage(img, 0, 0, ${size}, ${size});
  document.getElementById('out').textContent = c.toDataURL('image/png');
};
img.src = ${JSON.stringify(src)};
</script>`);
const dom = execFileSync(chrome, ['--headless=new', '--disable-gpu', `--user-data-dir=${path.join(dir, 'profile')}`, '--virtual-time-budget=3000', '--dump-dom', `file://${page}`], { encoding: 'utf8', maxBuffer: 1 << 24, stdio: ['ignore', 'pipe', 'ignore'] });
fs.rmSync(dir, { recursive: true, force: true });
const found = dom.match(/data:image\/png;base64,([A-Za-z0-9+/=]+)/);
if (!found) { console.error('Chrome gave no image.'); process.exit(1); }
fs.writeFileSync(out, Buffer.from(found[1], 'base64'));
console.log(`${path.relative(process.cwd(), out)}: ${fs.statSync(out).size} bytes`);
