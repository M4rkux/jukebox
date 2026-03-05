// Jukebox — app.js
// HTMX + small helpers

// Auto-remove toasts after 3s
document.body.addEventListener('htmx:afterSwap', function(evt) {
  const toasts = evt.detail.target.querySelectorAll?.('.toast') || [];
  toasts.forEach(t => {
    setTimeout(() => t.remove(), 3000);
  });

  // Global toast handler
  const globalToast = document.querySelector('.toast');
  if (globalToast) {
    setTimeout(() => globalToast.remove(), 3000);
  }
});

// Passcode input: auto-format + auto-submit on 6 digits
document.addEventListener('DOMContentLoaded', () => {
  const passcodeInput = document.querySelector('.passcode-input');
  if (passcodeInput) {
    passcodeInput.addEventListener('input', (e) => {
      // Strip non-digits
      e.target.value = e.target.value.replace(/\D/g, '').slice(0, 6);
      // Auto-submit when 6 digits entered
      if (e.target.value.length === 6) {
        e.target.closest('form')?.submit();
      }
    });
  }
});

// Copy share URL
function copyShareURL() {
  const urlEl = document.getElementById('share-url-text');
  if (!urlEl) return;
  navigator.clipboard.writeText(urlEl.textContent.trim()).then(() => {
    const btn = document.querySelector('.btn-copy');
    if (btn) {
      const orig = btn.textContent;
      btn.textContent = '✓ Copied';
      setTimeout(() => btn.textContent = orig, 2000);
    }
  });
}
