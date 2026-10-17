// 外観設定: カラーテーマ、アクセント色、背景色、透過度。
// 設定は localStorage に保存し、CSS 変数と xterm のテーマへ反映する。

import moonGirl from './assets/bg/moon-girl.svg?url';
import { LoadImage } from '../wailsjs/go/main/App';

const STORAGE_KEY = 'jterm.appearance';

const ANSI_DARK = {
  black: '#3b4252', red: '#f85149', green: '#3fb950', yellow: '#d29922',
  blue: '#58a6ff', magenta: '#bc8cff', cyan: '#39c5cf', white: '#d0d7de',
  brightBlack: '#6e7681', brightRed: '#ff7b72', brightGreen: '#56d364', brightYellow: '#e3b341',
  brightBlue: '#79c0ff', brightMagenta: '#d2a8ff', brightCyan: '#56d4dd', brightWhite: '#ffffff',
};

const ANSI_LIGHT = {
  black: '#24292f', red: '#cf222e', green: '#1a7f37', yellow: '#9a6700',
  blue: '#0969da', magenta: '#8250df', cyan: '#1b7c83', white: '#6e7781',
  brightBlack: '#57606a', brightRed: '#a40e26', brightGreen: '#2da44e', brightYellow: '#bf8700',
  brightBlue: '#218bff', brightMagenta: '#a475f9', brightCyan: '#3192aa', brightWhite: '#8c959f',
};

export const THEMES = [
  { id: 'dark', label: 'Dark', dark: true, bg: '#1e1e1e', fg: '#cccccc', accent: '#0e7ad3' },
  { id: 'light', label: 'Light', dark: false, bg: '#ffffff', fg: '#24292f', accent: '#0969da', ansi: ANSI_LIGHT },
  { id: 'solarized-dark', label: 'Solarized Dark', dark: true, bg: '#002b36', fg: '#93a1a1', accent: '#268bd2' },
  { id: 'solarized-light', label: 'Solarized Light', dark: false, bg: '#fdf6e3', fg: '#586e75', accent: '#268bd2', ansi: ANSI_LIGHT },
  { id: 'nord', label: 'Nord', dark: true, bg: '#2e3440', fg: '#d8dee9', accent: '#88c0d0' },
  { id: 'dracula', label: 'Dracula', dark: true, bg: '#282a36', fg: '#f8f8f2', accent: '#bd93f9' },
  { id: 'gruvbox-dark', label: 'Gruvbox Dark', dark: true, bg: '#282828', fg: '#ebdbb2', accent: '#d79921' },
  { id: 'tokyo-night', label: 'Tokyo Night', dark: true, bg: '#1a1b26', fg: '#c0caf5', accent: '#7aa2f7' },
  { id: 'rose-pine', label: 'Rosé Pine', dark: true, bg: '#191724', fg: '#e0def4', accent: '#c4a7e7' },
  { id: 'catppuccin-latte', label: 'Catppuccin Latte', dark: false, bg: '#eff1f5', fg: '#4c4f69', accent: '#1e66f5', ansi: ANSI_LIGHT },
  { id: 'synthwave', label: 'Synthwave', dark: true, bg: '#262335', fg: '#f4eee4', accent: '#ff7edb' },
];

export const ACCENT_PRESETS = [
  '#0e7ad3', '#3fb950', '#8250df', '#bf3989', '#f85149', '#d29922', '#39c5cf', '#8b949e',
];

// 接続先の背景に選べる同梱画像
export const PRESET_IMAGES = [
  { id: 'moon-girl', label: 'Moonlit girl', url: moonGirl },
];

export const DEFAULT_IMAGE_OPACITY = 35;

export const DEFAULTS = { theme: 'dark', accent: null, background: null, opacity: 100 };

export const OPACITY_RANGE = { min: 20, max: 100 };

export const LINKS = {
  repo: 'https://github.com/jojonki/jterm',
  developer: 'https://github.com/jojonki',
  sponsor: 'https://github.com/sponsors/jojonki',
};

// ---------------------------------------------------------------------------
// 色の計算

export function normalizeHex(color) {
  if (typeof color !== 'string') return null;
  const v = color.trim();
  if (/^#[0-9a-f]{6}$/i.test(v)) return v.toLowerCase();
  if (/^#[0-9a-f]{3}$/i.test(v)) return `#${[...v.slice(1)].map((c) => c + c).join('')}`.toLowerCase();
  return null;
}

const toRgb = (hex) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16));
const toHex = (rgb) => `#${rgb.map((c) => Math.round(c).toString(16).padStart(2, '0')).join('')}`;

/** a に b を ratio (0..1) だけ混ぜる */
function mix(a, b, ratio) {
  const [ra, rb] = [toRgb(a), toRgb(b)];
  return toHex(ra.map((c, i) => c + (rb[i] - c) * ratio));
}

function rgba(hex, alpha) {
  const [r, g, b] = toRgb(hex);
  return `rgba(${r}, ${g}, ${b}, ${alpha})`;
}

/** 背景色に対して読みやすい前景色(黒か白) */
function readableOn(hex) {
  const [r, g, b] = toRgb(hex).map((c) => c / 255);
  const ch = (c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);
  const lum = 0.2126 * ch(r) + 0.7152 * ch(g) + 0.0722 * ch(b);
  return (lum + 0.05) / 0.05 >= 1.05 / (lum + 0.05) ? '#101215' : '#ffffff';
}

// ---------------------------------------------------------------------------
// 設定の読み書きと適用

function normalize(raw) {
  const s = { ...DEFAULTS, ...raw };
  if (!THEMES.some((t) => t.id === s.theme)) s.theme = DEFAULTS.theme;
  s.accent = normalizeHex(s.accent);
  s.background = normalizeHex(s.background);
  const op = Number(s.opacity);
  s.opacity = Number.isFinite(op)
    ? Math.min(OPACITY_RANGE.max, Math.max(OPACITY_RANGE.min, Math.round(op)))
    : DEFAULTS.opacity;
  return s;
}

function load() {
  try {
    return normalize(JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}'));
  } catch {
    return normalize({});
  }
}

let state = load();
const listeners = new Set();

export const getSettings = () => ({ ...state });
export const getTheme = (id = state.theme) => THEMES.find((t) => t.id === id) ?? THEMES[0];

/** 現在の設定(look があれば接続先の設定で上書き)から導いた実効色 */
function palette(look) {
  const theme = getTheme(look?.theme || state.theme);
  // 接続先でテーマを選んでいるときは、全体設定のカスタム色には従わず、選んだテーマの色を使う
  // (全体と同じテーマを選んでいても同様。そうしないと全体側の変更で接続先の設定が上書きされて見える)
  const own = Boolean(look?.theme);
  const bg = normalizeHex(look?.background) ?? (own ? theme.bg : state.background ?? theme.bg);
  const fg = theme.fg;
  const accent = normalizeHex(look?.accent) ?? (own ? theme.accent : state.accent ?? theme.accent);
  const toward = theme.dark ? '#ffffff' : '#000000';
  return {
    theme, bg, fg, accent, toward,
    side: mix(bg, toward, 0.04),
    tabbar: mix(bg, toward, 0.08),
    hover: mix(bg, toward, 0.1),
    input: mix(bg, toward, 0.12),
    border: mix(bg, toward, 0.16),
    dim: mix(fg, bg, 0.45),
    select: mix(bg, accent, 0.35),
  };
}

/** xterm.js に渡すテーマ。背景画像があるときは画像が透けるよう端末の背景を透明にする */
export function xtermTheme(look) {
  const p = palette(look);
  const alpha = hasImage(look) ? 0 : state.opacity / 100;
  return {
    ...(p.theme.dark ? ANSI_DARK : ANSI_LIGHT),
    background: rgba(p.bg, alpha),
    foreground: p.fg,
    cursor: p.fg,
    cursorAccent: p.bg,
    selectionBackground: rgba(p.accent, 0.4),
  };
}

const hasImage = (look) => Boolean(look?.image);

/** palette から CSS 変数の一覧を作る */
function cssVars(p, a) {
  return {
    '--bg': rgba(p.bg, a),
    '--bg-solid': p.bg,
    '--bg-side': rgba(p.side, a),
    '--bg-tabbar': rgba(p.tabbar, a),
    '--bg-tab': rgba(p.tabbar, a),
    '--bg-tab-active': rgba(p.bg, a),
    '--bg-hover': rgba(p.hover, a),
    '--bg-select': rgba(p.select, Math.max(a, 0.6)),
    '--bg-input': rgba(p.input, Math.max(a, 0.6)),
    '--bg-popup': mix(p.bg, p.toward, 0.05),
    '--border': rgba(p.border, Math.max(a, 0.6)),
    '--fg': p.fg,
    '--fg-dim': p.dim,
    '--accent': p.accent,
    '--accent-fg': readableOn(p.accent),
    '--kbd-bg': rgba(p.input, 1),
  };
}

export function applyAppearance() {
  const p = palette();
  const root = document.documentElement;
  for (const [k, v] of Object.entries(cssVars(p, state.opacity / 100))) root.style.setProperty(k, v);
  root.style.colorScheme = p.theme.dark ? 'dark' : 'light';
  root.dataset.theme = state.theme;
  for (const fn of listeners) fn();
}

const PANE_VARS = [...Object.keys(cssVars(palette(), 1)), '--pane-bg', '--pane-image', '--pane-image-opacity'];

/**
 * 接続先ごとの見た目をペインに適用する。look が空なら全体設定のまま。
 * CSS 変数をペインの要素に置くので、配下の UI だけが接続先の色になる。
 */
export async function applyLook(el, look, token) {
  const clear = () => { for (const k of PANE_VARS) el.style.removeProperty(k); };
  const customized = look && (look.theme || look.accent || look.background || look.image);
  if (!customized) { clear(); el.classList.remove('has-image'); return; }
  const p = palette(look);
  const a = state.opacity / 100;
  const vars = cssVars(p, a);
  const url = await imageUrl(look);
  if (token && !token()) return; // 画像の読み込み中に別の適用が走った
  if (url) {
    // 端末の背景は透明にし、色は画像の下の面(--pane-bg)で表現する
    vars['--pane-bg'] = rgba(p.bg, a);
    vars['--bg'] = rgba(p.bg, 0);
    vars['--pane-image'] = `url("${url}")`;
    vars['--pane-image-opacity'] = String((look.imageOpacity || DEFAULT_IMAGE_OPACITY) / 100);
  }
  clear();
  for (const [k, v] of Object.entries(vars)) el.style.setProperty(k, v);
  el.classList.toggle('has-image', Boolean(url));
}

const imageCache = new Map();

/** look.image を <img>/CSS で使える URL にする。読めなければ null */
export async function imageUrl(look) {
  const ref = look?.image;
  if (!ref) return null;
  if (ref.startsWith('preset:')) return PRESET_IMAGES.find((i) => i.id === ref.slice(7))?.url ?? null;
  if (!imageCache.has(ref)) imageCache.set(ref, LoadImage(ref).catch(() => null));
  return imageCache.get(ref);
}

export function update(patch) {
  state = normalize({ ...state, ...patch });
  try { localStorage.setItem(STORAGE_KEY, JSON.stringify(state)); } catch { /* 保存できなくても動作は続ける */ }
  applyAppearance();
}

export const reset = () => update({ ...DEFAULTS });

/** 設定が変わったときに呼ばれる(xterm の更新用) */
export function onAppearanceChange(fn) {
  listeners.add(fn);
}

export function currentColors(look) {
  const p = palette(look);
  return { bg: p.bg, accent: p.accent, themeBg: p.theme.bg, themeAccent: p.theme.accent };
}
