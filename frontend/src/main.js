import '@xterm/xterm/css/xterm.css';
import './style.css';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { Unicode11Addon } from '@xterm/addon-unicode11';
import { WebLinksAddon } from '@xterm/addon-web-links';
import * as Backend from '../wailsjs/go/main/App';
import {
  EventsOn, ClipboardGetText, ClipboardSetText, BrowserOpenURL,
} from '../wailsjs/runtime/runtime';

const isMac = /Mac/i.test(navigator.platform);
const MOD_LABEL = isMac ? '⌘' : 'Ctrl+Shift+';

const TERM_FONT = '"Cascadia Mono", Consolas, "BIZ UDGothic", "MS Gothic", Menlo, "Hiragino Sans", monospace';

// ---------------------------------------------------------------------------
// 小さな DOM ヘルパ

function h(tag, props = {}, ...children) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(props)) {
    if (v == null || v === false) continue;
    if (k === 'class') el.className = v;
    else if (k === 'text') el.textContent = v;
    else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else el.setAttribute(k, v === true ? '' : v);
  }
  el.append(...children.flat().filter((c) => c != null));
  return el;
}

const $ = (sel) => document.querySelector(sel);

function b64ToBytes(b64) {
  const bin = atob(b64);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  return bytes;
}

// ---------------------------------------------------------------------------
// セッション一覧

let sessions = [];

function target(s) {
  let t = s.host;
  if (s.user) t = `${s.user}@${t}`;
  if (s.port && s.port !== 22) t += `:${s.port}`;
  return t;
}

function filterSessions(query) {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  return sessions.filter((s) => {
    const hay = `${s.name} ${target(s)}`.toLowerCase();
    return words.every((w) => hay.includes(w));
  });
}

function highlight(text, query) {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (!words.length) return [text];
  const lower = text.toLowerCase();
  const marks = new Array(text.length).fill(false);
  for (const w of words) {
    let i = lower.indexOf(w);
    while (i >= 0) {
      marks.fill(true, i, i + w.length);
      i = lower.indexOf(w, i + 1);
    }
  }
  const out = [];
  let i = 0;
  while (i < text.length) {
    let j = i;
    while (j < text.length && marks[j] === marks[i]) j++;
    const part = text.slice(i, j);
    out.push(marks[i] ? h('mark', { text: part }) : part);
    i = j;
  }
  return out;
}

function openCount(sessionId) {
  let n = 0;
  for (const tab of tabs) {
    for (const p of tab.panes()) if (p.session?.id === sessionId && p.state !== 'closed') n++;
  }
  return n;
}

// SessionList はサイドバーとペイン内ピッカーで共用する検索付きリスト。
class SessionList {
  constructor(input, listEl, { onOpen, singleClick = false }) {
    this.input = input;
    this.listEl = listEl;
    this.onOpen = onOpen;
    this.singleClick = singleClick;
    this.sel = 0;
    this.items = [];
    input.addEventListener('input', () => { this.sel = 0; this.render(); });
    input.addEventListener('keydown', (e) => this.onKey(e));
  }

  render() {
    const q = this.input.value;
    this.items = filterSessions(q);
    if (this.sel >= this.items.length) this.sel = Math.max(0, this.items.length - 1);
    const children = this.items.map((s, i) => {
      const n = openCount(s.id);
      return h('div', {
        class: `session-item${i === this.sel ? ' selected' : ''}`,
        title: target(s),
        onclick: () => {
          this.sel = i;
          this.render();
          if (this.singleClick) this.onOpen(s, 'tab');
        },
        ondblclick: () => this.onOpen(s, 'tab'),
        oncontextmenu: (e) => {
          e.preventDefault();
          this.sel = i;
          this.render();
          sessionMenu(s, e.clientX, e.clientY);
        },
      },
      h('div', { class: 'text' },
        h('div', { class: 'name' }, highlight(s.name, q)),
        h('div', { class: 'sub' }, highlight(target(s), q))),
      n > 0 ? h('span', { class: 'badge', text: String(n), title: `Open in ${n} pane(s)` }) : null);
    });
    if (!sessions.length) {
      children.push(h('div', { class: 'empty-note', text: 'No sessions yet. Click “＋ New session” to add one.' }));
    } else if (!this.items.length) {
      children.push(h('div', { class: 'empty-note', text: 'No matching sessions' }));
    }
    this.listEl.replaceChildren(...children);
    this.listEl.querySelector('.selected')?.scrollIntoView({ block: 'nearest' });
  }

  onKey(e) {
    if (e.isComposing) return;
    switch (e.key) {
      case 'ArrowDown':
        this.sel = Math.min(this.sel + 1, this.items.length - 1);
        this.render();
        e.preventDefault();
        break;
      case 'ArrowUp':
        this.sel = Math.max(this.sel - 1, 0);
        this.render();
        e.preventDefault();
        break;
      case 'Enter': {
        const s = this.items[this.sel];
        if (s) this.onOpen(s, e.shiftKey ? 'split-h' : 'tab');
        e.preventDefault();
        break;
      }
      case 'Escape':
        if (this.input.value) {
          this.input.value = '';
          this.sel = 0;
          this.render();
        } else {
          activePane()?.focus();
        }
        e.preventDefault();
        break;
    }
  }

  select(id) {
    const i = this.items.findIndex((s) => s.id === id);
    if (i >= 0) { this.sel = i; this.render(); }
  }
}

let sidebarList;

async function reloadSessions() {
  const list = await Backend.ListSessions();
  sessions = list.sort((a, b) => a.name.localeCompare(b.name, 'ja', { numeric: true }));
  sidebarList.render();
  for (const tab of tabs) for (const p of tab.panes()) p.picker?.render();
}

// ---------------------------------------------------------------------------
// ペイン: 1つの端末 = 1つの SSH 接続

let paneSeq = 0;
const connPanes = new Map(); // connID -> Pane

class Pane {
  constructor(tab) {
    this.id = `p${++paneSeq}`;
    this.tab = tab;
    this.node = null;
    this.session = null;
    this.state = 'empty'; // empty / connecting / connected / closed
    this.connId = null;
    this.attempt = 0;
    this.term = null;
    this.picker = null;
    this.unsubData = null;

    this.titleEl = h('span', { class: 'title' });
    this.dotEl = h('span', { class: 'dot empty' });
    this.header = h('div', { class: 'pane-header' },
      this.dotEl, this.titleEl,
      h('button', { text: '×', title: 'Close pane', onclick: () => closePane(this) }));
    this.body = h('div', { class: 'term-host' });
    this.bar = null;
    this.el = h('div', { class: 'pane' }, this.header, this.body);
    this.el.addEventListener('mousedown', () => setActivePane(this, false));
    this.el.addEventListener('focusin', () => setActivePane(this, false));
  }

  get title() {
    return this.session ? this.session.name : 'New tab';
  }

  showPicker(preselectId) {
    this.picker = null;
    const input = h('input', { type: 'search', placeholder: 'Search and press Enter to connect', spellcheck: 'false' });
    const listEl = h('div', { class: 'session-list' });
    const box = h('div', { class: 'picker' }, h('div', { class: 'picker-box' },
      h('h2', { text: 'Select a session' }),
      input,
      listEl,
      h('div', {}, h('button', { text: '＋ New session', onclick: () => newSessionInto(this) }))));
    this.picker = new SessionList(input, listEl, {
      singleClick: true,
      onOpen: (s) => this.connect(s),
    });
    this.picker.render();
    if (preselectId) this.picker.select(preselectId);
    this.body.replaceChildren(box);
    this.update();
    requestAnimationFrame(() => input.focus());
  }

  ensureTerm() {
    if (this.term) return;
    const term = new Terminal({
      fontFamily: TERM_FONT,
      fontSize: 14,
      cursorBlink: true,
      scrollback: 10000,
      allowProposedApi: true,
      macOptionIsMeta: true,
      cursorStyle: 'block',
      cursorInactiveStyle: 'outline',
      theme: { background: '#1e1e1e', cursor: '#ffffff', cursorAccent: '#1e1e1e' },
    });
    this.fit = new FitAddon();
    term.loadAddon(this.fit);
    term.loadAddon(new Unicode11Addon());
    term.loadAddon(new WebLinksAddon((e, uri) => {
      if (e.ctrlKey || e.metaKey) BrowserOpenURL(uri);
    }));
    term.unicode.activeVersion = '11';

    term.onData((data) => {
      if (this.state === 'connected') Backend.Write(this.connId, data);
      else if (this.state === 'closed' && (data === 'r' || data === 'R' || data === '\r')) this.reconnect();
    });
    term.onResize(({ cols, rows }) => {
      if (this.connId) Backend.Resize(this.connId, cols, rows);
    });
    // 選択したら自動でコピーする
    term.onSelectionChange(() => {
      const text = term.getSelection();
      if (text) ClipboardSetText(text);
    });

    this.termEl = h('div', { style: 'height:100%' });
    // 右クリックで貼り付け
    this.termEl.addEventListener('contextmenu', (e) => {
      e.preventDefault();
      this.paste();
    });
    this.term = term;
    new ResizeObserver(() => this.refit()).observe(this.body);
  }

  refit() {
    if (!this.term || this.fitPending) return;
    this.fitPending = true;
    requestAnimationFrame(() => {
      this.fitPending = false;
      if (this.body.offsetWidth > 0 && this.body.offsetHeight > 0) {
        try { this.fit.fit(); } catch { /* 非表示中など */ }
      }
    });
  }

  connect(session) {
    this.session = session;
    this.picker = null;
    this.ensureTerm();
    this.body.replaceChildren(this.termEl);
    if (!this.term.element) this.term.open(this.termEl);
    this.refit();
    this.start();
    this.focus();
  }

  start() {
    this.disconnect();
    this.setBar(null);
    this.connId = `${this.id}-${++this.attempt}-${crypto.randomUUID()}`;
    connPanes.set(this.connId, this);
    this.unsubData = EventsOn(`data:${this.connId}`, (b64) => this.term.write(b64ToBytes(b64)));
    this.state = 'connecting';
    // 入力できる状態になるまでカーソルを隠す
    this.term.write('\x1b[?25l');
    this.update();
    try { this.fit.fit(); } catch { /* ignore */ }
    Backend.Connect(this.connId, this.session.id, this.term.cols, this.term.rows).catch((err) => {
      this.onStatus({ state: 'closed', message: String(err) });
    });
  }

  reconnect() {
    // 編集で設定が変わっているかもしれないので最新を使う
    const latest = sessions.find((s) => s.id === this.session.id);
    if (!latest) {
      this.term.write('\r\n\x1b[31mThis session has been deleted.\x1b[0m\r\n');
      return;
    }
    this.session = latest;
    this.term.write('\r\n');
    this.start();
  }

  onStatus({ state, message }) {
    if (state === 'connected') {
      this.state = 'connected';
      this.term.write('\x1b[?25h');
      if (this === activePane() && !document.querySelector('dialog[open]')) this.focus();
    } else if (state === 'closed') {
      this.state = 'closed';
      this.term.write('\x1b[?25l');
      const msg = message ? `Disconnected: ${message}` : 'Disconnected';
      this.term.write(`\r\n\x1b[33m${msg.replace(/\r?\n/g, '\r\n')}\x1b[0m\r\n`);
      this.setBar(message ? `Disconnected: ${message.split(/\r?\n/)[0]}` : 'Disconnected');
      connPanes.delete(this.connId);
      this.unsubData?.();
      this.unsubData = null;
    }
    this.update();
  }

  setBar(msg) {
    this.bar?.remove();
    this.bar = null;
    if (msg == null) return;
    this.bar = h('div', { class: 'pane-bar' },
      h('span', { class: 'msg', text: msg, title: msg }),
      h('button', { class: 'primary', text: 'Reconnect (R)', onclick: () => this.reconnect() }),
      h('button', { text: 'Close', onclick: () => closePane(this) }));
    this.el.append(this.bar);
  }

  disconnect() {
    if (this.connId && this.state !== 'closed') Backend.Disconnect(this.connId);
    if (this.connId) connPanes.delete(this.connId);
    this.unsubData?.();
    this.unsubData = null;
  }

  destroy() {
    this.disconnect();
    this.term?.dispose();
    this.el.remove();
  }

  focus() {
    const el = this.term && !this.picker ? this.term : this.body.querySelector('input');
    if (!el) return;
    el.focus();
    requestAnimationFrame(() => el.focus());
  }

  async paste() {
    if (this.state !== 'connected') return;
    const text = await ClipboardGetText();
    if (text) this.term.paste(text);
  }

  copy() {
    const text = this.term?.getSelection();
    if (text) ClipboardSetText(text);
  }

  update() {
    this.titleEl.textContent = this.session ? `${this.session.name}  (${target(this.session)})` : 'Select a session';
    this.dotEl.className = `dot ${this.state}`;
    this.tab.renderTitle();
    sidebarList?.render();
  }
}

// ---------------------------------------------------------------------------
// タブとレイアウト
//
// レイアウトは二分木。葉が { type: 'pane', pane }、節が
// { type: 'split', dir: 'h'|'v', ratio, a, b }。h は左右、v は上下に分割。

const tabs = [];
let currentTab = null;
let tabSeq = 0;

class Tab {
  constructor() {
    this.id = ++tabSeq;
    this.root = null;
    this.active = null;
    this.el = h('div', { class: 'tab-content' });
    this.dotEl = h('span', { class: 'dot empty' });
    this.titleEl = h('span', { class: 'title' });
    this.tabEl = h('div', {
      class: 'tab',
      draggable: 'true',
      onmousedown: (e) => {
        if (e.button === 1) { e.preventDefault(); closeTab(this); }
      },
      onclick: () => activateTab(this),
      ondragstart: (e) => {
        e.dataTransfer.setData('text/x-tab', String(this.id));
        e.dataTransfer.effectAllowed = 'move';
      },
      ondragover: (e) => {
        if (!e.dataTransfer.types.includes('text/x-tab')) return;
        e.preventDefault();
        this.tabEl.classList.add('drag-over');
      },
      ondragleave: () => this.tabEl.classList.remove('drag-over'),
      ondrop: (e) => {
        this.tabEl.classList.remove('drag-over');
        const from = tabs.findIndex((t) => String(t.id) === e.dataTransfer.getData('text/x-tab'));
        if (from < 0) return;
        const [moved] = tabs.splice(from, 1);
        tabs.splice(tabs.indexOf(this), 0, moved);
        renderTabBar();
      },
    },
    this.dotEl, this.titleEl,
    h('button', {
      class: 'close',
      text: '×',
      title: 'Close tab',
      onclick: (e) => { e.stopPropagation(); closeTab(this); },
    }));
  }

  panes(node = this.root, out = []) {
    if (!node) return out;
    if (node.type === 'pane') out.push(node.pane);
    else { this.panes(node.a, out); this.panes(node.b, out); }
    return out;
  }

  renderTitle() {
    const p = this.active;
    const n = this.panes().length;
    const title = p ? p.title + (n > 1 ? ` (${n})` : '') : '';
    this.titleEl.textContent = title;
    this.tabEl.title = title;
    this.dotEl.className = `dot ${p ? p.state : 'empty'}`;
    if (this === currentTab) document.title = title ? `${title} - jterm` : 'jterm';
  }

  render() {
    this.el.replaceChildren(this.build(this.root));
    this.el.classList.toggle('multi', this.panes().length > 1);
    for (const p of this.panes()) p.refit();
  }

  build(node) {
    if (node.type === 'pane') return node.pane.el;
    const cellA = h('div', { class: 'cell' }, this.build(node.a));
    const cellB = h('div', { class: 'cell' }, this.build(node.b));
    const setRatio = () => {
      cellA.style.flexGrow = node.ratio;
      cellB.style.flexGrow = 1 - node.ratio;
    };
    setRatio();
    const divider = h('div', { class: 'divider' });
    const splitEl = h('div', { class: `split ${node.dir}` }, cellA, divider, cellB);
    divider.addEventListener('mousedown', (e) => {
      e.preventDefault();
      const rect = splitEl.getBoundingClientRect();
      divider.classList.add('dragging');
      document.body.classList.add(`resizing-${node.dir}`);
      const move = (ev) => {
        const r = node.dir === 'h'
          ? (ev.clientX - rect.left) / rect.width
          : (ev.clientY - rect.top) / rect.height;
        node.ratio = Math.min(0.9, Math.max(0.1, r));
        setRatio();
      };
      const up = () => {
        divider.classList.remove('dragging');
        document.body.classList.remove(`resizing-${node.dir}`);
        window.removeEventListener('mousemove', move);
        window.removeEventListener('mouseup', up);
        this.active?.focus();
      };
      window.addEventListener('mousemove', move);
      window.addEventListener('mouseup', up);
    });
    return splitEl;
  }

  replace(oldNode, newNode) {
    newNode.parent = oldNode.parent;
    if (!oldNode.parent) this.root = newNode;
    else if (oldNode.parent.a === oldNode) oldNode.parent.a = newNode;
    else oldNode.parent.b = newNode;
  }
}

function activePane() {
  return currentTab?.active ?? null;
}

function setActivePane(pane, focus = true) {
  const tab = pane.tab;
  if (tab.active !== pane) {
    tab.active?.el.classList.remove('focused');
    tab.active = pane;
    pane.el.classList.add('focused');
    tab.renderTitle();
  }
  if (focus) pane.focus();
}

function newTab(session) {
  const tab = new Tab();
  const pane = new Pane(tab);
  tab.root = { type: 'pane', pane, parent: null };
  pane.node = tab.root;
  tabs.push(tab);
  $('#workspace').append(tab.el);
  tab.render();
  setActivePane(pane, false);
  activateTab(tab);
  if (session) pane.connect(session);
  else pane.showPicker();
  return tab;
}

function activateTab(tab) {
  if (currentTab && currentTab !== tab) currentTab.el.classList.remove('active');
  currentTab = tab;
  tab.el.classList.add('active');
  renderTabBar();
  tab.renderTitle();
  for (const p of tab.panes()) p.refit();
  tab.active?.focus();
}

function closeTab(tab) {
  for (const p of tab.panes()) p.destroy();
  const i = tabs.indexOf(tab);
  tabs.splice(i, 1);
  tab.el.remove();
  if (currentTab === tab) {
    currentTab = null;
    const next = tabs[Math.min(i, tabs.length - 1)];
    if (next) activateTab(next);
  }
  renderTabBar();
  sidebarList.render();
  if (!tabs.length) document.title = 'jterm';
}

function renderTabBar() {
  $('#tabs').replaceChildren(...tabs.map((t) => {
    t.tabEl.classList.toggle('active', t === currentTab);
    return t.tabEl;
  }));
  currentTab?.tabEl.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  $('#welcome').hidden = tabs.length > 0;
}

// splitPane はペインを分割し、新しいペインを返す。session を省略するとピッカーを表示する。
function splitPane(pane, dir, session) {
  const tab = pane.tab;
  const leaf = pane.node;
  const newPane = new Pane(tab);
  const newLeaf = { type: 'pane', pane: newPane };
  const split = { type: 'split', dir, ratio: 0.5, a: leaf, b: newLeaf };
  tab.replace(leaf, split);
  leaf.parent = split;
  newLeaf.parent = split;
  newPane.node = newLeaf;
  tab.render();
  setActivePane(newPane, false);
  if (session) newPane.connect(session);
  else newPane.showPicker(pane.session?.id);
  return newPane;
}

function closePane(pane) {
  const tab = pane.tab;
  const leaf = pane.node;
  if (!leaf.parent) {
    closeTab(tab);
    return;
  }
  const parent = leaf.parent;
  const sibling = parent.a === leaf ? parent.b : parent.a;
  tab.replace(parent, sibling);
  pane.destroy();
  if (tab.active === pane) tab.active = null;
  tab.render();
  const next = tab.panes(sibling)[0];
  setActivePane(next, tab === currentTab);
  sidebarList.render();
}

// openSession は where に応じてセッションを開く。
function openSession(session, where = 'tab') {
  const pane = activePane();
  if (where === 'tab' || !pane) newTab(session);
  else splitPane(pane, where === 'split-h' ? 'h' : 'v', session);
}

// focusDirection は現在のペインから指定方向にある隣のペインへフォーカスを移す。
function focusDirection(dx, dy) {
  const cur = activePane();
  if (!cur) return;
  const r = cur.el.getBoundingClientRect();
  const cx = r.left + r.width / 2;
  const cy = r.top + r.height / 2;
  let best = null;
  let bestDist = Infinity;
  for (const p of currentTab.panes()) {
    if (p === cur) continue;
    const pr = p.el.getBoundingClientRect();
    const px = pr.left + pr.width / 2;
    const py = pr.top + pr.height / 2;
    const along = (px - cx) * dx + (py - cy) * dy;
    if (along <= 0) continue;
    const across = Math.abs((px - cx) * dy) + Math.abs((py - cy) * dx);
    const dist = along + across * 2;
    if (dist < bestDist) { bestDist = dist; best = p; }
  }
  if (best) setActivePane(best);
}

// ---------------------------------------------------------------------------
// ダイアログ

// ask はモーダルで問い合わせる。同時に来た問い合わせは順番に表示する。
const askQueue = [];
let asking = null;

function ask(opts) {
  return new Promise((resolve) => {
    askQueue.push({ ...opts, resolve });
    showNextAsk();
  });
}

function cancelAsk(id) {
  const i = askQueue.findIndex((q) => q.id === id);
  if (i >= 0) askQueue.splice(i, 1)[0].resolve({ ok: false, value: '' });
  if (asking?.id === id) finishAsk(false);
}

function showNextAsk() {
  if (asking || !askQueue.length) return;
  asking = askQueue.shift();
  const dlg = $('#prompt-dialog');
  const input = $('#pd-input');
  $('#pd-title').textContent = asking.title;
  $('#pd-message').textContent = asking.message;
  input.value = '';
  input.type = asking.kind === 'secret' || asking.kind === 'password' ? 'password' : 'text';
  input.hidden = asking.kind === 'confirm';
  $('#pd-remember').checked = false;
  $('#pd-remember-row').hidden = asking.kind !== 'password';
  $('#pd-ok').textContent = asking.okLabel ?? (asking.kind === 'confirm' ? 'Yes' : 'OK');
  $('#pd-ok').classList.toggle('danger', !!asking.danger);
  dlg.showModal();
  focusSoon(asking.kind === 'confirm' ? $('#pd-ok') : input);
}

function finishAsk(ok) {
  if (!asking) return;
  const cur = asking;
  asking = null;
  $('#prompt-dialog').close();
  cur.resolve({ ok, value: ok ? $('#pd-input').value : '', remember: ok && $('#pd-remember').checked });
  $('#pd-input').value = '';
  if (askQueue.length) showNextAsk();
  else activePane()?.focus();
}

$('#prompt-form').addEventListener('submit', (e) => { e.preventDefault(); finishAsk(true); });
$('#pd-cancel').addEventListener('click', () => finishAsk(false));
$('#prompt-dialog').addEventListener('cancel', (e) => { e.preventDefault(); finishAsk(false); });

// editSession はセッションの作成・編集ダイアログを開く。
// 戻り値は { session, connect } または null(キャンセル)。
function editSession(session) {
  const dlg = $('#session-dialog');
  const form = $('#session-form');
  const err = $('#sd-error');
  const isNew = !session?.id;
  $('#sd-title').textContent = isNew ? 'New session' : 'Edit session';
  form.name.value = session?.name ?? '';
  form.host.value = session?.host ?? '';
  form.port.value = session?.port || 22;
  form.user.value = session?.user ?? '';
  form.password.value = '';
  form.password.placeholder = session?.hasPassword ? 'Saved (leave blank to keep)' : 'Leave blank to ask when connecting';
  form.clearPassword.checked = false;
  $('#sd-clearpw-row').hidden = !session?.hasPassword;
  fillKeySelect(session?.keyFile ?? '');
  err.textContent = '';
  dlg.showModal();
  focusSoon(isNew && !session?.host ? form.host : form.name);

  return new Promise((resolve) => {
    const cleanup = () => {
      form.removeEventListener('submit', onSubmit);
      $('#sd-cancel').removeEventListener('click', onCancel);
      dlg.removeEventListener('cancel', onCancel);
    };
    const onCancel = (e) => {
      e?.preventDefault();
      cleanup();
      dlg.close();
      resolve(null);
    };
    const onSubmit = async (e) => {
      e.preventDefault();
      try {
        const saved = await Backend.SaveSession({
          id: isNew ? '' : session.id,
          name: form.name.value,
          host: form.host.value,
          port: Number(form.port.value) || 22,
          user: form.user.value,
          keyFile: form.keyFile.value,
          password: form.password.value,
          clearPassword: form.clearPassword.checked,
          hasPassword: false,
          lastUsed: 0,
        });
        cleanup();
        form.password.value = '';
        dlg.close();
        await reloadSessions();
        resolve({ session: saved, connect: e.submitter?.value === 'connect' });
      } catch (ex) {
        err.textContent = String(ex);
      }
    };
    form.addEventListener('submit', onSubmit);
    $('#sd-cancel').addEventListener('click', onCancel);
    dlg.addEventListener('cancel', onCancel);
  });
}

// fillKeySelect は秘密鍵の選択肢を作る。~/.ssh で見つかった鍵と、現在の設定値を並べる。
async function fillKeySelect(current) {
  const select = $('#session-form').keyFile;
  const keys = await Backend.ListKeyFiles();
  if (current && !keys.includes(current)) keys.unshift(current);
  select.replaceChildren(
    h('option', { value: '', text: 'Auto (~/.ssh/id_ed25519, id_ecdsa, id_rsa)' }),
    ...keys.map((k) => h('option', { value: k, text: k })),
  );
  select.value = current;
}

$('#sd-browse').addEventListener('click', async () => {
  const path = await Backend.PickKeyFile();
  if (!path) return;
  const select = $('#session-form').keyFile;
  if (![...select.options].some((o) => o.value === path)) select.append(h('option', { value: path, text: path }));
  select.value = path;
});

// focusSoon は描画直後にフォーカスする。WebView によっては showModal 直後の focus が効かないため。
function focusSoon(el) {
  el.focus();
  requestAnimationFrame(() => el.focus());
}

async function editAndMaybeConnect(session) {
  const r = await editSession(session);
  if (r?.connect) openSession(r.session, 'tab');
  else activePane()?.focus();
}

async function newSessionInto(pane) {
  const r = await editSession(null);
  if (!r) return pane?.focus();
  if (pane && pane.state === 'empty') {
    if (r.connect) pane.connect(r.session);
    else pane.picker?.select(r.session.id);
  } else if (r.connect) {
    newTab(r.session);
  }
}

// ---------------------------------------------------------------------------
// コンテキストメニュー

function showMenu(items, x, y) {
  const menu = $('#ctxmenu');
  menu.replaceChildren(...items.map((it) => (it === '-'
    ? h('hr')
    : h('div', {
      class: `item${it.danger ? ' danger' : ''}`,
      text: it.label,
      onclick: () => { hideMenu(); it.action(); },
    }))));
  menu.hidden = false;
  const mw = menu.offsetWidth;
  const mh = menu.offsetHeight;
  menu.style.left = `${Math.min(x, window.innerWidth - mw - 4)}px`;
  menu.style.top = `${Math.min(y, window.innerHeight - mh - 4)}px`;
}

function hideMenu() {
  $('#ctxmenu').hidden = true;
}

window.addEventListener('mousedown', (e) => {
  if (!$('#ctxmenu').contains(e.target)) hideMenu();
}, true);
window.addEventListener('blur', hideMenu);

function sessionMenu(s, x, y) {
  const hasPane = !!activePane();
  showMenu([
    { label: 'Open in new tab', action: () => openSession(s, 'tab') },
    hasPane && { label: 'Open in split right', action: () => openSession(s, 'split-h') },
    hasPane && { label: 'Open in split down', action: () => openSession(s, 'split-v') },
    '-',
    { label: 'Edit…', action: () => editAndMaybeConnect(s) },
    {
      label: 'Duplicate',
      action: () => editAndMaybeConnect({ ...s, id: '', name: `${s.name} (copy)`, hasPassword: false }),
    },
    s.hasPassword && {
      label: 'Forget saved password',
      action: async () => { await Backend.ForgetPassword(s.id); reloadSessions(); },
    },
    {
      label: 'Forget saved key passphrase',
      action: () => Backend.ForgetPassphrase(s.id),
    },
    {
      label: 'Delete…',
      danger: true,
      action: async () => {
        const r = await ask({ kind: 'confirm', title: 'Delete session', message: `Delete “${s.name}”?`, okLabel: 'Delete', danger: true });
        if (r.ok) {
          await Backend.DeleteSession(s.id);
          reloadSessions();
        }
      },
    },
  ].filter(Boolean), x, y);
}

// ---------------------------------------------------------------------------
// キーボードショートカット

const shortcuts = [
  ['KeyT', 'New tab', () => newTab()],
  ['KeyW', 'Close pane', () => activePane() && closePane(activePane())],
  ['KeyD', 'Split right', () => activePane() && splitPane(activePane(), 'h')],
  ['KeyE', 'Split down', () => activePane() && splitPane(activePane(), 'v')],
  ['KeyK', 'Search sessions', () => focusSearch()],
  ['KeyB', 'Toggle sidebar', () => toggleSidebar()],
  ['KeyC', 'Copy', () => activePane()?.copy()],
  ['KeyV', 'Paste', () => activePane()?.paste()],
  ['ArrowLeft', 'Focus left pane', () => focusDirection(-1, 0)],
  ['ArrowRight', 'Focus right pane', () => focusDirection(1, 0)],
  ['ArrowUp', 'Focus upper pane', () => focusDirection(0, -1)],
  ['ArrowDown', 'Focus lower pane', () => focusDirection(0, 1)],
];

function isMod(e) {
  // Windows では端末のキー(Ctrl+C 等)とぶつからないよう Ctrl+Shift を使う
  if (isMac) return e.metaKey && !e.ctrlKey;
  return e.ctrlKey && e.shiftKey && !e.altKey && !e.metaKey;
}

function cycleTab(delta) {
  if (!tabs.length) return;
  const i = tabs.indexOf(currentTab);
  activateTab(tabs[(i + delta + tabs.length) % tabs.length]);
}

// キャプチャ段階で処理して、xterm にキーが渡らないようにする
document.addEventListener('keydown', (e) => {
  if (document.querySelector('dialog[open]')) return;
  // 検索欄などの通常の入力欄では、コピー・貼り付け等は標準の動作に任せる
  const inField = e.target.matches?.('input, textarea:not(.xterm-helper-textarea)');
  if (inField && ['KeyA', 'KeyC', 'KeyV', 'KeyX'].includes(e.code)) return;
  let action = null;
  if (e.ctrlKey && e.code === 'Tab') {
    action = () => cycleTab(e.shiftKey ? -1 : 1);
  } else if (isMod(e)) {
    const sc = shortcuts.find(([code]) => code === e.code);
    if (sc) action = sc[2];
    const m = /^Digit([1-9])$/.exec(e.code);
    if (m) action = () => { const t = tabs[Number(m[1]) - 1]; if (t) activateTab(t); };
  }
  if (!action) return;
  e.preventDefault();
  e.stopPropagation();
  hideMenu();
  action();
}, true);

function focusSearch() {
  const app = $('#app');
  if (app.classList.contains('sidebar-hidden')) toggleSidebar();
  $('#search').focus();
  $('#search').select();
}

function toggleSidebar() {
  $('#app').classList.toggle('sidebar-hidden');
  for (const t of tabs) for (const p of t.panes()) p.refit();
}

function renderShortcutHelp() {
  const rows = [
    ['Double-click a session', 'Connect in a new tab'],
    ['Right-click a session', 'Open in split / edit / delete'],
    ...shortcuts.filter(([code]) => !code.startsWith('Arrow')).map(([code, label]) => [`${MOD_LABEL}${code.slice(3)}`, label]),
    [`${MOD_LABEL}←↑↓→`, 'Move between panes'],
    ['Ctrl+Tab', 'Next tab'],
    ['Select text / right-click in terminal', 'Copy / paste'],
  ];
  $('#shortcut-help').replaceChildren(...rows.map(([k, v]) => h('tr', {}, h('td', {}, h('kbd', { text: k })), h('td', { text: v }))));
}

// ---------------------------------------------------------------------------
// バックエンドからのイベント

EventsOn('ssh:status', (ev) => connPanes.get(ev.conn)?.onStatus(ev));

EventsOn('ssh:prompt', async (ev) => {
  const pane = connPanes.get(ev.conn);
  if (pane && pane.tab !== currentTab) activateTab(pane.tab);
  if (pane) setActivePane(pane, false);
  const r = await ask(ev);
  Backend.AnswerPrompt(ev.id, r.value, r.ok, !!r.remember);
});

EventsOn('ssh:prompt-cancel', (id) => cancelAsk(id));

EventsOn('sessions:changed', () => reloadSessions());

// ---------------------------------------------------------------------------
// 起動

sidebarList = new SessionList($('#search'), $('#session-list'), {
  onOpen: (s, where) => openSession(s, where),
});

$('#btn-new-session').addEventListener('click', () => newSessionInto(activePane()?.state === 'empty' ? activePane() : null));
$('#btn-import-ssh').addEventListener('click', async () => {
  const status = $('#import-status');
  let text;
  try {
    const r = await Backend.ImportSSHConfig();
    text = `Imported ${r.added}, skipped ${r.skipped} existing.`;
    if (r.unsupported) text += ` ${r.unsupported} use ProxyJump/ProxyCommand, which is not supported yet.`;
  } catch (e) {
    text = `Import failed: ${e}`;
  }
  status.textContent = text;
  status.hidden = false;
  setTimeout(() => { status.hidden = true; }, 8000);
});
$('#btn-new-tab').addEventListener('click', () => newTab());
$('#btn-split-h').addEventListener('click', () => activePane() && splitPane(activePane(), 'h'));
$('#btn-split-v').addEventListener('click', () => activePane() && splitPane(activePane(), 'v'));
$('#btn-toggle-sidebar').addEventListener('click', () => toggleSidebar());
$('#tabbar').addEventListener('dblclick', (e) => {
  if (e.target === e.currentTarget || e.target.id === 'tabs') newTab();
});
// アプリ全体で既定の右クリックメニューを出さない
document.addEventListener('contextmenu', (e) => {
  if (!e.target.closest('input')) e.preventDefault();
});

async function init() {
  renderShortcutHelp();
  renderTabBar();
  // 画面の再読み込み時に、前の画面の接続が残らないようにする
  await Backend.DisconnectAll();
  await reloadSessions();
  $('#search').focus();
}

init();
