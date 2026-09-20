// Frontend for the workouts list (cmd/server's JSON API) - no build step,
// loaded straight as an ES module. Talks to /api/** on the same origin
// (Firebase Hosting rewrites that to the Cloud Run service; see
// firebase.json). Concept2 token management and display preferences live
// on preferences.html (prefs.js) - see shared.js for what's common.
import { authedFetch, loadPrefs, savePrefs, wireAuthNav, setAvatar } from "./shared.js";

// Concept2's own per-request cap (see internal/concept2.ListLatest). One
// fetch pulls everything available up to this, sorted newest-first by the
// backend; "Show" and Previous/Next then just slice through it client-side
// - no extra network round trip per page.
const MAX_FETCH = 250;

const el = (id) => document.getElementById(id);
const signedOut = el("signed-out");
const signedIn = el("signed-in");
const statusEl = el("status");
const workoutsBody = document.querySelector("#workouts tbody");
const limitSelect = el("limit-select");
const prevBtn = el("prev-page");
const nextBtn = el("next-page");
const pageInfo = el("page-info");

let allWorkouts = [];
let currentPage = 1;

const savedLimit = loadPrefs().workoutsLimit;
if (savedLimit) limitSelect.value = savedLimit;

function setStatus(msg, isError = false) {
  statusEl.textContent = msg;
  statusEl.classList.toggle("error", isError);
}

function setStatusHTML(html, isError = false) {
  statusEl.innerHTML = html;
  statusEl.classList.toggle("error", isError);
}

el("refresh-list").addEventListener("click", loadWorkouts);

limitSelect.addEventListener("change", () => {
  savePrefs({ workoutsLimit: limitSelect.value });
  currentPage = 1;
  renderPage();
});

prevBtn.addEventListener("click", () => {
  currentPage--;
  renderPage();
});

nextBtn.addEventListener("click", () => {
  currentPage++;
  renderPage();
});

async function loadWorkouts() {
  try {
    setStatus("Loading workouts...");
    const res = await authedFetch(`/api/workouts?limit=${MAX_FETCH}`);
    allWorkouts = await res.json();
    currentPage = 1;
    renderPage();
  } catch (err) {
    if (/no concept2 token saved/i.test(err.message)) {
      allWorkouts = [];
      renderPage();
      setStatusHTML('No Concept2 token saved yet - add one on the <a href="preferences.html">Preferences</a> page.', true);
      return;
    }
    setStatus(err.message, true);
  }
}

// renderPage slices allWorkouts into pages of the "Show" size and renders
// whichever page currentPage points at, updating the Previous/Next controls
// to match - all in memory, no re-fetch.
function renderPage() {
  const pageSize = parseInt(limitSelect.value, 10) || 10;
  const totalPages = Math.max(1, Math.ceil(allWorkouts.length / pageSize));
  currentPage = Math.min(Math.max(currentPage, 1), totalPages);

  const start = (currentPage - 1) * pageSize;
  renderWorkouts(allWorkouts.slice(start, start + pageSize));

  pageInfo.textContent = `Page ${currentPage} of ${totalPages}`;
  prevBtn.disabled = currentPage <= 1;
  nextBtn.disabled = currentPage >= totalPages;

  if (allWorkouts.length > 0) {
    setStatus(`Loaded ${allWorkouts.length} workout(s).`);
  }
}

// formatDateParts renders just the day/month/year of `date` in the given
// timeZone (or the browser's own, if undefined), ordered per the
// "Date format" preference: "eu" (DD/MM/YY, the default) or "us"
// (MM/DD/YY). Built from formatToParts rather than dateStyle so the
// day/month order is under our control instead of the browser locale's.
function formatDateParts(date, timeZone, dateFormat) {
  const parts = new Intl.DateTimeFormat("en-CA", {
    day: "2-digit",
    month: "2-digit",
    year: "2-digit",
    ...(timeZone ? { timeZone } : {}),
  }).formatToParts(date);
  const get = (type) => parts.find((p) => p.type === type).value;
  const day = get("day");
  const month = get("month");
  const year = get("year");
  return dateFormat === "us" ? `${month}/${day}/${year}` : `${day}/${month}/${year}`;
}

// formatDate renders w.date according to the "Times in"/"Time format"/"Date
// format" preferences (set on preferences.html): the viewer's own timezone
// (default), the timezone the workout was actually recorded in
// (w.timezone, an IANA name from Concept2 - not always present), or UTC;
// 12-/24-hour clock, or the browser's own default; and DD/MM/YY (default)
// or MM/DD/YY. Returns { date, time } separately so the table can hide the
// clock time on small screens and keep just the date.
function formatDate(w) {
  const date = new Date(w.date);
  const prefs = loadPrefs();
  const timeOptions = { timeStyle: "short" };
  if (prefs.hourFormat === "12") timeOptions.hour12 = true;
  else if (prefs.hourFormat === "24") timeOptions.hour12 = false;

  let timeZone;
  let suffix = "";
  if (prefs.timezone === "utc") {
    timeZone = "UTC";
    suffix = " UTC";
  } else if (prefs.timezone === "recorded") {
    if (!w.timezone) {
      const time = new Intl.DateTimeFormat(undefined, timeOptions).format(date);
      return { date: formatDateParts(date, undefined, prefs.dateFormat), time: `${time} (recorded tz unknown)` };
    }
    timeZone = w.timezone;
  }

  const time = new Intl.DateTimeFormat(undefined, { ...timeOptions, ...(timeZone ? { timeZone } : {}) }).format(date);
  return { date: formatDateParts(date, timeZone, prefs.dateFormat), time: `${time}${suffix}` };
}

// TYPE_ICONS covers Concept2's machine types; anything not listed here
// falls back to showing its raw type string even on small screens, rather
// than a meaningless icon.
const TYPE_ICONS = {
  rower: "\u{1F6A3}",
  bike: "\u{1F6B4}",
  skierg: "\u{1F3BF}",
  dynamic: "\u{1F30A}", // the Dynamic's sliding rail mimics rowing on water
};

function typeIcon(type) {
  return TYPE_ICONS[type] || type;
}

function renderWorkouts(workouts) {
  workoutsBody.innerHTML = "";
  for (const w of workouts) {
    const tr = document.createElement("tr");

    const km = (w.distanceMetres / 1000).toFixed(1);
    const { date, time } = formatDate(w);
    tr.innerHTML = `
      <td><span class="date-part">${date}</span><span class="time-part">, ${time}</span></td>
      <td><span class="type-text">${w.type}</span><span class="type-icon" aria-hidden="true">${typeIcon(w.type)}</span></td>
      <td>${km} km</td>
      <td>${w.timeFormatted}</td>
      <td>${w.workoutType || ""}</td>
      <td class="actions"></td>
    `;

    const actionsTd = tr.lastElementChild;

    const tcxLink = document.createElement("a");
    tcxLink.innerHTML = `
      <span class="action-text">Download .tcx</span>
      <span class="action-icon" aria-hidden="true">
        <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M12 3v12" />
          <path d="m7 10 5 5 5-5" />
          <path d="M5 21h14" />
        </svg>
      </span>
    `;
    tcxLink.title = "Download .tcx";
    tcxLink.href = "#";
    tcxLink.addEventListener("click", async (e) => {
      e.preventDefault();
      try {
        const res = await authedFetch(`/api/workouts/${w.id}/tcx`);
        const blob = await res.blob();
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = `workout-${w.id}.tcx`;
        a.click();
        URL.revokeObjectURL(url);
      } catch (err) {
        setStatus(err.message, true);
      }
    });
    actionsTd.appendChild(tcxLink);

    workoutsBody.appendChild(tr);
  }
}

const avatarImg = el("avatar-img");

wireAuthNav({
  signInBtn: el("sign-in"),
  authArea: el("auth-area"),
  signedOut,
  signedIn,
  extraNavHTML: '<a class="nav-link" href="preferences.html">Preferences</a>',
  onSignedIn: (user) => {
    setAvatar(avatarImg, user);
    loadWorkouts();
  },
  onSignedOut: () => {
    setAvatar(avatarImg, null);
    setStatus("");
  },
});
