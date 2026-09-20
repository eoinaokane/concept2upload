// Shared between index.html (app.js) and preferences.html (prefs.js):
// Firebase init/auth, an authenticated fetch helper, and the per-browser
// display preferences (never sent to the backend - these are just UI
// state, so plain localStorage rather than anything synced server-side).
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
export const auth = getAuth(app);
export { GoogleAuthProvider, signInWithPopup, signOut, onAuthStateChanged };

export async function authedFetch(path, options = {}) {
  const user = auth.currentUser;
  if (!user) throw new Error("not signed in");
  const idToken = await user.getIdToken();
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

const PREFS_KEY = "concept2upload:prefs";

export function loadPrefs() {
  try {
    return JSON.parse(localStorage.getItem(PREFS_KEY)) || {};
  } catch {
    return {};
  }
}

// savePrefs merges patch into the existing saved preferences (a partial
// update), so callers only need to name the keys they're changing.
export function savePrefs(patch) {
  try {
    localStorage.setItem(PREFS_KEY, JSON.stringify({ ...loadPrefs(), ...patch }));
  } catch {
    // Private browsing / blocked storage - preferences just won't persist.
  }
}

// wireAuthNav wires up the header's sign-in/sign-out UI (top right - just
// "Sign out", not who you are; see setAvatar below for that) and the
// signed-out/signed-in section toggle, shared by both pages. onSignedIn is
// called (with the Firebase user) each time a sign-in is detected, so
// callers that want to show "signed in as ..." can render that themselves
// wherever it belongs on their own page.
export function wireAuthNav({ signInBtn, authArea, signedOut, signedIn, extraNavHTML = "", onSignedIn, onSignedOut }) {
  signInBtn.addEventListener("click", () => {
    signInWithPopup(auth, new GoogleAuthProvider()).catch((err) => {
      authArea.textContent = "";
      console.error(err);
    });
  });

  onAuthStateChanged(auth, (user) => {
    if (user) {
      signedOut.classList.add("hidden");
      signedIn.classList.remove("hidden");
      authArea.innerHTML = extraNavHTML;
      const btn = document.createElement("button");
      btn.textContent = "Sign out";
      btn.addEventListener("click", () => signOut(auth));
      authArea.appendChild(btn);
      onSignedIn?.(user);
    } else {
      signedOut.classList.remove("hidden");
      signedIn.classList.add("hidden");
      authArea.innerHTML = "";
      onSignedOut?.();
    }
  });
}

// setAvatar shows/hides the header's account-photo thumbnail (top left,
// next to the title) based on sign-in state - pass it straight to
// wireAuthNav's onSignedIn/onSignedOut, or call it from your own.
export function setAvatar(imgEl, user) {
  if (user?.photoURL) {
    imgEl.src = user.photoURL;
    imgEl.alt = user.displayName || user.email || "Account photo";
    imgEl.classList.remove("hidden");
  } else {
    imgEl.classList.add("hidden");
    imgEl.removeAttribute("src");
  }
}
