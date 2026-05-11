const state = {
  apiKey: localStorage.getItem("smth.apiKey") || "",
  items: [],
  nextCursor: "",
  selectedId: new URLSearchParams(location.search).get("id") || "",
  unread: new Set(),
  query: "",
  project: "",
  stream: null,
  pendingDelete: null,
  undoTimer: 0,
  toastTimer: 0,
  toastTick: 0,
};

const els = {
  app: document.querySelector(".app"),
  main: document.querySelector(".main"),
  conn: document.getElementById("conn"),
  connLabel: document.getElementById("connLabel"),
  apiKey: document.getElementById("apiKey"),
  search: document.getElementById("search"),
  project: document.getElementById("project"),
  refresh: document.getElementById("refresh"),
  list: document.getElementById("list"),
  count: document.getElementById("count"),
  more: document.getElementById("more"),
  mainTitle: document.getElementById("mainTitle"),
  mainProject: document.getElementById("mainProject"),
  mainTime: document.getElementById("mainTime"),
  mainSepA: document.getElementById("mainSepA"),
  mainSepB: document.getElementById("mainSepB"),
  frame: document.getElementById("frame"),
  copy: document.getElementById("copy"),
  open: document.getElementById("open"),
  delete: document.getElementById("delete"),
  toast: document.getElementById("toast"),
  toastText: document.getElementById("toastText"),
  toastUndo: document.getElementById("toastUndo"),
  toastBar: document.getElementById("toastBar"),
};

els.apiKey.value = state.apiKey;

function headers() {
  return state.apiKey ? { "X-API-Key": state.apiKey } : {};
}

async function api(path, options = {}) {
  const response = await fetch(path, {
    ...options,
    headers: {
      ...headers(),
      ...(options.headers || {}),
    },
  });
  if (!response.ok) {
    const message = await response.text();
    throw new Error(message.trim() || `${response.status} ${response.statusText}`);
  }
  if (response.status === 204) {
    return null;
  }
  return response.json();
}

async function loadArtifacts({ append = false } = {}) {
  const params = new URLSearchParams({ limit: "50" });
  if (state.project) {
    params.set("project", state.project);
  }
  if (append && state.nextCursor) {
    params.set("before", state.nextCursor);
  }

  const data = await api(`/api/artifacts?${params}`);
  state.items = append ? mergeById(state.items, data.items || []) : data.items || [];
  state.nextCursor = data.next_cursor || "";

  if (!state.selectedId && state.items.length) {
    state.selectedId = state.items[0].id;
  }
  if (state.selectedId && !state.items.some((item) => item.id === state.selectedId)) {
    state.selectedId = state.items[0]?.id || "";
  }

  render();
}

function mergeById(existing, incoming) {
  const next = existing.slice();
  const seen = new Set(next.map((item) => item.id));
  for (const item of incoming) {
    if (!seen.has(item.id)) {
      next.push(item);
      seen.add(item.id);
    }
  }
  return next;
}

function render() {
  const visible = filteredItems();
  const groups = groupByDay(visible);
  els.list.replaceChildren(...groups.map(renderGroup));
  if (!groups.length) {
    const empty = document.createElement("div");
    empty.className = "sb-empty";
    empty.textContent = state.items.length ? "No matches" : "No artifacts";
    els.list.append(empty);
  }

  els.count.textContent = `${state.items.length} artifact${state.items.length === 1 ? "" : "s"}`;
  els.more.disabled = !state.nextCursor;

  const selected = selectedItem();
  els.main.classList.toggle("is-empty", !selected);
  els.copy.disabled = !selected;
  els.open.disabled = !selected;
  els.delete.disabled = !selected || !state.apiKey;

  if (!selected) {
    els.mainTitle.textContent = "no artifact selected";
    els.mainProject.textContent = "";
    els.mainTime.textContent = "";
    els.mainSepA.hidden = true;
    els.mainSepB.hidden = true;
    els.frame.removeAttribute("src");
    els.frame.removeAttribute("srcdoc");
    return;
  }

  const title = displayTitle(selected.title || selected.id, selected.project || "");
  els.mainTitle.textContent = title;
  els.mainTitle.title = selected.title || selected.id;
  els.mainProject.textContent = selected.project || "no project";
  els.mainTime.textContent = `${relTime(dateOf(selected.updated_at || selected.created_at))} ago`;
  els.mainSepA.hidden = false;
  els.mainSepB.hidden = false;

  if (els.frame.dataset.artifactId !== selected.id) {
    loadFrameHTML(selected.id);
  }

  const url = new URL(location.href);
  if (url.searchParams.get("id") !== selected.id) {
    url.searchParams.set("id", selected.id);
    history.replaceState({}, "", url);
  }
}

async function loadFrameHTML(id) {
  els.frame.dataset.artifactId = id;
  els.frame.removeAttribute("src");
  try {
    const response = await fetch(`/a/${id}`, { headers: headers() });
    if (!response.ok) {
      throw new Error((await response.text()).trim() || `${response.status} ${response.statusText}`);
    }
    if (state.selectedId !== id) {
      return;
    }
    els.frame.srcdoc = await response.text();
  } catch (err) {
    if (state.selectedId !== id) {
      return;
    }
    els.frame.removeAttribute("srcdoc");
    showToast(err.message);
  }
}

function renderGroup(group) {
  const root = document.createElement("div");
  root.className = "sb-group";

  const label = document.createElement("div");
  label.className = "sb-group-label";
  label.textContent = group.label;
  root.append(label);

  for (const item of group.items) {
    root.append(renderItem(item));
  }
  return root;
}

function renderItem(item) {
  const button = document.createElement("button");
  button.type = "button";
  button.dataset.aid = item.id;
  button.className = `sb-item${item.id === state.selectedId ? " is-selected" : ""}`;
  button.addEventListener("click", () => selectItem(item.id));

  if (item.id === state.selectedId) {
    const bar = document.createElement("span");
    bar.className = "sb-item-bar";
    button.append(bar);
  }

  const row = document.createElement("div");
  row.className = "sb-item-row";

  const title = document.createElement("span");
  title.className = "sb-item-title";
  title.textContent = item.title || item.id;
  row.append(title);

  if (state.unread.has(item.id)) {
    const dot = document.createElement("span");
    dot.className = "sb-item-dot";
    row.append(dot);
  }

  const meta = document.createElement("div");
  meta.className = "sb-item-meta";
  const project = document.createElement("span");
  project.textContent = item.project || "no project";
  const sep = document.createElement("span");
  sep.className = "sb-item-sep";
  sep.textContent = "·";
  const time = document.createElement("span");
  time.textContent = relTime(dateOf(item.created_at));
  meta.append(project, sep, time);

  button.append(row, meta);
  return button;
}

function filteredItems() {
  const q = state.query.trim().toLowerCase();
  if (!q) {
    return state.items;
  }
  return state.items.filter((item) => {
    const haystack = [
      item.title,
      item.project,
      ...(item.tags || []),
      item.id,
    ]
      .filter(Boolean)
      .join(" ")
      .toLowerCase();
    return haystack.includes(q);
  });
}

function selectedItem() {
  return state.items.find((item) => item.id === state.selectedId) || null;
}

function selectItem(id) {
  state.selectedId = id;
  state.unread.delete(id);
  render();
  requestAnimationFrame(() => {
    document.querySelector(`[data-aid="${CSS.escape(id)}"]`)?.scrollIntoView({ block: "nearest" });
  });
}

async function refreshMetadata(id) {
  const item = await api(`/api/artifacts/${id}`);
  const idx = state.items.findIndex((candidate) => candidate.id === id);
  if (idx >= 0) {
    state.items.splice(idx, 1, item);
  } else if (!state.project || item.project === state.project) {
    state.items.unshift(item);
  }
  state.items.sort((a, b) => b.id.localeCompare(a.id));
}

function connectStream() {
  if (state.stream) {
    state.stream.close();
  }

  setConn("reconnecting");
  state.stream = new EventSource("/api/stream");
  state.stream.addEventListener("open", () => setConn("connected"));
  state.stream.addEventListener("error", () => setConn("disconnected"));
  state.stream.addEventListener("new", async (event) => {
    const data = JSON.parse(event.data);
    await refreshMetadata(data.id);
    if (!state.selectedId) {
      state.selectedId = data.id;
    } else if (state.selectedId !== data.id) {
      state.unread.add(data.id);
    }
    render();
  });
  state.stream.addEventListener("update", async (event) => {
    const data = JSON.parse(event.data);
    await refreshMetadata(data.id);
    if (state.selectedId === data.id) {
      await loadFrameHTML(data.id);
    } else {
      state.unread.add(data.id);
    }
    render();
  });
  state.stream.addEventListener("delete", (event) => {
    const data = JSON.parse(event.data);
    removeItem(data.id);
    render();
  });
}

function setConn(value) {
  const labels = {
    connected: "connected",
    reconnecting: "reconnecting",
    disconnected: "disconnected",
  };
  els.conn.dataset.state = value;
  els.connLabel.textContent = labels[value] || value;
}

async function copyLink() {
  const selected = selectedItem();
  if (!selected) {
    return;
  }
  const url = new URL(location.href);
  url.searchParams.set("id", selected.id);
  await navigator.clipboard?.writeText(url.toString());
  showToast("copied link");
}

function openRaw() {
  const selected = selectedItem();
  if (selected) {
    window.open(`/a/${selected.id}`, "_blank", "noopener");
  }
}

function requestDelete() {
  const selected = selectedItem();
  if (!selected || !state.apiKey) {
    return;
  }

  commitPendingDelete();

  const victim = selected;
  removeItem(victim.id);
  state.pendingDelete = {
    item: victim,
    expiresAt: Date.now() + 3000,
  };
  showUndoToast(`deleted "${displayTitle(victim.title || victim.id, victim.project || "")}"`);
  render();

  state.undoTimer = window.setTimeout(() => {
    commitPendingDelete();
  }, 3000);
}

function undoDelete() {
  if (!state.pendingDelete) {
    return;
  }
  window.clearTimeout(state.undoTimer);
  const item = state.pendingDelete.item;
  state.pendingDelete = null;
  state.items.unshift(item);
  state.items.sort((a, b) => b.id.localeCompare(a.id));
  state.selectedId = item.id;
  hideToast();
  render();
}

function commitPendingDelete() {
  if (!state.pendingDelete) {
    return;
  }

  window.clearTimeout(state.undoTimer);
  window.clearInterval(state.toastTick);

  const pending = state.pendingDelete;
  state.pendingDelete = null;
  hideToast();

  api(`/api/artifacts/${pending.item.id}`, { method: "DELETE" }).catch((err) => {
    state.items.unshift(pending.item);
    state.items.sort((a, b) => b.id.localeCompare(a.id));
    if (!state.selectedId) {
      state.selectedId = pending.item.id;
    }
    render();
    showToast(err.message);
  });
}

function removeItem(id) {
  state.items = state.items.filter((item) => item.id !== id);
  state.unread.delete(id);
  if (state.selectedId === id) {
    state.selectedId = state.items[0]?.id || "";
  }
}

function showToast(message) {
  window.clearTimeout(state.toastTimer);
  window.clearInterval(state.toastTick);
  els.toastText.textContent = message;
  els.toastUndo.hidden = true;
  els.toastBar.style.transform = "scaleX(0)";
  els.toast.hidden = false;
  state.toastTimer = window.setTimeout(hideToast, 1800);
}

function showUndoToast(message) {
  els.toastText.textContent = message;
  els.toastUndo.hidden = false;
  els.toast.hidden = false;
  const tick = () => {
    if (!state.pendingDelete) {
      return;
    }
    const remaining = Math.max(0, state.pendingDelete.expiresAt - Date.now());
    els.toastBar.style.transform = `scaleX(${remaining / 3000})`;
  };
  tick();
  state.toastTick = window.setInterval(tick, 100);
}

function hideToast() {
  window.clearInterval(state.toastTick);
  els.toast.hidden = true;
}

function dateOf(value) {
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? new Date(0) : parsed;
}

function isSameDay(a, b) {
  return a.getFullYear() === b.getFullYear()
    && a.getMonth() === b.getMonth()
    && a.getDate() === b.getDate();
}

function dayBucket(date) {
  const today = new Date();
  if (isSameDay(date, today)) {
    return "Today";
  }
  const yesterday = new Date(today);
  yesterday.setDate(yesterday.getDate() - 1);
  if (isSameDay(date, yesterday)) {
    return "Yesterday";
  }
  const sameYear = date.getFullYear() === today.getFullYear();
  return date.toLocaleDateString("en-US", sameYear
    ? { month: "short", day: "numeric" }
    : { month: "short", day: "numeric", year: "numeric" });
}

function relTime(date) {
  const seconds = Math.max(1, Math.floor((Date.now() - date.getTime()) / 1000));
  if (seconds < 60) {
    return `${seconds}s`;
  }
  if (seconds < 3600) {
    return `${Math.floor(seconds / 60)}m`;
  }
  if (seconds < 86400) {
    return `${Math.floor(seconds / 3600)}h`;
  }
  return date.toLocaleTimeString("en-US", { hour: "numeric", minute: "2-digit" });
}

function groupByDay(items) {
  const groups = [];
  let last = "";
  for (const item of items) {
    const bucket = dayBucket(dateOf(item.created_at));
    if (bucket !== last) {
      groups.push({ label: bucket, items: [] });
      last = bucket;
    }
    groups[groups.length - 1].items.push(item);
  }
  return groups;
}

function displayTitle(title, project) {
  if (!project) {
    return title;
  }
  const safe = project.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const re = new RegExp(`\\s*[—\\-–]\\s*${safe}\\s*$`, "i");
  return title.replace(re, "").trim() || title;
}

els.apiKey.addEventListener("change", async () => {
  state.apiKey = els.apiKey.value.trim();
  if (state.apiKey) {
    localStorage.setItem("smth.apiKey", state.apiKey);
  } else {
    localStorage.removeItem("smth.apiKey");
  }
  connectStream();
  try {
    await loadArtifacts();
  } catch (err) {
    showToast(err.message);
  }
});

els.search.addEventListener("input", () => {
  state.query = els.search.value;
  render();
});

els.project.addEventListener("keydown", async (event) => {
  if (event.key !== "Enter") {
    return;
  }
  state.project = els.project.value.trim();
  state.nextCursor = "";
  state.selectedId = "";
  try {
    await loadArtifacts();
  } catch (err) {
    showToast(err.message);
  }
});

els.refresh.addEventListener("click", async () => {
  try {
    await loadArtifacts();
    showToast("refreshed");
  } catch (err) {
    showToast(err.message);
  }
});

els.more.addEventListener("click", () => {
  loadArtifacts({ append: true }).catch((err) => showToast(err.message));
});

els.copy.addEventListener("click", () => {
  copyLink().catch((err) => showToast(err.message));
});

els.open.addEventListener("click", openRaw);
els.delete.addEventListener("click", requestDelete);
els.toastUndo.addEventListener("click", undoDelete);

window.addEventListener("keydown", (event) => {
  const tag = (document.activeElement?.tagName || "").toLowerCase();
  const inField = tag === "input" || tag === "textarea";

  if ((event.metaKey || event.ctrlKey) && event.key === "k") {
    event.preventDefault();
    els.search.focus();
    els.search.select();
    return;
  }

  if (inField) {
    return;
  }

  if (event.key === "/") {
    event.preventDefault();
    els.search.focus();
    return;
  }
  if (event.key === "Escape") {
    state.query = "";
    els.search.value = "";
    els.search.blur();
    render();
    return;
  }
  if (event.key === "j" || event.key === "k") {
    const visible = filteredItems();
    if (!visible.length) {
      return;
    }
    event.preventDefault();
    const idx = Math.max(0, visible.findIndex((item) => item.id === state.selectedId));
    const nextIdx = event.key === "j"
      ? Math.min(visible.length - 1, idx + 1)
      : Math.max(0, idx - 1);
    selectItem(visible[nextIdx].id);
    return;
  }
  if (event.key === "Enter" && state.selectedId) {
    openRaw();
    return;
  }
  if (event.key === "d" && state.selectedId) {
    event.preventDefault();
    requestDelete();
  }
});

connectStream();
loadArtifacts().catch((err) => showToast(err.message));
