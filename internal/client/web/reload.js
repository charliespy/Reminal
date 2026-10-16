// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar
//
// A script of this page that did not load. The page's scripts are named by
// their content (assets/<name>.<hash>.js), so a page names exactly the build
// it was made with. One that loads from just before a deploy, or a relay
// upgrade, that no longer has its build finds nothing there: loaded again,
// it is the page of the current build. Once only, in case the network itself
// is what failed. This file is served at a fixed path and not hashed, and
// runs before any of the page's own scripts: the guard must not be one of
// the scripts it guards.
(function () {
  window.addEventListener('error', function (e) {
    var t = e && e.target;
    if (!t || t.tagName !== 'SCRIPT' || !/\/assets\//.test(t.src || '')) return;
    try {
      if (sessionStorage.getItem('reminal-asset-reload')) return;
      sessionStorage.setItem('reminal-asset-reload', '1');
    } catch (_) { return; }
    location.reload();
  }, true);
  window.addEventListener('load', function () { try { sessionStorage.removeItem('reminal-asset-reload'); } catch (_) {} });
})();
