// Minimal frontend for cmd/server's JSON API - no build step, loaded
// straight as an ES module. Talks to /api/** on the same origin (Firebase
// Hosting rewrites that to the Cloud Run service; see firebase.json).
import { initializeApp } from "https://www.gstatic.com/firebasejs/11.0.2/firebase-app.js";
import {
  getAuth,
  GoogleAuthProvider,
  signInWithPopup,
  signOut,
  onAuthStateChanged,
} from "https://www.gstatic.com/firebasejs/11.0.2/firebase-auth.js";
import { firebaseConfig } from "./firebase-config.js";

const app = initializeApp(firebaseConfig);
const auth = getAuth(app);

const el = (id) => document.getElementById(id);
const authArea = el("auth-area");
const signedOut = el("signed-out");
const signedIn = el("signed-in");
const statusEl = el("status");
const workoutsBody = document.querySelector("#workouts tbody");
const limitSelect = el("limit-select");
const timezoneSelect = el("timezone-select");

let currentUser = null;
let lastWorkouts = []; // re-rendered on a timezone-preference change without re-fetching

// Remembered per-browser display preferences - not user data, so plain
// localStorage rather than anything synced through the backend.
const PREFS_KEY = "concept2upload:prefs";

function loadPrefs() {
  try {
    return JSON.parse(localStorage.getItem(PREFS_KEY)) || {};
  } catch {
    return {};
  }
}

function savePrefs(prefs) {
  try {
    localStorage.setItem(PREFS_KEY, JSON.stringify(prefs));
  } catch {
    // Private browsing / blocked storage - preferences just won't persist.
  }
}

const prefs = loadPrefs();
if (prefs.limit) limitSelect.value = prefs.limit;
if (prefs.timezone) timezoneSelect.value = prefs.timezone;

function setStatus(msg, isError = false) {
  statusEl.textContent = msg;
  statusEl.classList.toggle("error", isError);
}

async function authedFetch(path, options = {}) {
  if (!currentUser) throw new Error("not signed in");
  const idToken = await currentUser.getIdToken();
  const res = await fetch(path, {
    ...options,
    headers: {
      ...(options.headers || {}),
      Authorization: `Bearer ${idToken}`,
    },
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error || `${path} returned ${res.status}`);
  }
  return res;
}

el("sign-in").addEventListener("click", () => {
  signInWithPopup(auth, new GoogleAuthProvider()).catch((err) => setStatus(err.message, true));
});

el("save-token").addEventListener("click", async () => {
  const token = el("concept2-token").value.trim();
  if (!token) return;
  try {
    await authedFetch("/api/concept2-token", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token }),
    });
    el("concept2-token").value = "";
    setStatus("Concept2 token saved.");
    loadWorkouts();
  } catch (err) {
    setStatus(err.message, true);
  }
});

el("refresh-list").addEventListener("click", loadWorkouts);

limitSelect.addEventListener("change", () => {
  savePrefs({ ...loadPrefs(), limit: limitSelect.value });
  loadWorkouts();
});

timezoneSelect.addEventListener("change", () => {
  savePrefs({ ...loadPrefs(), timezone: timezoneSelect.value });
  renderWorkouts(lastWorkouts); // already have the data - just re-render with the new preference
});

async function loadWorkouts() {
  try {
    setStatus("Loading workouts...");
    const res = await authedFetch(`/api/workouts?limit=${limitSelect.value}`);
    lastWorkouts = await res.json();
    renderWorkouts(lastWorkouts);
    setStatus(`Loaded ${lastWorkouts.length} workout(s).`);
  } catch (err) {
    setStatus(err.message, true);
  }
}

// formatDate renders w.date according to the "Times in" preference:
// the viewer's own timezone (default), the timezone the workout was
// actually recorded in (w.timezone, an IANA name from Concept2 - not
// always present), or UTC.
function formatDate(w) {
  const date = new Date(w.date);
  const options = { dateStyle: "medium", timeStyle: "short" };
  switch (timezoneSelect.value) {
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

onAuthStateChanged(auth, (user) => {
  currentUser = user;
  if (user) {
    signedOut.classList.add("hidden");
    signedIn.classList.remove("hidden");
    authArea.innerHTML = "";
    const who = document.createElement("span");
    who.textContent = `${user.displayName || user.email} `;
    const btn = document.createElement("button");
    btn.textContent = "Sign out";
    btn.addEventListener("click", () => signOut(auth));
    authArea.appendChild(who);
    authArea.appendChild(btn);
    loadWorkouts();
  } else {
    signedOut.classList.remove("hidden");
    signedIn.classList.add("hidden");
    authArea.innerHTML = "";
    setStatus("");
  }
});
