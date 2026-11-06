#!/usr/bin/env node
// Bakes the icons of gloss-icons.json for the app: mygo runs no JS and draws no
// SVG filters, so each icon's hover motion is sampled into frames of plain SVG,
// recoloured for the ink ground, and written to app/icons/<id>.svgz, one frame
// per line, gzipped. See docs/decisions/0012-baked-icons.md.
//
//   node scripts/bake.mjs [--frames 24] [--out ../../app/icons]
//
// Needs Node 18+ and Chrome (CHROME overrides the path). Run from anywhere; the
// app needs neither: the files it embeds are checked in.
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import zlib from 'node:zlib';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const args = process.argv.slice(2);
const opt = (name, fallback) => { const i = args.indexOf(name); return i >= 0 ? args[i + 1] : fallback; };
const frames = Number(opt('--frames', 24));
const out = path.resolve(root, opt('--out', '../../app/icons'));
const manifest = JSON.parse(fs.readFileSync(path.join(root, 'gloss-icons.json'), 'utf8'));

const chrome = process.env.CHROME || [
  '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
  '/Applications/Chromium.app/Contents/MacOS/Chromium',
  '/usr/bin/google-chrome',
  '/usr/bin/chromium'
].find((p) => fs.existsSync(p));
if (!chrome) { console.error('Chrome not found. Set CHROME to the browser binary.'); process.exit(1); }

// ---------- Colour for the ink ground ----------
// The engine paints for a light page: bodies lifted toward white, content pure
// white. On ink that glares. Here each colour keeps its hue and takes a place by
// its role, read from how far it sits between the accent and white: bodies go
// dark, content stays the lightest thing, and hover (q = 1) lifts it all a step.
const s2l = (c) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);
const l2s = (c) => { c = Math.max(0, Math.min(1, c)); return c <= 0.0031308 ? 12.92 * c : 1.055 * c ** (1 / 2.4) - 0.055; };
function toOk(hex) {
  const [r, g, b] = [1, 3, 5].map((i) => s2l(parseInt(hex.slice(i, i + 2), 16) / 255));
  const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b);
  const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
  const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);
  const a = 1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s;
  const bb = 0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s;
  return { L: 0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s, C: Math.hypot(a, bb), H: Math.atan2(bb, a) };
}
function okHex({ L, C, H }) {
  const a = C * Math.cos(H), b = C * Math.sin(H);
  const l = (L + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m = (L - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s = (L - 0.0894841775 * a - 1.291485548 * b) ** 3;
  const rgb = [4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s, -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s, -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s];
  return '#' + rgb.map((v) => Math.round(l2s(v) * 255).toString(16).padStart(2, '0')).join('').toUpperCase();
}
function onInk(hex, accent, q, chroma) {
  const { L } = toOk(hex);
  const A = accent;
  const toHex = ({ L, C, H }) => okHex({ L, C: C * chroma, H });
  const amt = L >= A.L ? (L - A.L) / (1 - A.L) : (L - A.L) / A.L;
  if (amt > 0.97) return toHex({ L: 0.84 + 0.08 * q, C: 0.012 + 0.006 * q, H: A.H }); // content and marks
  if (amt < -0.02) return toHex({ L: 0.34 + 0.03 * q, C: 0.05 + 0.015 * q, H: A.H });  // deep marks
  if (amt < 0.02) return toHex({ L: 0.6 + 0.1 * q, C: 0.07 + 0.02 * q, H: A.H });     // outline
  return toHex({ L: 0.4 + 0.3 * amt + 0.06 * q, C: 0.08 - 0.03 * amt + 0.018 * q, H: A.H }); // fills
}
// A dash that has not started yet draws nothing in a browser, but a round cap
// leaves a dot in mygo: such a stroke simply is not in that frame.
function dropUndrawn(svg) {
  return svg.replace(/<path\b[^>]*>\s*<\/path>/g, (el) => {
    const len = /stroke-dasharray="([\d.]+)"/.exec(el), off = /stroke-dashoffset="([\d.]+)"/.exec(el);
    return len && off && Number(off[1]) >= Number(len[1]) ? '' : el;
  });
}

const expand = (h) => (h.length === 4 ? '#' + [...h.slice(1)].map((c) => c + c).join('') : h);
function recolour(svg, accentHex, q, chroma) {
  const accent = toOk(accentHex);
  // Masks cut the outside strokes by luminance: their black and white must stay.
  return svg.split(/(<mask[\s\S]*?<\/mask>)/).map((part, i) => (i % 2
    ? part
    : part.replace(/#[0-9A-Fa-f]{6}\b|#[fF]{3}\b/g, (h) => onInk(expand(h), accent, q, chroma)))).join('');
}

// ---------- Sampling, in the engine's own page ----------
const read = (f) => fs.readFileSync(path.join(root, f), 'utf8');
const js = [read('src/engine.js'), read('src/icons.js'), read('src/app.js')].join('\n');
const script = `const opts = ${JSON.stringify({ manifest, frames })};
  const pal = PALETTES[0];
  runtime.palette = pal;
  const host = document.createElement('div'); document.body.appendChild(host);
  const result = opts.manifest.map((entry, n) => {
    const icon = ICONS.find((x) => x.name === entry.icon);
    if (!icon) throw new Error('Unknown icon: ' + entry.icon);
    const i = ORDER.length + n;
    pal.colors[i] = entry.accent;
    const cell = document.createElement('span'); host.appendChild(cell);
    const inst = mountIcon(icon, i, cell);
    const from = targetOf(inst);
    inst.hover = 1;
    const to = targetOf(inst);
    const live = cell.querySelector('svg');
    const out = [];
    for (let f = 0; f < opts.frames; f++) {
      inst.cur = interp(from, to, f / (opts.frames - 1));
      render(inst);
      const svg = live.cloneNode(true);
      const lives = [...live.querySelectorAll('.ol, .ln')];
      [...svg.querySelectorAll('.ol, .ln')].forEach((el, j) => {
        const cs = getComputedStyle(lives[j]);
        el.setAttribute('fill', 'none');
        el.setAttribute('stroke-width', parseFloat(cs.strokeWidth));
        if (cs.strokeLinecap !== 'butt') el.setAttribute('stroke-linecap', cs.strokeLinecap);
        el.removeAttribute('class');
      });
      svg.querySelectorAll('filter').forEach((el) => el.remove());
      svg.querySelectorAll('[filter]').forEach((el) => el.removeAttribute('filter'));
      svg.querySelectorAll('*').forEach((el) => [...el.attributes].forEach((a) => { if (a.name.startsWith('data-')) el.removeAttribute(a.name); }));
      out.push(svg.outerHTML);
    }
    return { id: entry.id, accent: entry.accent, frames: out };
  });
  const tag = document.createElement('script'); tag.type = 'application/json'; tag.id = 'bake';
  tag.textContent = JSON.stringify(result); document.body.appendChild(tag);`;
const page = read('index.html')
  .replace('<head>', '<head><script>window.addEventListener("error",(e)=>{document.documentElement.dataset.err=e.message;});</script>')
  .replace(/<script src="src\/engine\.js"><\/script>\s*<script src="src\/icons\.js"><\/script>\s*<script src="src\/app\.js"><\/script>/,
    `<script>\n${js}\n</script><script>{${script}}</script>`);

const work = fs.mkdtempSync(path.join(os.tmpdir(), 'gloss-bake-'));
let dom;
try {
  const file = path.join(work, 'bake.html');
  fs.writeFileSync(file, page);
  dom = execFileSync(chrome, ['--headless=new', '--disable-gpu', '--force-prefers-reduced-motion', `--user-data-dir=${path.join(work, 'profile')}`,
    '--virtual-time-budget=3000', '--dump-dom', 'file://' + file],
  { encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'], maxBuffer: 1024 * 1024 * 1024 });
} finally {
  fs.rmSync(work, { recursive: true, force: true });
}
const err = dom.match(/data-err="([^"]*)"/);
if (err) { console.error(err[1]); process.exit(1); }
const json = dom.match(/<script type="application\/json" id="bake">([\s\S]*?)<\/script>/);
if (!json) { console.error('The page produced no icons.'); process.exit(1); }

// The padded box holds what moves past the canvas, as a bin's lid (see visualSize in app/icons.go).
const VIEW = 'viewBox="-2.5 -5.5 36 36"';
fs.mkdirSync(out, { recursive: true });
for (const f of fs.readdirSync(out)) if (f.endsWith('.svgz')) fs.rmSync(path.join(out, f));
let raw = 0, packed = 0;
for (const { id, accent, frames: list } of JSON.parse(json[1])) {
  // chroma scales how much colour the icon keeps, for hues that read stronger on ink at the same value.
  const chroma = manifest.find((m) => m.id === id).chroma ?? 1;
  const text = list.map((svg, f) => recolour(dropUndrawn(svg), accent, f / (list.length - 1), chroma)
    .replace(/\saria-hidden="true"/, '')
    .replace(/<svg viewBox="[^"]*"/, `<svg xmlns="http://www.w3.org/2000/svg" ${VIEW}`)
    .replace(/>\s+</g, '><')
    .replace(/\n\s*/g, ' ')).join('\n');
  const gz = zlib.gzipSync(text, { level: 9 });
  fs.writeFileSync(path.join(out, `${id}.svgz`), gz);
  raw += text.length; packed += gz.length;
}
console.log(`${manifest.length} icons × ${frames} frames: ${(raw / 1024).toFixed(0)} KB raw, ${(packed / 1024).toFixed(1)} KB gzipped → ${path.relative(process.cwd(), out)}`);
