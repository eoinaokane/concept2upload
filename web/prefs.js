// Preferences page: Concept2 token management (save/revoke) and the
// display preferences (timezone, time format) that app.js reads when
// rendering the workouts list. See shared.js for what's common with
// index.html.
import { authedFetch, loadPrefs, savePrefs, wireAuthNav } from "./shared.js";

const el = (id) => document.getElementById(id);
const statusEl = el("status");
const timezoneSelect = el("timezone-select");
const hourFormatSelect = el("hour-format-select");

function setStatus(msg, isError = false) {
  statusEl.textContent = msg;
  statusEl.classList.toggle("error", isError);
}

function setTokenStatus(msg) {
  el("token-status").textContent = msg;
}

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
    setTokenStatus("Token saved.");
  } catch (err) {
    setTokenStatus("");
    setStatus(err.message, true);
  }
});

el("revoke-token").addEventListener("click", async () => {
  if (!confirm("Remove your saved Concept2 token? You'll need to paste it again to use the workouts list.")) return;
  try {
    await authedFetch("/api/concept2-token", { method: "DELETE" });
    setTokenStatus("Token revoked.");
  } catch (err) {
    setTokenStatus("");
    setStatus(err.message, true);
  }
});

function loadDisplayPrefs() {
  const prefs = loadPrefs();
  timezoneSelect.value = prefs.timezone || "local";
  hourFormatSelect.value = prefs.hourFormat || "auto";
}

timezoneSelect.addEventListener("change", () => savePrefs({ timezone: timezoneSelect.value }));
hourFormatSelect.addEventListener("change", () => savePrefs({ hourFormat: hourFormatSelect.value }));

wireAuthNav({
  signInBtn: el("sign-in"),
  authArea: el("auth-area"),
  signedOut: el("signed-out"),
  signedIn: el("signed-in"),
  extraNavHTML: '<a class="nav-link" href="index.html">&larr; Workouts</a>',
  onSignedIn: loadDisplayPrefs,
  onSignedOut: () => setStatus(""),
});
