// 外観設定ダイアログとヘルプ(ショートカット一覧・スポンサーリンク)ダイアログ。
import { BrowserOpenURL, EventsOn } from '../wailsjs/runtime/runtime';
import {
  PickImage, PickSSHConfig, ImportSSHConfig, AppVersion, CheckForUpdate, DownloadUpdate, InstallUpdate, OpenReleasePage,
} from '../wailsjs/go/main/App';
import {
  THEMES, ACCENT_PRESETS, OPACITY_RANGE, LINKS, PRESET_IMAGES, DEFAULT_IMAGE_OPACITY,
  getSettings, getTheme, update, reset, currentColors, onAppearanceChange,
} from './appearance.js';

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text != null) e.textContent = text;
  return e;
}

const field = (label, ...nodes) => {
  const wrap = el('div', 'field');
  if (label === null) wrap.append(...nodes);
  else wrap.append(el('div', 'field-label', label), ...nodes);
  return wrap;
};

function colorRow(value, presets, onPick, onClear, clearLabel) {
  const row = el('div', 'color-row');
  const input = el('input');
  input.type = 'color';
  input.value = value;
  input.addEventListener('input', () => onPick(input.value));
  row.append(input);
  for (const c of presets) {
    const sw = el('button', 'swatch');
    sw.type = 'button';
    sw.style.background = c;
    sw.title = c;
    sw.addEventListener('click', () => onPick(c));
    row.append(sw);
  }
  const clear = el('button', 'link-btn', clearLabel);
  clear.type = 'button';
  clear.addEventListener('click', onClear);
  row.append(clear);
  return row;
}

let APP_VERSION = '';
AppVersion().then((v) => { APP_VERSION = v; });

let pendingUpdateCheck = false;

export function initSettings({ shortcutRows, activeSession, saveLook }) {
  const dialog = $('#app-dialog');
  const body = $('#ad-body');
  const tabsNav = $('#ad-tabs');
  const TABS = [
    ['appearance', 'Appearance'],
    ['sshconfig', 'SSH Config'],
    ['shortcuts', 'Shortcuts'],
    ['about', 'About'],
  ];
  let current = 'appearance';
  let scope = 'global'; // 'global' | 'session'

  const open = (url) => BrowserOpenURL(url);

  // 設定項目のラベル。セッションで上書き中なら印と「Global に戻す」を付ける
  const label = (text, overridden, onReset) => {
    const l = el('div', 'field-label', text);
    if (overridden) {
      l.append(el('span', 'badge', '● Modified for this session'));
      const b = el('button', 'link-btn', 'Reset to Global');
      b.type = 'button';
      b.addEventListener('click', onReset);
      l.append(b);
    }
    return l;
  };

  function renderSettings() {
    const session = activeSession();
    if (!session) scope = 'global';
    const inSession = scope === 'session';
    const look = (inSession && session.look) || {};
    const g = getSettings();
    const colors = currentColors(inSession ? look : undefined);
    body.replaceChildren();

    // 値の保存先。Global は localStorage、セッションは接続先ごとの look
    const setLook = async (patch) => {
      const next = { ...look, ...patch };
      for (const k of Object.keys(next)) if (!next[k]) delete next[k];
      await saveLook(session, Object.keys(next).length ? next : null);
      render();
    };
    const set = (globalPatch, lookPatch) => (inSession ? setLook(lookPatch) : update(globalPatch));

    const switcher = el('div', 'scope-switch');
    for (const [id, text] of [['global', 'Global'], ['session', session ? `Session: ${session.name}` : 'Session (no active session)']]) {
      const b = el('button', null, text);
      b.type = 'button';
      b.title = text;
      b.disabled = id === 'session' && !session;
      b.classList.toggle('active', id === scope);
      b.addEventListener('click', () => { scope = id; render(); });
      switcher.append(b);
    }
    body.append(switcher, el('div', 'scope-note', inSession
      ? `Changes apply only to “${session.name}”. Items that are not modified follow Global.`
      : 'Changes apply to every session, unless a session overrides them.'));

    const grid = el('div', 'theme-grid');
    const themes = inSession ? [{ id: '', label: 'Same as Global', bg: '', fg: '', accent: '', inherit: true }, ...THEMES] : THEMES;
    const activeTheme = inSession ? look.theme ?? '' : g.theme;
    for (const t of themes) {
      const b = el('button', t.inherit ? 'theme-card inherit' : 'theme-card');
      b.type = 'button';
      if (t.id === activeTheme) b.classList.add('active');
      b.style.setProperty('--c-bg', t.bg);
      b.style.setProperty('--c-fg', t.fg);
      b.style.setProperty('--c-accent', t.accent);
      b.innerHTML = '<span class="chip"><i></i><i></i><i></i></span>';
      b.append(el('span', 'name', t.label));
      b.addEventListener('click', () => set({ theme: t.id }, { theme: t.id }));
      grid.append(b);
    }
    body.append(field(null, label('Color theme', inSession && look.theme, () => setLook({ theme: '' })), grid));

    body.append(field(null, label('Accent color', inSession && look.accent, () => setLook({ accent: '' })), colorRow(
      colors.accent, ACCENT_PRESETS,
      (c) => set({ accent: c }, { accent: c }),
      () => set({ accent: null }, { accent: '' }),
      inSession ? 'Same as Global' : 'Use theme color',
    )));

    body.append(field(null, label('Background color', inSession && look.background, () => setLook({ background: '' })), colorRow(
      colors.bg, [],
      (c) => set({ background: c }, { background: c }),
      () => set({ background: null }, { background: '' }),
      inSession ? 'Same as Global' : 'Use theme color',
    )));

    if (inSession) {
      // 背景画像は接続先ごとの設定
      const image = look.image ?? '';
      const select = el('select');
      select.append(el('option', null, 'None'), ...PRESET_IMAGES.map((i) => {
        const o = el('option', null, i.label);
        o.value = `preset:${i.id}`;
        return o;
      }));
      select.firstChild.value = '';
      if (image && !image.startsWith('preset:')) {
        const o = el('option', null, image);
        o.value = image;
        select.append(o);
      }
      select.value = image;
      select.addEventListener('change', () => setLook({ image: select.value, imageOpacity: select.value ? look.imageOpacity : 0 }));
      const browse = el('button', null, 'Browse…');
      browse.type = 'button';
      browse.addEventListener('click', async () => {
        const path = await PickImage();
        if (path) setLook({ image: path });
      });
      const row = el('div', 'color-row');
      select.classList.add('grow');
      row.append(select, browse);
      body.append(field(null, label('Background image', image, () => setLook({ image: '', imageOpacity: 0 })), row));

      if (image) {
        const strength = look.imageOpacity || DEFAULT_IMAGE_OPACITY;
        const r = el('input');
        r.type = 'range';
        r.min = 5;
        r.max = 100;
        r.value = strength;
        const out = el('span', 'range-out', `${strength}%`);
        r.addEventListener('input', () => { out.textContent = `${r.value}%`; });
        r.addEventListener('change', () => setLook({ imageOpacity: Number(r.value) }));
        const rr = el('div', 'range-row');
        rr.append(r, out);
        body.append(field('Image strength', rr));
      }
      return;
    }

    const range = el('input');
    range.type = 'range';
    range.min = OPACITY_RANGE.min;
    range.max = OPACITY_RANGE.max;
    range.value = g.opacity;
    const out = el('span', 'range-out', `${g.opacity}%`);
    range.addEventListener('input', () => {
      out.textContent = `${range.value}%`;
      update({ opacity: Number(range.value) });
    });
    const rangeRow = el('div', 'range-row');
    rangeRow.append(range, out);
    body.append(field('Background opacity (all sessions)', rangeRow));
    body.append(el('div', 'hint', 'Opacity needs a compositor that supports window transparency (macOS, Windows 11). Background images are set per session: switch to the Session tab.'));
  }

  function renderSSHConfig() {
    body.replaceChildren();
    const KEY = 'jterm.sshConfigPath';
    let path = '';
    try { path = localStorage.getItem(KEY) || ''; } catch { /* 保存できなくても動く */ }

    const input = el('input', 'grow');
    input.type = 'text';
    input.readOnly = true;
    input.placeholder = 'No file selected';
    input.value = path;
    const browse = el('button', null, 'Browse…');
    browse.type = 'button';
    const importBtn = el('button', null, 'Import');
    importBtn.type = 'button';
    importBtn.disabled = !path;
    const status = el('div', 'hint');

    browse.addEventListener('click', async () => {
      const picked = await PickSSHConfig();
      if (!picked) return;
      path = picked;
      input.value = path;
      importBtn.disabled = false;
      status.textContent = '';
      try { localStorage.setItem(KEY, path); } catch { /* 同上 */ }
    });
    importBtn.addEventListener('click', async () => {
      try {
        const r = await ImportSSHConfig(path);
        let text = `Imported ${r.added}, skipped ${r.skipped} existing.`;
        if (r.unsupported) text += ` ${r.unsupported} use ProxyJump/ProxyCommand, which is not supported yet.`;
        status.textContent = text;
      } catch (e) {
        status.textContent = `Import failed: ${e}`;
      }
    });

    const row = el('div', 'color-row');
    row.append(input, browse, importBtn);
    body.append(
      field('ssh config file', row),
      el('div', 'hint', 'Choose your ssh_config file (usually ~/.ssh/config, but the location depends on your environment), then import its Host entries as sessions.'),
      status,
    );
  }

  function renderShortcuts() {
    const hb = body;
    hb.replaceChildren();
    const table = el('table', 'shortcut-table');
    for (const [k, v] of shortcutRows()) {
      const tr = el('tr');
      const td = el('td');
      td.append(el('kbd', null, k));
      tr.append(td, el('td', null, v));
      table.append(tr);
    }
    hb.append(table);
  }

  function renderAbout() {
    const hb = body;
    hb.replaceChildren();
    const about = el('div', 'about');
    about.append(
      el('div', 'about-name', `jterm v${APP_VERSION}`),
      el('div', 'hint', 'A tabbed SSH terminal with split panes. Built and maintained by jojonki.'),
    );
    const links = el('div', 'about-links');
    for (const [label, url, cls] of [
      ['♥ Sponsor on GitHub', LINKS.sponsor, 'sponsor'],
      ['Repository', LINKS.repo, ''],
      ['@jojonki', LINKS.developer, ''],
    ]) {
      const b = el('button', cls, label);
      b.type = 'button';
      b.addEventListener('click', () => open(url));
      links.append(b);
    }
    about.append(links);

    const upd = el('div', 'about-update');
    const status = el('div', 'hint');
    const btn = el('button', '', 'Check for Updates');
    btn.type = 'button';
    btn.addEventListener('click', () => checkUpdate(status, btn));
    upd.append(btn, status);
    about.append(upd);
    hb.append(about);
    if (pendingUpdateCheck) {
      pendingUpdateCheck = false;
      checkUpdate(status, btn);
    }
  }

  // 最新リリースを確認し、新しい版があれば更新ボタンに切り替える
  async function checkUpdate(status, btn) {
    btn.disabled = true;
    status.textContent = 'Checking…';
    try {
      const info = await CheckForUpdate();
      if (!info.available) {
        status.textContent = `You are up to date (v${info.current}).`;
        btn.disabled = false;
        return;
      }
      if (!info.assetName) {
        status.textContent = `v${info.latest} is available (you have v${info.current}).`;
        btn.textContent = 'Open release page';
        btn.disabled = false;
        btn.onclick = () => OpenReleasePage();
        return;
      }
      status.textContent = `v${info.latest} is available (you have v${info.current}). jterm will restart, closing open connections.`;
      btn.textContent = `Update to v${info.latest} and Restart`;
      btn.disabled = false;
      btn.onclick = () => installUpdate(info, status, btn);
    } catch (e) {
      status.textContent = `Could not check for updates: ${e}`;
      btn.disabled = false;
    }
  }

  // 更新を入れて再起動する。置き換えられないときは、ダウンロードだけに切り替える
  async function installUpdate(info, status, btn) {
    btn.disabled = true;
    status.textContent = 'Downloading…';
    const offs = [
      EventsOn('update:progress', (pct) => { status.textContent = `Downloading… ${pct}%`; }),
      EventsOn('update:installing', () => { status.textContent = 'Installing…'; }),
    ];
    try {
      await InstallUpdate();
      status.textContent = 'Restarting…';
    } catch (e) {
      status.textContent = `Could not update automatically: ${e}`;
      btn.textContent = `Download v${info.latest}`;
      btn.disabled = false;
      btn.onclick = () => downloadUpdate(status, btn);
    } finally {
      offs.forEach((off) => off());
    }
  }

  async function downloadUpdate(status, btn) {
    btn.disabled = true;
    status.textContent = 'Downloading…';
    const off = EventsOn('update:progress', (pct) => { status.textContent = `Downloading… ${pct}%`; });
    try {
      const path = await DownloadUpdate();
      status.textContent = `Saved to ${path}. Quit jterm and replace it with the new version.`;
    } catch (e) {
      status.textContent = `Download failed: ${e}`;
      btn.textContent = 'Open release page';
      btn.onclick = () => OpenReleasePage();
    } finally {
      off();
    }
    btn.disabled = false;
  }

  function render() {
    tabsNav.replaceChildren(...TABS.map(([id, label]) => {
      const b = el('button', 'tab-btn', label);
      b.type = 'button';
      b.setAttribute('role', 'tab');
      b.classList.toggle('active', id === current);
      b.addEventListener('click', () => show(id));
      return b;
    }));
    ({ appearance: renderSettings, sshconfig: renderSSHConfig, shortcuts: renderShortcuts, about: renderAbout })[current]();
    const resetBtn = $('#ad-reset');
    resetBtn.hidden = current !== 'appearance';
    resetBtn.textContent = scope === 'session' && activeSession() ? 'Reset this session' : 'Reset Global appearance';
  }

  function show(id, nextScope) {
    current = id;
    if (nextScope) scope = nextScope;
    render();
    if (!dialog.open) dialog.showModal();
  }

  // テーマ切替などで見た目が変わったとき、開いている設定画面の選択状態を更新する
  onAppearanceChange(() => { if (dialog.open && current === 'appearance') render(); });

  $('#ad-close').addEventListener('click', () => dialog.close());
  $('#ad-reset').addEventListener('click', async () => {
    const session = activeSession();
    if (scope === 'session' && session) await saveLook(session, null);
    else reset();
    render();
  });

  return {
    showSettings: () => show('appearance', activeSession() ? 'session' : 'global'),
    showSessionSettings: () => show('appearance', 'session'),
    showHelp: () => show('shortcuts'),
    showAbout: () => show('about'),
    showUpdate: () => { pendingUpdateCheck = true; show('about'); },
    get version() { return APP_VERSION; },
    themeLabel: () => getTheme().label,
  };
}

function $(sel) { return document.querySelector(sel); }
