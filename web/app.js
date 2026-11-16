/* open-rmslist frontend — vanilla JS + Leaflet. No build step. */
"use strict";

const state = {
  stations: [],
  serviceCodes: [],      // [{code, count}]
  config: {},
  conditions: null,
  ranked: new Map(),     // callsign -> best ranked entry
  filters: { serviceCode: "", mode: "", band: "" },
  theme: "dark",
};

const $ = (id) => document.getElementById(id);

const BAND_ORDER = ["160m","80m","60m","40m","30m","20m","17m","15m","12m","10m","6m","2m","1.25m","70cm","33cm","23cm","HF","Other"];
const MODE_ORDER = ["VARA","VARA FM","Mercury","VardeFM","ARDOP","Packet","PACTOR","Robust Packet","WINMOR","Other"];

/* ---------- theme ---------- */
function applyTheme(theme) {
  state.theme = theme;
  document.documentElement.dataset.theme = theme;
  try { localStorage.setItem("rmslist-theme", theme); } catch (_) {}
  $("theme-btn").textContent = theme === "dark" ? "☀" : "◐";
  switchMapTiles(theme);
}

/* ---------- map ---------- */
let map, markerLayer, rankLayer, darkTiles, lightTiles;

function initMap() {
  map = L.map("map", { zoomControl: true, worldCopyJump: true }).setView([20, 0], 2);

  darkTiles = L.tileLayer("https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png", {
    attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> &copy; <a href="https://carto.com/attributions">CARTO</a>',
    subdomains: "abcd",
    maxZoom: 19,
  });
  lightTiles = L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
    attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
    maxZoom: 19,
  });

  markerLayer = L.layerGroup().addTo(map);
  rankLayer = L.layerGroup().addTo(map);

  switchMapTiles(state.theme);
}

function switchMapTiles(theme) {
  if (!map) return;
  const want = theme === "dark" ? darkTiles : lightTiles;
  const other = theme === "dark" ? lightTiles : darkTiles;
  if (!map.hasLayer(want)) want.addTo(map);
  if (map.hasLayer(other)) map.removeLayer(other);
}

/* ---------- helpers ---------- */
async function fetchJSON(url, opts) {
  const res = await fetch(url, opts);
  if (!res.ok) throw new Error(`${url} -> ${res.status}`);
  return res.json();
}

function freshnessColor(hours) {
  if (hours == null || hours < 0) return "#8b949e";
  if (hours < 2) return getCSS("--green");
  if (hours < 4) return getCSS("--yellow");
  return getCSS("--red");
}

function getCSS(name) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || "#999";
}

function qualityBadge(q) {
  if (q == null) return "";
  const cls = q >= 70 ? "q-good" : q >= 40 ? "q-fair" : "q-poor";
  return `<span class="q-badge ${cls}">${q}%</span>`;
}

function modeDisplay(station) {
  // Return a readable "mode(s)" string for the station.
  const modes = [...new Set(station.channels.map((c) => c.mode))].slice(0, 3);
  return modes.join(" / ");
}

/* ---------- data ---------- */
async function load() {
  const [config, serviceCodes, gateways, conditions] = await Promise.all([
    fetchJSON("/api/config"),
    fetchJSON("/api/servicecodes"),
    fetchJSON("/api/gateways"),
    fetchJSON("/api/conditions").catch(() => null),
  ]);
  state.config = config;
  state.serviceCodes = serviceCodes;
  state.stations = gateways || [];
  state.conditions = conditions;

  $("status").textContent =
    `${state.stations.length} gateways · ` + (conditions ? "conditions ready" : "no conditions");
  populateServiceCodes();
  populateBandMode();
  renderConditions();
  applyFilters();
}

function populateServiceCodes() {
  const sel = $("service-code");
  sel.innerHTML = "";
  const all = document.createElement("option");
  all.value = "";
  all.textContent = "All service codes";
  sel.appendChild(all);

  const counts = new Map(state.serviceCodes.map((s) => [s.code, s.count]));
  const codes = new Set((state.config.service_codes || []).concat([...counts.keys()]));
  for (const code of codes) {
    const opt = document.createElement("option");
    opt.value = code;
    const n = counts.get(code);
    opt.textContent = code + (n != null ? ` (${n})` : "");
    sel.appendChild(opt);
  }
}

function populateBandMode() {
  const bands = new Set();
  const modes = new Set();
  for (const st of state.stations) {
    for (const ch of st.channels) {
      if (ch.band) bands.add(ch.band);
      if (ch.mode_family) modes.add(ch.mode_family);
    }
  }

  const bandSel = $("band");
  bandSel.innerHTML = "";
  bandSel.appendChild(opt("", "All bands"));
  const orderedBands = BAND_ORDER.filter((b) => bands.has(b)).concat(
    [...bands].filter((b) => !BAND_ORDER.includes(b)).sort());
  for (const b of orderedBands) bandSel.appendChild(opt(b, b));

  const modeSel = $("mode");
  modeSel.innerHTML = "";
  modeSel.appendChild(opt("", "All modes"));
  const orderedModes = MODE_ORDER.filter((m) => modes.has(m)).concat(
    [...modes].filter((m) => !MODE_ORDER.includes(m)).sort());
  for (const m of orderedModes) modeSel.appendChild(opt(m, m));
}

function opt(value, text) {
  const o = document.createElement("option");
  o.value = value;
  o.textContent = text;
  return o;
}

/* ---------- filtering + rendering ---------- */
function filteredStations() {
  const { serviceCode, mode, band } = state.filters;
  const out = [];
  for (const st of state.stations) {
    const channels = st.channels.filter((c) => {
      if (serviceCode && c.service_code !== serviceCode) return false;
      if (mode && c.mode_family !== mode) return false;
      if (band && c.band !== band) return false;
      return true;
    });
    if (channels.length) out.push({ ...st, channels });
  }
  return out;
}

function applyFilters() {
  const stations = filteredStations();
  $("count").textContent = `${stations.length}`;
  renderMarkers(stations);
  renderList(stations);
}

function renderMarkers(stations) {
  markerLayer.clearLayers();
  rankLayer.clearLayers();

  for (const st of stations) {
    if (!st.latitude && !st.longitude) continue;
    const color = freshnessColor(st.hours_since_status);
    const marker = L.circleMarker([st.latitude, st.longitude], {
      radius: 6,
      color: "#00000055",
      weight: 1,
      fillColor: color,
      fillOpacity: 0.9,
    });
    marker.bindPopup(popupHTML(st), { maxWidth: 320 });
    markerLayer.addLayer(marker);

    // Ranked highlight (larger, white-ringed, colored by quality).
    const r = state.ranked.get(st.callsign);
    if (r && $("rank-mode").checked) {
      const rc = r.link_quality >= 70 ? getCSS("--green") : r.link_quality >= 40 ? getCSS("--yellow") : getCSS("--red");
      const rm = L.circleMarker([st.latitude, st.longitude], {
        radius: 11,
        color: "#ffffff",
        weight: 1.5,
        fillColor: rc,
        fillOpacity: 0.85,
      });
      rm.bindPopup(popupHTML(st, r), { maxWidth: 320 });
      rankLayer.addLayer(rm);
    }
  }
}

function popupHTML(st, rank) {
  const channels = st.channels.map((c) =>
    `<div class="ch-row"><span class="ch-mode">${esc(c.mode)}</span>` +
    `<span class="ch-freq">${esc(c.frequency || fmtHz(c.frequency_hz))}</span>` +
    `<span class="ch-sc">${esc(c.service_code || "PUBLIC")}</span></div>`).join("");
  const rankLine = rank
    ? `<div><b>Link quality:</b> ${rank.link_quality}% · ${rank.distance_km.toFixed(0)} km · ${rank.band_open ? "open" : "closed"}` +
      (rank.muf_mhz ? ` · MUF ${rank.muf_mhz.toFixed(1)} MHz` : "") +
      (rank.note ? ` · ${esc(rank.note)}` : "") + `</div>`
    : "";
  return `<div><b>${esc(st.callsign)}</b></div>` +
    `<div class="station-meta">${esc(st.gridsquare || firstGrid(st))} · ${freshText(st.hours_since_status)}</div>` +
    `<div style="margin-top:4px">${channels}</div>` +
    rankLine +
    (st.comments ? `<div class="station-meta">${esc(st.comments)}</div>` : "");
}

function firstGrid(st) {
  return st.channels[0]?.gridsquare || "";
}

function fmtHz(hz) {
  if (!hz) return "";
  if (hz >= 1e6) return (hz / 1e6).toFixed(3).replace(/0+$/, "").replace(/\.$/, "") + " MHz";
  if (hz >= 1e3) return (hz / 1e3).toFixed(1) + " kHz";
  return hz + " Hz";
}

function freshText(h) {
  if (h == null || h < 0) return "unknown";
  return `${h}h ago`;
}

function renderList(stations) {
  const list = $("list");
  list.innerHTML = "";

  const sorted = [...stations];
  if (state.ranked.size) {
    sorted.sort((a, b) => {
      const qa = state.ranked.get(a.callsign)?.link_quality ?? -1;
      const qb = state.ranked.get(b.callsign)?.link_quality ?? -1;
      if (qa !== qb) return qb - qa;
      return a.callsign.localeCompare(b.callsign);
    });
  } else {
    sorted.sort((a, b) => a.callsign.localeCompare(b.callsign));
  }

  if (!sorted.length) {
    list.innerHTML = `<li class="empty">No gateways match the current filters.</li>`;
    return;
  }

  for (const st of sorted) {
    const li = document.createElement("li");
    const r = state.ranked.get(st.callsign);
    const fc = st.hours_since_status == null || st.hours_since_status < 0 ? "fresh-gray"
      : st.hours_since_status < 2 ? "fresh-green"
      : st.hours_since_status < 4 ? "fresh-yellow" : "fresh-red";
    li.innerHTML =
      `<div style="display:flex;align-items:center">` +
        `<span class="fresh ${fc}"></span>` +
        `<span class="station-call">${esc(st.callsign)}<span class="sq">${esc(firstGrid(st))}</span></span>` +
        (r ? qualityBadge(r.link_quality) : "") +
      `</div>` +
      `<div class="station-meta">${esc(modeDisplay(st))} · ${freshText(st.hours_since_status)}` +
        (r ? ` · ${r.distance_km.toFixed(0)} km` : "") +
        (r && r.note ? ` · ${esc(r.note)}` : "") + `</div>` +
      `<div class="station-channels">${st.channels.slice(0, 4).map((c) =>
        `<div class="ch-row"><span class="ch-mode">${esc(c.mode)}</span><span class="ch-freq">${esc(c.frequency || fmtHz(c.frequency_hz))}</span><span class="ch-sc">${esc(c.service_code || "PUBLIC")}</span></div>`).join("")}` +
        (st.channels.length > 4 ? `<div class="station-meta">+${st.channels.length - 4} more</div>` : "") +
      `</div>`;
    li.addEventListener("click", () => {
      if (!st.latitude && !st.longitude) return;
      map.setView([st.latitude, st.longitude], Math.max(map.getZoom(), 8));
    });
    list.appendChild(li);
  }
}

function esc(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

/* ---------- ranking ---------- */
async function doRank() {
  const grid = $("grid").value.trim();
  if (!grid) {
    setNote("Enter your 4- or 6-character grid locator to rank.");
    $("grid").focus();
    return;
  }
  const req = {
    grid,
    band: $("band").value,
    mode: $("mode").value,
    service_code: $("service-code").value,
  };
  setNote("Ranking…");
  try {
    const results = await fetchJSON("/api/rank", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(req),
    });
    state.ranked = new Map();
    for (const r of results) {
      if (!state.ranked.has(r.callsign) || state.ranked.get(r.callsign).link_quality < r.link_quality) {
        state.ranked.set(r.callsign, r);
      }
    }
    let msg = `${results.length} candidate${results.length === 1 ? "" : "s"} ranked for ` +
      `${req.band || "all bands"} ${req.mode || "all modes"} from ${grid}.`;
    if (!state.config.voacap) {
      msg += " · point-to-point VOACAP forecasts off (install voacapl or set -voacap-url).";
    }
    setNote(msg);
    applyFilters();
  } catch (err) {
    setNote("Ranking failed: " + err.message);
  }
}

function clearRank() {
  state.ranked = new Map();
  setNote("");
  applyFilters();
}

/* ---------- conditions ---------- */
function renderConditions() {
  const c = state.conditions;
  const bar = $("conditions");
  if (!c) { bar.hidden = true; return; }
  bar.hidden = false;
  $("sfi").textContent = c.sfi || "–";
  $("a").textContent = c.a_index || "–";
  $("k").textContent = c.k_index || "–";
  $("cond-updated").textContent = c.updated ? new Date(c.updated).toLocaleString() : "–";
  $("cond-stale").hidden = !c.stale;
}

function setNote(msg) {
  const el = $("cond-note");
  el.textContent = msg || "";
  $("conditions").hidden = !(msg || state.conditions);
}

/* ---------- events ---------- */
function wireEvents() {
  $("service-code").addEventListener("change", (e) => {
    state.filters.serviceCode = e.target.value;
    clearRank();
  });
  $("mode").addEventListener("change", (e) => { state.filters.mode = e.target.value; clearRank(); });
  $("band").addEventListener("change", (e) => { state.filters.band = e.target.value; clearRank(); });
  $("grid").addEventListener("change", () => { $("grid").value = $("grid").value.trim().toUpperCase(); });
  $("rank-btn").addEventListener("click", doRank);
  $("rank-mode").addEventListener("change", applyFilters);
  $("theme-btn").addEventListener("click", () => applyTheme(state.theme === "dark" ? "light" : "dark"));
}

/* ---------- boot ---------- */
(async function boot() {
  let theme = "dark";
  try { theme = localStorage.getItem("rmslist-theme") || "dark"; } catch (_) {}
  applyTheme(theme);
  initMap();
  wireEvents();
  try {
    await load();
  } catch (err) {
    $("status").textContent = "failed to load: " + err.message;
    setNote("Could not load data — is the server running?");
  }
})();
