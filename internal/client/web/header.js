  // ── header fit: one row at every width ──────────────────────────────
  // header.tight (see its CSS) whenever the wordmark, toolbar and status pill
  // would not share one row; then header.icons, and header.snug, while the
  // toolbar still overflows the room left to it. Each step is measured and
  // decided in the same frame, so nothing flickers; re-checked when the
  // width, the visible controls, their labels or the status text change.
  (function () {
    const header = document.querySelector('header');
    const form = document.getElementById('session-form');
    const status = document.getElementById('status');
    if (!header || !form) return;
    let queued = false;
    // Two boxes are on different rows when one starts below where the other
    // ends. Comparing tops alone would not do: items of different heights,
    // centred on one row, have different tops.
    function onTwoRows(els) {
      const r = els.filter(e => e && e.offsetParent !== null).map(e => e.getBoundingClientRect());
      return r.some(a => r.some(b => a.top >= b.bottom - 2));
    }
    function wraps() {
      return onTwoRows([header.querySelector('h1'), form, status]) || onTwoRows([...form.children]);
    }
    const overflows = () => form.scrollWidth > form.clientWidth + 1;
    // The toolbar's labelled controls: a menu's summary, or a button.
    function controls() {
      return [...form.children].map(c => c.tagName === 'DETAILS' ? c.querySelector(':scope > summary') : c)
        .filter(c => c && (c.tagName === 'SUMMARY' || c.tagName === 'BUTTON'));
    }
    // A control's label is its own text, not the caret's.
    function labelOf(c) {
      return [...c.childNodes].filter(n => n.nodeType === 3).map(n => n.textContent).join('').trim();
    }
    // Every control is named by its label for screen readers, and while the
    // label is hidden by its title too (a tooltip on hover or long-press).
    // A title the control already has is its own and stays.
    function nameControls(iconsOnly) {
      for (const c of controls()) {
        const l = labelOf(c);
        if (!l) continue;
        if (c.getAttribute('aria-label') !== l) c.setAttribute('aria-label', l);
        if (c.hasAttribute('title') && !c.hasAttribute('data-fit-title')) continue;
        if (iconsOnly) { c.setAttribute('title', l); c.setAttribute('data-fit-title', ''); }
        else if (c.hasAttribute('data-fit-title')) { c.removeAttribute('title'); c.removeAttribute('data-fit-title'); }
      }
    }
    function fit() {
      queued = false;
      if (form.classList.contains('menu-open')) return; // never re-lay out under an open menu
      header.classList.remove('tight', 'icons', 'snug');
      if (wraps()) header.classList.add('tight');
      // On a phone the toolbar scrolls without ever wrapping, so overflow is
      // checked on its own, not only once tight.
      if (overflows()) {
        // Keep the labelled height: an icon alone is shorter, and a smaller
        // target than the one it replaces is harder to hit.
        const h = Math.max(0, ...controls().filter(c => c.offsetParent !== null).map(c => c.offsetHeight));
        if (h) header.style.setProperty('--ctl-h', h + 'px');
        header.classList.add('icons');
        if (overflows()) header.classList.add('snug');
      }
      nameControls(header.classList.contains('icons'));
    }
    function queue() { if (!queued) { queued = true; requestAnimationFrame(fit); } }
    window.addEventListener('resize', queue);
    // Only the toolbar's own controls matter: a dropdown list updating deep
    // inside it must not force a layout every frame. Its class is watched too,
    // so closing a menu (menu-open going away) re-fits the row.
    new MutationObserver(ms => {
      if (ms.some(m => m.target === form || m.target.parentElement === form)) queue();
    }).observe(form, { subtree: true, childList: true, attributes: true, attributeFilter: ['hidden', 'style', 'class'] });
    // A label changing (Connect → Disconnect) changes the control's width.
    for (const c of controls()) new MutationObserver(queue).observe(c, { childList: true, characterData: true, subtree: true });
    if (status) new MutationObserver(queue).observe(status, { childList: true, characterData: true, subtree: true });
    new MutationObserver(queue).observe(document.body, { attributes: true, attributeFilter: ['class'] });
    queue();
  })();
