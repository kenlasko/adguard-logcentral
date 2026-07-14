// Behaviour for the logs page. Served as a same-origin file (not inline) so the
// strict Content-Security-Policy (script-src 'self', no 'unsafe-inline' or
// 'unsafe-eval') can stay in place. Loaded with defer, after htmx, so the DOM
// and window.htmx are both ready when this runs.

// Reformat every timestamp into the viewer's own timezone. The server renders
// an unambiguous UTC value in each <time datetime>; here we replace the text
// with the browser-local date and time. Runs on load and after every htmx swap
// (load-more, auto-refresh, filter changes) so new rows convert too.
(function () {
  function pad(n) { return n < 10 ? '0' + n : '' + n; }
  function formatLocal(d) {
    return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate()) +
      ' ' + pad(d.getHours()) + ':' + pad(d.getMinutes()) + ':' + pad(d.getSeconds());
  }
  function localize(root) {
    var nodes = (root || document).querySelectorAll('time[datetime]:not([data-tz])');
    for (var i = 0; i < nodes.length; i++) {
      var el = nodes[i];
      var d = new Date(el.getAttribute('datetime'));
      if (!isNaN(d.getTime())) { el.textContent = formatLocal(d); }
      el.setAttribute('data-tz', '');
    }
  }
  localize(document);
  // Rescan the whole document after any swap; the :not([data-tz]) guard makes
  // this idempotent and cheap, and covers outerHTML swaps (load-more) whose
  // event target does not contain the freshly inserted rows.
  document.body.addEventListener('htmx:afterSwap', function () { localize(document); });
})();

// Auto-refresh the log while the toggle is on. Driven from JavaScript rather
// than an htmx "every 10s [expr]" trigger: htmx compiles the bracket filter
// with the Function constructor, which the CSP's lack of 'unsafe-eval' forbids,
// so the filter would silently fail and the poll fire unconditionally. Here the
// checkbox is consulted directly, and the refresh reuses the tbody's hx-get and
// hx-include via a custom "refresh" event.
(function () {
  var INTERVAL_MS = 10000;
  var auto = document.getElementById('auto');
  var body = document.getElementById('log-body');
  if (!auto || !body) { return; }
  window.setInterval(function () {
    if (auto.checked && window.htmx) { window.htmx.trigger(body, 'refresh'); }
  }, INTERVAL_MS);
})();

// Persist the operator's chosen block target instance across visits.
(function () {
  var sel = document.getElementById('block-target');
  if (!sel) { return; }
  var key = 'adguard-block-target';
  var saved = localStorage.getItem(key);
  if (saved) {
    for (var i = 0; i < sel.options.length; i++) {
      if (sel.options[i].value === saved) { sel.value = saved; break; }
    }
  }
  sel.addEventListener('change', function () {
    localStorage.setItem(key, sel.value);
  });
})();

// Click a client or domain cell to filter the log by that exact value.
(function () {
  var table = document.querySelector('table.logs');
  var search = document.querySelector('#filters input[name=search]');
  if (!table || !search) { return; }
  table.addEventListener('click', function (ev) {
    var btn = ev.target.closest('.cell-filter');
    if (!btn) { return; }
    // A long-press swallows the click that would otherwise filter (see below).
    if (table.dataset.longPressed === 'true') { return; }
    search.value = btn.getAttribute('data-value');
    // Fire the same event the filter form listens for, triggering a refetch.
    search.dispatchEvent(new Event('change', { bubbles: true }));
  });
})();

// Long-press a log row to block or unblock its domain via a confirmation
// modal. This is the touch-only replacement for the per-row Block button,
// which the mobile card layout hides.
(function () {
  var table = document.querySelector('table.logs');
  var modal = document.getElementById('block-modal');
  var targetSel = document.getElementById('block-target');
  if (!table || !modal) { return; }

  var LONG_PRESS_MS = 500;
  var MOVE_TOLERANCE = 10;
  var confirmBtn = document.getElementById('block-modal-confirm');
  var errorEl = document.getElementById('block-modal-error');
  var timer = null;
  var startX = 0, startY = 0;
  var pending = null; // { row, domain, action, instance }

  function clearTimer() {
    if (timer) { window.clearTimeout(timer); timer = null; }
  }

  function openModal(row) {
    var blocked = row.getAttribute('data-blocked') === 'true';
    var action = blocked ? 'unblock' : 'block';
    var label = blocked ? 'Unblock' : 'Block';
    var instance = targetSel ? targetSel.value : '';
    pending = { row: row, domain: row.getAttribute('data-domain'), action: action, instance: instance };

    document.getElementById('block-modal-title').textContent = label + ' domain';
    document.getElementById('block-modal-action').textContent = label;
    document.getElementById('block-modal-domain').textContent = pending.domain;
    document.getElementById('block-modal-instance').textContent = instance;
    confirmBtn.textContent = label;
    confirmBtn.className = 'modal-btn modal-confirm ' + (blocked ? 'is-unblock' : 'is-block');
    confirmBtn.disabled = false;
    errorEl.hidden = true;

    modal.hidden = false;
    document.body.classList.add('modal-open');
    confirmBtn.focus();
  }

  function closeModal() {
    modal.hidden = true;
    document.body.classList.remove('modal-open');
    pending = null;
  }

  function submit() {
    if (!pending) { return; }
    var body = new URLSearchParams();
    body.set('domain', pending.domain);
    body.set('action', pending.action);
    body.set('instance', pending.instance);
    confirmBtn.disabled = true;
    errorEl.hidden = true;
    fetch('/partials/block', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: body.toString(),
      credentials: 'same-origin'
    }).then(function (resp) {
      if (!resp.ok) { throw new Error('block request failed'); }
      // Reflect the new state so a later long-press offers the inverse action.
      pending.row.setAttribute('data-blocked', pending.action === 'block' ? 'true' : 'false');
      closeModal();
    }).catch(function () {
      confirmBtn.disabled = false;
      errorEl.hidden = false;
    });
  }

  // Touch handlers drive the press-and-hold gesture.
  table.addEventListener('touchstart', function (ev) {
    var row = ev.target.closest('tr[data-domain]');
    if (!row) { return; }
    var t = ev.touches[0];
    startX = t.clientX;
    startY = t.clientY;
    table.dataset.longPressed = 'false';
    clearTimer();
    timer = window.setTimeout(function () {
      table.dataset.longPressed = 'true';
      openModal(row);
    }, LONG_PRESS_MS);
  }, { passive: true });

  table.addEventListener('touchmove', function (ev) {
    var t = ev.touches[0];
    if (Math.abs(t.clientX - startX) > MOVE_TOLERANCE ||
        Math.abs(t.clientY - startY) > MOVE_TOLERANCE) {
      clearTimer();
    }
  }, { passive: true });

  table.addEventListener('touchend', clearTimer);
  table.addEventListener('touchcancel', clearTimer);

  // Dismiss controls: Cancel button, backdrop tap, or the Escape key.
  modal.addEventListener('click', function (ev) {
    if (ev.target.closest('[data-modal-close]')) { closeModal(); }
  });
  confirmBtn.addEventListener('click', submit);
  document.addEventListener('keydown', function (ev) {
    if (ev.key === 'Escape' && !modal.hidden) { closeModal(); }
  });
})();
