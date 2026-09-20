// Frontend for the workouts list (cmd/server's JSON API) - no build step,
// loaded straight as an ES module. Talks to /api/** on the same origin
// (Firebase Hosting rewrites that to the Cloud Run service; see
// firebase.json). Concept2 token management and display preferences live
// on preferences.html (prefs.js) - see shared.js for what's common.
import { authedFetch, loadPrefs, savePrefs, wireAuthNav } from "./shared.js";

const el = (id) => document.getElementById(id);
const signedOut = el("signed-out");
const signedIn = el("signed-in");
const statusEl = el("status");
const workoutsBody = document.querySelector("#workouts tbody");
const limitSelect = el("limit-select");

let lastWorkouts = []; // kept so a "Times in"/"Time format" change elsewhere doesn't need a re-fetch

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
  loadWorkouts();
});

async function loadWorkouts() {
  try {
    setStatus("Loading workouts...");
    const res = await authedFetch(`/api/workouts?limit=${limitSelect.value}`);
    lastWorkouts = await res.json();
    renderWorkouts(lastWorkouts);
    setStatus(`Loaded ${lastWorkouts.length} workout(s).`);
  } catch (err) {
    if (/no concept2 token saved/i.test(err.message)) {
      workoutsBody.innerHTML = "";
      setStatusHTML('No Concept2 token saved yet - add one on the <a href="preferences.html">Preferences</a> page.', true);
      return;
    }
    setStatus(err.message, true);
  }
}

// formatDate renders w.date according to the "Times in"/"Time format"
// preferences (set on preferences.html): the viewer's own timezone
// (default), the timezone the workout was actually recorded in
// (w.timezone, an IANA name from Concept2 - not always present), or UTC;
// and 12-/24-hour clock, or the browser's own default.
function formatDate(w) {
  const date = new Date(w.date);
  const prefs = loadPrefs();
  const options = { dateStyle: "medium", timeStyle: "short" };
  if (prefs.hourFormat === "12") options.hour12 = true;
  else if (prefs.hourFormat === "24") options.hour12 = false;

  switch (prefs.timezone) {
    case "utc":
      return new Intl.DateTimeFormat(undefined, { ...options, timeZone: "UTC" }).format(date) + " UTC";
    case "recorded":
      if (!w.timezone) return new Intl.DateTimeFormat(undefined, options).format(date) + " (recorded tz unknown)";
      return new Intl.DateTimeFormat(undefined, { ...options, timeZone: w.timezone }).format(date);
    default:
      return new Intl.DateTimeFormat(undefined, options).format(date);
  }
}

function renderWorkouts(workouts) {
  workoutsBody.innerHTML = "";
  for (const w of workouts) {
    const tr = document.createElement("tr");

    const km = (w.distanceMetres / 1000).toFixed(1);
    tr.innerHTML = `
      <td>${formatDate(w)}</td>
      <td>${w.type}</td>
      <td>${km} km</td>
      <td>${w.timeFormatted}</td>
      <td>${w.workoutType || ""}</td>
      <td class="actions"></td>
    `;

    const actionsTd = tr.lastElementChild;

    const tcxLink = document.createElement("a");
    tcxLink.textContent = "Download .tcx";
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

wireAuthNav({
  signInBtn: el("sign-in"),
  authArea: el("auth-area"),
  signedOut,
  signedIn,
  extraNavHTML: '<a class="nav-link" href="preferences.html">Preferences</a>',
  onSignedIn: loadWorkouts,
  onSignedOut: () => setStatus(""),
});
