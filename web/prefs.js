// Preferences page: Concept2 token management (save/revoke) and the
// display preferences (timezone, time format) that app.js reads when
// rendering the workouts list. See shared.js for what's common with
// index.html.
import {
  authedFetch,
  loadPrefs,
  savePrefs,
  clearPrefs,
  wireAuthNav,
  setAvatar,
  auth,
  GoogleAuthProvider,
  reauthenticateWithPopup,
  deleteUser,
} from "./shared.js";

const el = (id) => document.getElementById(id);
const statusEl = el("status");
const timezoneSelect = el("timezone-select");
const hourFormatSelect = el("hour-format-select");
const dateFormatSelect = el("date-format-select");
const tokenConnected = el("token-connected");
const tokenEntry = el("token-entry");

function setStatus(msg, isError = false) {
  statusEl.textContent = msg;
  statusEl.classList.toggle("error", isError);
}

function setTokenStatus(msg) {
  el("token-status").textContent = msg;
}

// setTokenConnected switches between the two token states: connected (lead
// with Revoke, the only thing most people need after initial setup) and
// not connected (lead with the entry form).
function setTokenConnected(saved) {
  tokenConnected.classList.toggle("hidden", !saved);
  tokenEntry.classList.toggle("hidden", saved);
}

async function loadTokenStatus() {
  try {
    const res = await authedFetch("/api/concept2-token");
    const { saved } = await res.json();
    setTokenConnected(saved);
  } catch (err) {
    setStatus(err.message, true);
  }
}

el("show-replace").addEventListener("click", () => {
  tokenEntry.classList.remove("hidden");
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
    setTokenConnected(true);
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
    setTokenConnected(false);
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
  dateFormatSelect.value = prefs.dateFormat || "eu";
}

timezoneSelect.addEventListener("change", () => savePrefs({ timezone: timezoneSelect.value }));
hourFormatSelect.addEventListener("change", () => savePrefs({ hourFormat: hourFormatSelect.value }));
dateFormatSelect.addEventListener("change", () => savePrefs({ dateFormat: dateFormatSelect.value }));

// Deletes, in order: the Firestore account doc (server-side data), the
// local display prefs, then the Firebase Auth account itself - the most
// destructive step last, so a failure partway through never leaves the
// account deleted with data still attached to it. Re-authenticating first
// is required by Firebase for deleteUser() and doubles as an "are you
// sure" check the user can back out of via the Google popup.
el("delete-account").addEventListener("click", async () => {
  if (!confirm("Delete your Concept2 token, preferences, and account? This can't be undone - signing in again later starts a brand-new account.")) return;

  const user = auth.currentUser;
  try {
    setStatus("Confirm with Google to continue...");
    await reauthenticateWithPopup(user, new GoogleAuthProvider());

    setStatus("Deleting your data...");
    await authedFetch("/api/account", { method: "DELETE" });
    clearPrefs();

    setStatus("Deleting your account...");
    await deleteUser(user);
  } catch (err) {
    setStatus(err.message, true);
  }
});

function showAccountInfo(user) {
  const who = user.displayName ? `${user.displayName} (${user.email})` : user.email;
  el("account-info").textContent = `Signed in as ${who}`;
}

const avatarImg = el("avatar-img");

wireAuthNav({
  signInBtn: el("sign-in"),
  authArea: el("auth-area"),
  signedOut: el("signed-out"),
  signedIn: el("signed-in"),
  extraNavHTML: '<a class="nav-link" href="index.html">&larr; Workouts</a>',
  onSignedIn: (user) => {
    setAvatar(avatarImg, user);
    showAccountInfo(user);
    loadDisplayPrefs();
    loadTokenStatus();
  },
  onSignedOut: () => {
    setAvatar(avatarImg, null);
    setStatus("");
  },
});
