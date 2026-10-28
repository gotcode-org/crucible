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
});
