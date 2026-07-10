(() => {
  "use strict";

  const RECENT_KEY = "folder-size:recent";
  const RECENT_MAX = 6;

  const el = {
    form: document.getElementById("scan-form"),
    input: document.getElementById("path-input"),
    scanBtn: document.getElementById("scan-btn"),
    cancelBtn: document.getElementById("cancel-btn"),
    conn: document.getElementById("conn"),
    progress: document.getElementById("progress"),
    progressStats: document.getElementById("progress-stats"),
    progressCurrent: document.getElementById("progress-current"),
    summary: document.getElementById("summary"),
    statSize: document.getElementById("stat-size"),
    statFiles: document.getElementById("stat-files"),
    statItems: document.getElementById("stat-items"),
    statFreed: document.getElementById("stat-freed"),
    treemap: document.getElementById("treemap"),
    toolbar: document.getElementById("toolbar"),
    crumbs: document.getElementById("breadcrumbs"),
    filter: document.getElementById("filter"),
    sort: document.getElementById("sort"),
    empty: document.getElementById("empty"),
    quick: document.getElementById("quick-paths"),
    recent: document.getElementById("recent-paths"),
    list: document.getElementById("list"),
    modal: document.getElementById("modal"),
    modalBody: document.getElementById("modal-body"),
    modalOk: document.getElementById("modal-ok"),
    modalCancel: document.getElementById("modal-cancel"),
    toast: document.getElementById("toast"),
  };

  const state = {
    root: "",
    current: null,
    children: [],
    sep: "/",
    ws: null,
    freed: 0,
    active: -1,
    pendingDelete: null,
  };
  let toastTimer = null;

  // ---- WebSocket -----------------------------------------------------------
  function connect() {
    const proto = location.protocol === "https:" ? "wss" : "ws";
    const ws = new WebSocket(`${proto}://${location.host}/ws`);
    state.ws = ws;
    ws.onopen = () => setConn(true);
    ws.onclose = () => {
      setConn(false);
      setScanning(false);
      setTimeout(connect, 1500);
    };
    ws.onmessage = (e) => handle(JSON.parse(e.data));
  }

  function send(msg) {
    if (state.ws?.readyState === WebSocket.OPEN) {
      state.ws.send(JSON.stringify(msg));
    }
  }

  function setConn(online) {
    el.conn.classList.toggle("online", online);
    el.conn.classList.toggle("offline", !online);
    el.conn.title = online ? "Соединение установлено" : "Нет соединения";
  }

  function setScanning(on) {
    el.scanBtn.disabled = on;
    el.cancelBtn.classList.toggle("hidden", !on);
    if (!on) el.progress.classList.add("hidden");
  }

  // ---- Messages ------------------------------------------------------------
  function handle(m) {
    switch (m.type) {
      case "init":
        state.sep = m.sep || "/";
        if (!el.input.value && m.home) el.input.value = m.home;
        renderChips(el.quick, m.roots || [], false);
        renderChips(el.recent, loadRecent(), true);
        break;
      case "progress":
        renderProgress(m.progress, m.done);
        break;
      case "scanned":
        setScanning(false);
        state.root = m.root.path;
        rememberRecent(m.root.path);
        showFolder(m.root, m.children || []);
        break;
      case "children":
        showFolder(m.parent, m.children || []);
        break;
      case "deleted":
        state.freed += m.freed || 0;
        el.statFreed.textContent = fmtSize(state.freed);
        toast(`Освобождено ${fmtSize(m.freed)} · ${fmtCount(m.files)} файл(ов)`, "ok");
        if (m.parent) showFolder(m.parent, m.children || []);
        break;
      case "cancelled":
        setScanning(false);
        el.empty.classList.remove("hidden");
        toast("Сканирование отменено", "err");
        break;
      case "error":
        setScanning(false);
        toast(m.message, "err");
        break;
    }
  }

  function renderProgress(p, done) {
    if (!p) return;
    el.progress.classList.remove("hidden");
    el.progressStats.textContent = done
      ? `Готово: ${fmtCount(p.files)} файлов · ${fmtSize(p.bytes)}`
      : `Сканирование: ${fmtCount(p.files)} файлов · ${fmtSize(p.bytes)}`;
    el.progressCurrent.textContent = p.current || "";
  }

  // ---- Folder view ---------------------------------------------------------
  function showFolder(parent, children) {
    state.current = parent;
    state.children = children;
    state.active = -1;
    el.empty.classList.add("hidden");
    el.summary.classList.remove("hidden");
    el.toolbar.classList.remove("hidden");
    el.statSize.textContent = fmtSize(parent.size);
    el.statFiles.textContent = fmtCount(parent.files);
    el.statItems.textContent = fmtCount(children.length);
    el.statFreed.textContent = fmtSize(state.freed);
    renderCrumbs(parent.path);
    renderView();
    el.list.focus({ preventScroll: true });
  }

  function visibleChildren() {
    const q = el.filter.value.trim().toLowerCase();
    let list = state.children;
    if (q) list = list.filter((c) => c.name.toLowerCase().includes(q));
    return sortList(list, el.sort.value);
  }

  function sortList(list, mode) {
    const arr = list.slice();
    const cmp = {
      "size-desc": (a, b) => b.size - a.size,
      "size-asc": (a, b) => a.size - b.size,
      "name-asc": (a, b) => a.name.localeCompare(b.name, "ru", { sensitivity: "base" }),
      "name-desc": (a, b) => b.name.localeCompare(a.name, "ru", { sensitivity: "base" }),
      "files-desc": (a, b) => (b.files || 0) - (a.files || 0),
    }[mode] || ((a, b) => b.size - a.size);
    arr.sort(cmp);
    return arr;
  }

  function renderView() {
    const kids = visibleChildren();
    renderTreemap(kids);
    el.list.innerHTML = "";
    if (!kids.length) {
      const li = document.createElement("li");
      li.className = "list-empty";
      li.textContent = state.children.length ? "Ничего не найдено по фильтру." : "Папка пуста.";
      el.list.appendChild(li);
      return;
    }
    const max = kids.reduce((mx, c) => Math.max(mx, c.linked ? 0 : c.size), 0) || 1;
    const frag = document.createDocumentFragment();
    kids.forEach((c, i) => frag.appendChild(row(c, max, i)));
    el.list.appendChild(frag);
    if (state.active >= 0) setActive(Math.min(state.active, kids.length - 1), false);
  }

  function renderTreemap(kids) {
    el.treemap.innerHTML = "";
    const total = state.current?.size || 0;
    if (!total || !kids.length) {
      el.treemap.classList.add("hidden");
      return;
    }
    el.treemap.classList.remove("hidden");
    let shown = 0;
    const frag = document.createDocumentFragment();
    for (const c of kids) {
      if (c.linked) continue;
      const pct = (c.size / total) * 100;
      if (pct < 0.5) continue;
      shown += pct;
      const s = document.createElement("span");
      s.className = c.isDir ? "dir" : "file";
      s.style.flex = String(Math.max(pct, 0.5));
      s.title = `${c.name} — ${fmtSize(c.size)} (${pct.toFixed(1)}%)`;
      if (c.isDir && c.hasChildren) {
        s.addEventListener("click", () => openPath(c.path));
      }
      frag.appendChild(s);
      if (shown > 98) break;
    }
    el.treemap.appendChild(frag);
  }

  function row(node, max, index) {
    const li = document.createElement("li");
    li.className = "row " + (node.isDir ? "dir" : "file") + (node.linked ? " linked" : "");
    li.role = "option";
    li.dataset.index = String(index);
    li.dataset.path = node.path;
    li.style.animationDelay = Math.min(index, 24) * 12 + "ms";

    const contrib = node.linked ? 0 : node.size;

    const bar = document.createElement("div");
    bar.className = "bar";
    bar.style.width = max > 0 ? Math.max(contrib ? 2 : 0, (contrib / max) * 100) + "%" : "0%";

    const icon = document.createElement("span");
    icon.className = "icon " + (node.isDir ? "dir" : "file");
    icon.textContent = node.linked ? "↗" : (node.isDir ? "▸" : extBadge(node.name));

    const name = document.createElement("span");
    name.className = "name";
    name.textContent = node.name || node.path;
    if (node.linked) {
      const small = document.createElement("small");
      small.textContent = "hard link";
      name.appendChild(small);
    } else if (node.isDir && node.files) {
      const small = document.createElement("small");
      small.textContent = fmtCount(node.files) + " ф.";
      name.appendChild(small);
    }

    const size = document.createElement("span");
    size.className = "size";
    if (node.linked) {
      size.textContent = "0 B";
      size.title = `Hard link — место уже учтено в другом пути\nЛогический: ${fmtSize(node.logical)}`;
      const sparse = document.createElement("small");
      sparse.className = "sparse";
      sparse.textContent = `↗ ${fmtSize(node.logical)}`;
      size.appendChild(document.createElement("br"));
      size.appendChild(sparse);
    } else {
      size.textContent = fmtSize(node.size);
      if (node.sparse && node.logical > node.size) {
        size.title = `На диске: ${fmtSize(node.size)}\nЛогический: ${fmtSize(node.logical)}`;
        const sparse = document.createElement("small");
        sparse.className = "sparse";
        sparse.textContent = `логич. ${fmtSize(node.logical)}`;
        size.appendChild(document.createElement("br"));
        size.appendChild(sparse);
      }
    }

    const pct = document.createElement("span");
    pct.className = "pct";
    if (node.linked) {
      pct.textContent = "link";
    } else {
      const share = state.current?.size ? (node.size / state.current.size) * 100 : 0;
      pct.textContent = share.toFixed(share < 10 ? 1 : 0) + "%";
    }

    const actions = document.createElement("span");
    actions.className = "actions";
    actions.append(
      mkBtn("reveal", "Показать", (e) => { e.stopPropagation(); send({ type: "reveal", path: node.path }); }),
      mkBtn("del", "Удалить", (e) => { e.stopPropagation(); askDelete(node); }),
    );

    li.append(bar, icon, name, size, pct, actions);
    li.addEventListener("click", () => {
      setActive(index, false);
      if (node.isDir && node.hasChildren) openPath(node.path);
    });
    li.addEventListener("dblclick", () => {
      if (!node.isDir) send({ type: "reveal", path: node.path });
    });
    return li;
  }

  function extBadge(name) {
    const i = name.lastIndexOf(".");
    if (i <= 0 || i === name.length - 1) return "·";
    return name.slice(i + 1, i + 4).toUpperCase();
  }

  function mkBtn(cls, text, onClick) {
    const b = document.createElement("button");
    b.className = cls;
    b.type = "button";
    b.textContent = text;
    b.addEventListener("click", onClick);
    return b;
  }

  function openPath(path) {
    send({ type: "open", path });
  }

  // ---- Breadcrumbs ---------------------------------------------------------
  function renderCrumbs(path) {
    el.crumbs.innerHTML = "";
    if (!state.root || !path) return;

    if (path !== state.root) {
      const up = document.createElement("span");
      up.className = "crumb crumb-up";
      up.textContent = "↑ Вверх";
      up.addEventListener("click", goUp);
      el.crumbs.appendChild(up);
    }

    const rel = path.startsWith(state.root) ? path.slice(state.root.length) : path;
    const parts = rel.split(state.sep).filter(Boolean);

    addCrumb(shortLabel(state.root), state.root, parts.length === 0);
    let acc = state.root;
    parts.forEach((part, i) => {
      acc = acc.endsWith(state.sep) ? acc + part : acc + state.sep + part;
      const sep = document.createElement("span");
      sep.className = "crumb-sep";
      sep.textContent = state.sep;
      el.crumbs.appendChild(sep);
      addCrumb(part, acc, i === parts.length - 1);
    });
  }

  function shortLabel(p) {
    if (p === "/") return "/";
    const base = p.split(state.sep).filter(Boolean).pop();
    return base || p;
  }

  function addCrumb(label, path, current) {
    const c = document.createElement("span");
    c.className = "crumb" + (current ? " current" : "");
    c.textContent = label;
    c.title = path;
    if (!current) c.addEventListener("click", () => openPath(path));
    el.crumbs.appendChild(c);
  }

  function goUp() {
    if (!state.current || state.current.path === state.root) return;
    const parent = state.current.path.split(state.sep).slice(0, -1).join(state.sep) || state.sep;
    // На Unix корень "/" — особый случай; иначе Dir.
    const up = state.current.path === state.sep
      ? state.sep
      : (parent || state.root);
    openPath(up === "" ? state.sep : up);
  }

  // ---- Delete modal --------------------------------------------------------
  function askDelete(node) {
    state.pendingDelete = node;
    const kind = node.isDir ? "папку" : "файл";
    el.modalBody.textContent =
      `${kind}: ${node.path}\n\nРазмер: ${fmtSize(node.size)}\nДействие необратимо.`;
    el.modal.classList.remove("hidden");
    el.modalOk.focus();
  }

  function closeModal() {
    el.modal.classList.add("hidden");
    state.pendingDelete = null;
  }

  el.modalCancel.addEventListener("click", closeModal);
  el.modal.addEventListener("click", (e) => { if (e.target === el.modal) closeModal(); });
  el.modalOk.addEventListener("click", () => {
    const node = state.pendingDelete;
    closeModal();
    if (node) send({ type: "delete", path: node.path });
  });

  // ---- Chips / recent ------------------------------------------------------
  function renderChips(container, paths, isRecent) {
    container.innerHTML = "";
    for (const p of paths) {
      const b = document.createElement("button");
      b.type = "button";
      b.className = "chip" + (isRecent ? " recent-chip" : "");
      b.textContent = isRecent ? "↻ " + shortLabel(p) : shortLabel(p);
      b.title = p;
      b.addEventListener("click", () => {
        el.input.value = p;
        startScan(p);
      });
      container.appendChild(b);
    }
  }

  function loadRecent() {
    try { return JSON.parse(localStorage.getItem(RECENT_KEY) || "[]"); }
    catch { return []; }
  }

  function rememberRecent(path) {
    const list = loadRecent().filter((p) => p !== path);
    list.unshift(path);
    localStorage.setItem(RECENT_KEY, JSON.stringify(list.slice(0, RECENT_MAX)));
    renderChips(el.recent, loadRecent(), true);
  }

  // ---- Keyboard ------------------------------------------------------------
  function setActive(index, scroll) {
    const rows = el.list.querySelectorAll(".row");
    rows.forEach((r) => r.classList.remove("active"));
    state.active = index;
    if (index < 0 || index >= rows.length) return;
    rows[index].classList.add("active");
    if (scroll) rows[index].scrollIntoView({ block: "nearest" });
  }

  function activeNode() {
    const kids = visibleChildren();
    return kids[state.active] || null;
  }

  el.list.addEventListener("keydown", (e) => {
    const kids = visibleChildren();
    if (!kids.length) return;
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActive(Math.min(state.active + 1, kids.length - 1), true);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive(Math.max(state.active - 1, 0), true);
    } else if (e.key === "Enter") {
      e.preventDefault();
      const n = activeNode();
      if (n?.isDir && n.hasChildren) openPath(n.path);
      else if (n) send({ type: "reveal", path: n.path });
    } else if (e.key === "Backspace" && !e.metaKey && !e.ctrlKey) {
      e.preventDefault();
      goUp();
    } else if (e.key === "Delete" || (e.key === "Backspace" && (e.metaKey || e.ctrlKey))) {
      e.preventDefault();
      const n = activeNode();
      if (n) askDelete(n);
    } else if (e.key === "Escape") {
      closeModal();
      el.filter.value = "";
      renderView();
    }
  });

  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && !el.modal.classList.contains("hidden")) {
      e.preventDefault();
      closeModal();
    }
  });

  // ---- Utils ---------------------------------------------------------------
  function fmtSize(bytes) {
    bytes = Number(bytes) || 0;
    if (bytes < 1024) return bytes + " B";
    const units = ["KB", "MB", "GB", "TB", "PB"];
    let v = bytes / 1024, i = 0;
    while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
    return (v >= 100 ? v.toFixed(0) : v.toFixed(1)) + " " + units[i];
  }

  function fmtCount(n) {
    return Number(n || 0).toLocaleString("ru");
  }

  function toast(text, kind) {
    el.toast.textContent = text;
    el.toast.className = "toast " + (kind || "");
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => el.toast.classList.add("hidden"), 3800);
  }

  function startScan(path) {
    path = (path || "").trim();
    if (!path) return;
    state.freed = 0;
    el.list.innerHTML = "";
    el.crumbs.innerHTML = "";
    el.summary.classList.add("hidden");
    el.toolbar.classList.add("hidden");
    el.treemap.classList.add("hidden");
    el.empty.classList.add("hidden");
    el.filter.value = "";
    setScanning(true);
    renderProgress({ files: 0, bytes: 0, current: "" }, false);
    send({ type: "scan", path });
  }

  // ---- Bindings ------------------------------------------------------------
  el.form.addEventListener("submit", (e) => {
    e.preventDefault();
    startScan(el.input.value);
  });
  el.cancelBtn.addEventListener("click", () => send({ type: "cancel" }));
  el.filter.addEventListener("input", () => { state.active = -1; renderView(); });
  el.sort.addEventListener("change", () => { state.active = -1; renderView(); });

  connect();
})();
