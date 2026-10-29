// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

/**
 * Crucible Web Shell Client Script
 */
document.addEventListener('DOMContentLoaded', () => {
  // Simple periodic health probe to ensure the web shell remains connected
  // to the crucibled server daemon.
  const checkHealth = async () => {
    try {
      const res = await fetch('/healthz');
      if (!res.ok) {
        console.warn('Crucible health check status:', res.status);
      }
    } catch (err) {
      console.error('Crucible health check failed:', err);
    }
  };

  // Run initial health check and poll every 30 seconds
  setInterval(checkHealth, 30000);

  // Operator Account Dropdown Menu Toggle
  const menuBtn = document.getElementById('account-menu-btn');
  const dropdown = document.getElementById('account-dropdown');
  const chevron = document.getElementById('account-menu-chevron');

  if (menuBtn && dropdown) {
    menuBtn.addEventListener('click', (e) => {
      e.stopPropagation();
      const isHidden = dropdown.classList.contains('hidden');
      if (isHidden) {
        dropdown.classList.remove('hidden');
        menuBtn.setAttribute('aria-expanded', 'true');
        if (chevron) chevron.style.transform = 'rotate(180deg)';
      } else {
        dropdown.classList.add('hidden');
        menuBtn.setAttribute('aria-expanded', 'false');
        if (chevron) chevron.style.transform = 'rotate(0deg)';
      }
    });

    document.addEventListener('click', (e) => {
      if (!dropdown.contains(e.target) && !menuBtn.contains(e.target)) {
        dropdown.classList.add('hidden');
        menuBtn.setAttribute('aria-expanded', 'false');
        if (chevron) chevron.style.transform = 'rotate(0deg)';
      }
    });

    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape' && !dropdown.classList.contains('hidden')) {
        dropdown.classList.add('hidden');
        menuBtn.setAttribute('aria-expanded', 'false');
        if (chevron) chevron.style.transform = 'rotate(0deg)';
      }
    });
  }

  // Sidebar Collapsible Navigation Groups State Persistence
  const navDetails = document.querySelectorAll('details[data-nav-group]');
  navDetails.forEach((el) => {
    const groupId = el.getAttribute('data-nav-group');
    if (!groupId) return;

    // Active route auto-expansion takes precedence over stored closed state
    const hasActiveChild = el.querySelector('[data-nav-active="true"]');
    if (hasActiveChild) {
      el.open = true;
    } else {
      const saved = localStorage.getItem('crucible_nav_' + groupId);
      if (saved !== null) {
        el.open = saved === 'true';
      }
    }

    el.addEventListener('toggle', () => {
      localStorage.setItem(
        'crucible_nav_' + groupId,
        el.open ? 'true' : 'false'
      );
    });
  });
});
