let currentDomain = '';
let currentFilePath = '';
let currentBrowsePath = '';
let cachedSites = [];
let cachedFiles = [];

document.addEventListener('DOMContentLoaded', () => {
  initTabs();
  loadServerStatus();
  loadSites();
  loadCertificates();
  loadLogs();
  loadSystemMetrics();
  loadNetworkInfo();
  setInterval(loadSystemMetrics, 4000);

  document.getElementById('logoutBtn').addEventListener('click', async () => {
    await fetch('/api/logout', { method: 'POST' });
    window.location.href = '/login';
  });

  document.getElementById('switchToCaddyBtn').addEventListener('click', () => switchEngine('caddy'));
  document.getElementById('switchToNginxBtn').addEventListener('click', () => switchEngine('nginx'));
  document.getElementById('deployForm').addEventListener('submit', handleDeploy);

  // Health probe events
  document.getElementById('refreshHealthBtn').addEventListener('click', loadSiteHealth);

  // Traffic events
  document.getElementById('trafficDomainSelect').addEventListener('change', (e) => loadTraffic(e.target.value));

  // Snapshot & Version events
  document.getElementById('uploadVersionForm').addEventListener('submit', handleUploadVersionArchive);
  document.getElementById('snapshotForm').addEventListener('submit', handleCreateSnapshot);
  document.getElementById('snapshotDomainSelect').addEventListener('change', (e) => loadSnapshots(e.target.value));
  document.getElementById('versionDomainSelect').addEventListener('change', (e) => loadSnapshots(e.target.value));

  // DNS IP copy badges
  document.getElementById('badgeIPv4').addEventListener('click', () => copyBadgeIP('badgeIPv4'));
  document.getElementById('badgeIPv6').addEventListener('click', () => copyBadgeIP('badgeIPv6'));
  document.getElementById('copyDnsIPv4Btn')?.addEventListener('click', () => {
    const el = document.getElementById('dnsDisplayIPv4');
    if (el && el.dataset.ip) copyTextValue(el.dataset.ip, 'copyDnsIPv4Btn');
  });
  document.getElementById('copyDnsIPv6Btn')?.addEventListener('click', () => {
    const el = document.getElementById('dnsDisplayIPv6');
    if (el && el.dataset.ip) copyTextValue(el.dataset.ip, 'copyDnsIPv6Btn');
  });

  // File Manager events
  document.getElementById('fileDomainSelect').addEventListener('change', (e) => {
    currentBrowsePath = '';
    loadFiles(e.target.value, '');
  });
  document.getElementById('fileSearchInput').addEventListener('input', handleFilterFiles);
  document.getElementById('newFileBtn').addEventListener('click', handleCreateFile);
  document.getElementById('newFolderBtn').addEventListener('click', handleCreateFolder);
  document.getElementById('uploadFileTriggerBtn').addEventListener('click', () => document.getElementById('fileDirectInput').click());
  document.getElementById('fileDirectInput').addEventListener('change', handleUploadSingleFile);

  // Image preview modal
  document.getElementById('closeImagePreviewBtn').addEventListener('click', () => {
    document.getElementById('imagePreviewModal').style.display = 'none';
  });

  // Editor events
  document.getElementById('editorSaveBtn').addEventListener('click', handleSaveEditorContent);
  document.getElementById('editorCloseBtn').addEventListener('click', closeEditor);

  // Enable Tab in editor textarea
  const textarea = document.getElementById('editorTextarea');
  textarea.addEventListener('keydown', (e) => {
    if (e.key === 'Tab') {
      e.preventDefault();
      const start = textarea.selectionStart;
      const end = textarea.selectionEnd;
      textarea.value = textarea.value.substring(0, start) + '  ' + textarea.value.substring(end);
      textarea.selectionStart = textarea.selectionEnd = start + 2;
    }
  });
});

function initTabs() {
  const tabs = document.querySelectorAll('.tab-btn');
  tabs.forEach(tab => {
    tab.addEventListener('click', () => {
      tabs.forEach(t => t.classList.remove('active'));
      document.querySelectorAll('.tab-pane').forEach(p => p.classList.remove('active'));

      tab.classList.add('active');
      const target = document.getElementById(tab.dataset.tab);
      if (target) target.classList.add('active');

      if (tab.dataset.tab === 'tab-health') {
        loadSiteHealth();
      } else if (tab.dataset.tab === 'tab-traffic') {
        const sel = document.getElementById('trafficDomainSelect');
        if (sel.value) loadTraffic(sel.value);
      } else if (tab.dataset.tab === 'tab-snapshots') {
        const sel = document.getElementById('snapshotDomainSelect');
        loadSnapshots(sel ? sel.value : '');
      } else if (tab.dataset.tab === 'tab-files') {
        const sel = document.getElementById('fileDomainSelect');
        if (sel.value) loadFiles(sel.value, currentBrowsePath);
      }
    });
  });
}

// System Resource Monitoring
async function loadSystemMetrics() {
  try {
    const res = await fetch('/api/metrics/system');
    if (!res.ok) return;
    const data = await res.json();

    document.getElementById('metricCPU').innerText = `${data.cpu_percent}%`;
    document.getElementById('fillCPU').style.width = `${Math.min(data.cpu_percent, 100)}%`;

    const ramUsed = formatBytes(data.mem_used_bytes);
    const ramTotal = formatBytes(data.mem_total_bytes);
    document.getElementById('metricRAM').innerText = `${ramUsed} / ${ramTotal}`;
    document.getElementById('fillRAM').style.width = `${Math.min(data.mem_percent, 100)}%`;

    const diskUsed = formatBytes(data.disk_used_bytes);
    const diskTotal = formatBytes(data.disk_total_bytes);
    document.getElementById('metricDisk').innerText = `${diskUsed} / ${diskTotal}`;
    document.getElementById('fillDisk').style.width = `${Math.min(data.disk_percent, 100)}%`;

    const hours = Math.floor(data.uptime_seconds / 3600);
    const mins = Math.floor((data.uptime_seconds % 3600) / 60);
    document.getElementById('metricUptime').innerText = `${hours}h ${mins}m`;
  } catch (err) {
    // Non-blocking background poll
  }
}

// Server Public IPv4 / IPv6 Resolution for DNS Records
async function loadNetworkInfo() {
  try {
    const res = await fetch('/api/system/network');
    if (!res.ok) return;
    const data = await res.json();

    const ipv4Badge = document.getElementById('badgeIPv4');
    const ipv6Badge = document.getElementById('badgeIPv6');
    const dnsValA = document.getElementById('dnsDisplayIPv4');
    const dnsValAAAA = document.getElementById('dnsDisplayIPv6');
    const dnsBoxAAAA = document.getElementById('dnsBoxIPv6');

    if (data.ipv4) {
      ipv4Badge.innerText = `IPv4: ${data.ipv4}`;
      ipv4Badge.dataset.ip = data.ipv4;
      if (dnsValA) {
        dnsValA.innerText = data.ipv4;
        dnsValA.dataset.ip = data.ipv4;
      }
    } else {
      if (dnsValA) dnsValA.innerText = 'Unavailable';
    }

    if (data.ipv6) {
      ipv6Badge.innerText = `IPv6: ${data.ipv6}`;
      ipv6Badge.dataset.ip = data.ipv6;
      ipv6Badge.style.display = 'inline-block';
      if (dnsValAAAA) {
        dnsValAAAA.innerText = data.ipv6;
        dnsValAAAA.dataset.ip = data.ipv6;
      }
      if (dnsBoxAAAA) dnsBoxAAAA.style.display = 'flex';
    }
  } catch (err) {
    console.error('Failed to resolve network IP', err);
  }
}

function copyBadgeIP(badgeId) {
  const badge = document.getElementById(badgeId);
  const ip = badge.dataset.ip;
  if (!ip) return;

  navigator.clipboard.writeText(ip).then(() => {
    const orig = badge.innerText;
    badge.innerText = `Copied!`;
    setTimeout(() => { badge.innerText = orig; }, 1500);
  });
}

function copyTextValue(text, btnId) {
  if (!text) return;
  navigator.clipboard.writeText(text).then(() => {
    const btn = document.getElementById(btnId);
    if (!btn) return;
    const orig = btn.innerText;
    btn.innerText = 'Copied!';
    setTimeout(() => { btn.innerText = orig; }, 1500);
  });
}

// Web Server Engine Status
async function loadServerStatus() {
  try {
    const res = await fetch('/api/server/status');
    if (!res.ok) return;
    const data = await res.json();
    document.getElementById('headerEngineBadge').innerText = `Engine: ${data.active_server.toUpperCase()}`;
    document.getElementById('switchStatusText').innerText = `State: ${data.state}`;

    document.getElementById('switchToCaddyBtn').disabled = (data.active_server === 'caddy');
    document.getElementById('switchToNginxBtn').disabled = (data.active_server === 'nginx');
  } catch (err) {
    console.error('Failed to load server status', err);
  }
}

async function switchEngine(target) {
  const alertEl = document.getElementById('switchAlert');
  alertEl.style.display = 'none';
  document.getElementById('switchToCaddyBtn').disabled = true;
  document.getElementById('switchToNginxBtn').disabled = true;

  try {
    const res = await fetch('/api/server/switch', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ target })
    });
    const data = await res.json();
    if (!res.ok) {
      alertEl.className = 'alert alert-danger';
      alertEl.innerText = data.error || 'Server switch failed';
      alertEl.style.display = 'block';
    } else {
      alertEl.className = 'alert alert-success';
      alertEl.innerText = `Switched successfully to ${target.toUpperCase()}`;
      alertEl.style.display = 'block';
    }
  } catch (err) {
    alertEl.className = 'alert alert-danger';
    alertEl.innerText = 'Network error during switch';
    alertEl.style.display = 'block';
  } finally {
    await loadServerStatus();
    await loadLogs();
  }
}

async function handleDeploy(e) {
  e.preventDefault();
  const alertEl = document.getElementById('deployAlert');
  const progressContainer = document.getElementById('deployProgress');
  const progressBar = document.getElementById('deployProgressBar');
  const deployBtn = document.getElementById('deployBtn');

  alertEl.style.display = 'none';
  progressContainer.style.display = 'block';
  progressBar.style.width = '30%';
  deployBtn.disabled = true;

  const domain = document.getElementById('domainInput').value.trim();
  const zipFile = document.getElementById('zipFileInput').files[0];
  const sslEnabled = document.getElementById('sslCheckbox').checked;

  const formData = new FormData();
  formData.append('domain', domain);
  formData.append('file', zipFile);
  formData.append('ssl_enabled', sslEnabled ? 'true' : 'false');

  try {
    progressBar.style.width = '70%';
    const res = await fetch('/api/sites/upload', {
      method: 'POST',
      body: formData
    });
    progressBar.style.width = '100%';
    const data = await res.json();

    if (!res.ok) {
      alertEl.className = 'alert alert-danger';
      alertEl.innerText = data.error || 'Deployment failed';
      alertEl.style.display = 'block';
    } else {
      alertEl.className = 'alert alert-success';
      alertEl.innerText = `Site ${domain} deployed successfully`;
      alertEl.style.display = 'block';
      document.getElementById('deployForm').reset();
      loadSites();
      loadCertificates();
    }
  } catch (err) {
    alertEl.className = 'alert alert-danger';
    alertEl.innerText = 'Network error during deployment';
    alertEl.style.display = 'block';
  } finally {
    deployBtn.disabled = false;
    setTimeout(() => { progressContainer.style.display = 'none'; progressBar.style.width = '0%'; }, 500);
  }
}

async function loadSites() {
  const tbody = document.getElementById('sitesTableBody');
  try {
    const res = await fetch('/api/sites');
    if (!res.ok) return;
    cachedSites = await res.json();

    updateDomainSelects(cachedSites);

    if (!cachedSites || cachedSites.length === 0) {
      tbody.innerHTML = '<tr><td colspan="7" class="text-muted">No sites configured</td></tr>';
      return;
    }

    tbody.innerHTML = cachedSites.map(s => {
      const isCustomVersion = s.active_version && s.active_version !== 'live/baseline';
      const versionBadge = isCustomVersion
        ? `<span class="badge badge-live" title="Active live version image">${escapeHtml(s.active_version)}</span>`
        : `<span class="badge" title="Default root files">${escapeHtml(s.active_version || 'live/baseline')}</span>`;

      return `
        <tr>
          <td><strong>${escapeHtml(s.domain)}</strong></td>
          <td>${versionBadge}</td>
          <td><code>${escapeHtml(s.root_path)}</code></td>
          <td>${s.ssl_enabled ? '<span class="badge badge-success">Enabled</span>' : '<span class="text-muted">Off</span>'}</td>
          <td><span class="badge badge-success">${escapeHtml(s.status)}</span></td>
          <td>${new Date(s.created_at).toLocaleDateString()}</td>
          <td>
            <div style="display:flex; gap:4px; align-items:center;">
              <button onclick="openVersionsForDomain('${escapeHtml(s.domain)}')" class="btn btn-secondary btn-xs" title="Manage site images & versions">Versions</button>
              <button onclick="openFilesForDomain('${escapeHtml(s.domain)}')" class="btn btn-secondary btn-xs" title="Open file manager">Files</button>
              <button onclick="deleteSite('${escapeHtml(s.domain)}')" class="btn btn-danger btn-xs" title="Delete site">Delete</button>
            </div>
          </td>
        </tr>
      `;
    }).join('');
  } catch (err) {
    tbody.innerHTML = '<tr><td colspan="7" class="text-muted">Failed to load sites</td></tr>';
  }
}

function openVersionsForDomain(domain) {
  const tabBtn = document.querySelector('.tab-btn[data-tab="tab-snapshots"]');
  if (tabBtn) tabBtn.click();
  const sel = document.getElementById('snapshotDomainSelect');
  if (sel) {
    sel.value = domain;
    loadSnapshots(domain);
  }
  const verSel = document.getElementById('versionDomainSelect');
  if (verSel) verSel.value = domain;
}

function openFilesForDomain(domain) {
  const tabBtn = document.querySelector('.tab-btn[data-tab="tab-files"]');
  if (tabBtn) tabBtn.click();
  const sel = document.getElementById('fileDomainSelect');
  if (sel) {
    sel.value = domain;
    currentBrowsePath = '';
    loadFiles(domain, '');
  }
}

function updateDomainSelects(sites) {
  const snapSel = document.getElementById('snapshotDomainSelect');
  const verSel = document.getElementById('versionDomainSelect');
  const fileSel = document.getElementById('fileDomainSelect');
  const trafSel = document.getElementById('trafficDomainSelect');

  const prevSnap = snapSel.value;
  const prevVer = verSel.value;
  const prevFile = fileSel.value;
  const prevTraf = trafSel.value;

  const options = sites.map(s => `<option value="${escapeHtml(s.domain)}">${escapeHtml(s.domain)}</option>`).join('');
  snapSel.innerHTML = `<option value="">All Domains</option>` + options;
  verSel.innerHTML = options;
  fileSel.innerHTML = options;
  trafSel.innerHTML = options;

  if (prevSnap && (prevSnap === '' || sites.some(s => s.domain === prevSnap))) snapSel.value = prevSnap;
  if (prevVer && sites.some(s => s.domain === prevVer)) verSel.value = prevVer;
  if (prevFile && sites.some(s => s.domain === prevFile)) fileSel.value = prevFile;
  if (prevTraf && sites.some(s => s.domain === prevTraf)) trafSel.value = prevTraf;
}

async function deleteSite(domain) {
  if (!confirm(`Delete site ${domain} and routes?`)) return;
  try {
    const res = await fetch(`/api/sites/${encodeURIComponent(domain)}`, { method: 'DELETE' });
    if (res.ok) {
      loadSites();
      loadCertificates();
    }
  } catch (err) {
    console.error('Failed to delete site', err);
  }
}

// Site Health Probes
async function loadSiteHealth() {
  const tbody = document.getElementById('healthTableBody');
  tbody.innerHTML = '<tr><td colspan="7" class="text-muted">Probing domain availability and certificates...</td></tr>';

  try {
    const res = await fetch('/api/sites/health');
    if (!res.ok) return;
    const results = await res.json();

    if (!results || results.length === 0) {
      tbody.innerHTML = '<tr><td colspan="7" class="text-muted">No sites to probe.</td></tr>';
      return;
    }

    tbody.innerHTML = results.map(h => {
      let badgeClass = 'badge-success';
      if (h.status === 'WARNING') badgeClass = 'badge-warning';
      if (h.status === 'DOWN') badgeClass = 'badge-failed';

      const codeStr = h.status_code ? `${h.status_code}` : '-';
      const ips = h.ip_addresses && h.ip_addresses.length > 0 ? h.ip_addresses.join(', ') : 'None';
      const sslStr = h.ssl_expiry ? `${h.ssl_expiry} (${h.ssl_days_left}d)` : 'None';

      return `
        <tr>
          <td><strong>${escapeHtml(h.domain)}</strong></td>
          <td><span class="badge ${badgeClass}">${escapeHtml(h.status)}</span></td>
          <td>${codeStr}</td>
          <td>${h.latency_ms} ms</td>
          <td>${escapeHtml(sslStr)}</td>
          <td><code>${escapeHtml(ips)}</code></td>
          <td><button onclick="probeSingleSite('${escapeHtml(h.domain)}')" class="btn btn-secondary btn-sm">Probe</button></td>
        </tr>
      `;
    }).join('');
  } catch (err) {
    tbody.innerHTML = '<tr><td colspan="7" class="text-muted">Failed to probe site health</td></tr>';
  }
}

async function probeSingleSite(domain) {
  try {
    const res = await fetch(`/api/sites/health?domain=${encodeURIComponent(domain)}`);
    if (res.ok) loadSiteHealth();
  } catch (err) {
    console.error(err);
  }
}

// Traffic & Peak Hours Analytics
async function loadTraffic(domain) {
  if (!domain) return;
  try {
    const res = await fetch(`/api/sites/traffic?domain=${encodeURIComponent(domain)}`);
    if (!res.ok) return;
    const data = await res.json();

    document.getElementById('statTotalHits').innerText = data.total_hits.toLocaleString();
    document.getElementById('statUniqueVisitors').innerText = data.unique_visitors.toLocaleString();
    document.getElementById('statHitsToday').innerText = data.hits_today.toLocaleString();
    document.getElementById('statPeakHour').innerText = `${String(data.peak_hour).padStart(2, '0')}:00 UTC`;

    renderHourlyChart(data.hourly_hits || []);
    renderTopPaths(data.top_paths || []);
  } catch (err) {
    console.error('Failed to load traffic', err);
  }
}

function renderHourlyChart(hourlyHits) {
  const container = document.getElementById('hourlyChartContainer');
  let max = 1;
  for (let i = 0; i < 24; i++) {
    if (hourlyHits[i] > max) max = hourlyHits[i];
  }

  let html = '';
  for (let i = 0; i < 24; i++) {
    const count = hourlyHits[i] || 0;
    const heightPct = Math.max((count / max) * 100, 3);
    html += `
      <div class="hour-col" title="${String(i).padStart(2, '0')}:00 - ${count} hits">
        <div class="hour-bar" style="height: ${heightPct}%"></div>
        <span class="hour-label">${i}</span>
      </div>
    `;
  }
  container.innerHTML = html;
}

function renderTopPaths(topPaths) {
  const tbody = document.getElementById('trafficPathsTableBody');
  if (!topPaths || topPaths.length === 0) {
    tbody.innerHTML = '<tr><td colspan="2" class="text-muted">No traffic data recorded</td></tr>';
    return;
  }

  tbody.innerHTML = topPaths.map(p => `
    <tr>
      <td><code>${escapeHtml(p.path)}</code></td>
      <td><strong>${p.count.toLocaleString()}</strong></td>
    </tr>
  `).join('');
}

// Site Images / Snapshots
async function loadSnapshots(domain) {
  const tbody = document.getElementById('snapshotsTableBody');
  tbody.innerHTML = '<tr><td colspan="6" class="text-muted">Loading snapshots...</td></tr>';

  const url = domain ? `/api/snapshots?domain=${encodeURIComponent(domain)}` : '/api/snapshots';
  try {
    const res = await fetch(url);
    if (!res.ok) return;
    const snaps = await res.json();

    if (!snaps || snaps.length === 0) {
      tbody.innerHTML = '<tr><td colspan="6" class="text-muted">No images saved' + (domain ? ` for ${escapeHtml(domain)}` : '') + ' yet</td></tr>';
      return;
    }

    tbody.innerHTML = snaps.map(s => {
      const statusBadge = s.is_active
        ? '<span class="badge badge-success">ACTIVE LIVE</span>'
        : '<span class="badge">STANDBY</span>';

      const switchBtn = s.is_active
        ? '<span class="text-muted" style="font-size:11px; margin-right:6px;">Current Live</span>'
        : `<button onclick="publishSnapshot('${escapeHtml(s.domain)}', ${s.id})" class="btn btn-primary btn-xs">Switch to This Version</button>`;

      return `
        <tr>
          <td><strong>${escapeHtml(s.name)}</strong></td>
          <td>${escapeHtml(s.domain)}</td>
          <td>${statusBadge}</td>
          <td><code>${escapeHtml(s.path)}</code></td>
          <td>${new Date(s.created_at).toLocaleString()}</td>
          <td>
            <div style="display:flex; gap:6px; align-items:center;">
              ${switchBtn}
              <button onclick="deleteSnapshot(${s.id}, '${escapeHtml(s.domain)}')" class="btn btn-danger btn-xs">Delete</button>
            </div>
          </td>
        </tr>
      `;
    }).join('');
  } catch (err) {
    tbody.innerHTML = '<tr><td colspan="6" class="text-muted">Failed to load snapshots</td></tr>';
  }
}

async function handleUploadVersionArchive(e) {
  e.preventDefault();
  const alertEl = document.getElementById('versionAlert');
  const spinner = document.getElementById('versionBtnSpinner');
  const btn = document.getElementById('uploadVersionBtn');

  alertEl.style.display = 'none';
  spinner.style.display = 'inline-block';
  btn.disabled = true;

  const domain = document.getElementById('versionDomainSelect').value;
  const versionName = document.getElementById('versionNameInput').value.trim();
  const zipFile = document.getElementById('versionZipFileInput').files[0];
  const makeLive = document.getElementById('versionMakeLiveCheckbox').checked;

  if (!domain || !versionName || !zipFile) {
    alertEl.className = 'alert alert-danger';
    alertEl.innerText = 'Domain, version tag, and zip archive are required';
    alertEl.style.display = 'block';
    btn.disabled = false;
    spinner.style.display = 'none';
    return;
  }

  const formData = new FormData();
  formData.append('domain', domain);
  formData.append('version_name', versionName);
  formData.append('file', zipFile);
  formData.append('make_live', makeLive ? 'true' : 'false');

  try {
    const res = await fetch('/api/snapshots/upload', {
      method: 'POST',
      body: formData
    });
    const data = await res.json();
    if (!res.ok) {
      alertEl.className = 'alert alert-danger';
      alertEl.innerText = data.error || 'Failed to upload version image';
      alertEl.style.display = 'block';
    } else {
      alertEl.className = 'alert alert-success';
      alertEl.innerText = `Version image ${versionName} saved${makeLive ? ' and activated as live site' : ''}`;
      alertEl.style.display = 'block';
      document.getElementById('uploadVersionForm').reset();
      loadSnapshots(domain);
      loadSites();
    }
  } catch (err) {
    alertEl.className = 'alert alert-danger';
    alertEl.innerText = 'Network error during version upload';
    alertEl.style.display = 'block';
  } finally {
    btn.disabled = false;
    spinner.style.display = 'none';
  }
}

async function handleCreateSnapshot(e) {
  e.preventDefault();
  const alertEl = document.getElementById('snapshotAlert');
  const spinner = document.getElementById('snapshotBtnSpinner');
  const btn = document.getElementById('saveSnapshotBtn');

  alertEl.style.display = 'none';
  spinner.style.display = 'inline-block';
  btn.disabled = true;

  const domain = document.getElementById('snapshotDomainSelect').value;
  const name = document.getElementById('snapshotNameInput').value.trim();

  if (!domain || !name) {
    btn.disabled = false;
    spinner.style.display = 'none';
    return;
  }

  try {
    const res = await fetch('/api/snapshots/create', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ domain, name })
    });
    const data = await res.json();
    if (!res.ok) {
      alertEl.className = 'alert alert-danger';
      alertEl.innerText = data.error || 'Failed to capture snapshot';
      alertEl.style.display = 'block';
    } else {
      alertEl.className = 'alert alert-success';
      alertEl.innerText = `Image snapshot ${name} created successfully`;
      alertEl.style.display = 'block';
      document.getElementById('snapshotNameInput').value = '';
      loadSnapshots(domain);
    }
  } catch (err) {
    alertEl.className = 'alert alert-danger';
    alertEl.innerText = 'Network error while capturing snapshot';
    alertEl.style.display = 'block';
  } finally {
    btn.disabled = false;
    spinner.style.display = 'none';
  }
}

async function publishSnapshot(domain, id) {
  if (!confirm(`Switch live site to snapshot image ID ${id}? Live files will be updated immediately.`)) return;
  try {
    const res = await fetch('/api/snapshots/publish', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ domain, snapshot_id: id })
    });
    const data = await res.json();
    if (!res.ok) {
      alert(data.error || 'Failed to publish snapshot');
    } else {
      loadSnapshots(domain);
      loadSites();
    }
  } catch (err) {
    alert('Network error publishing snapshot');
  }
}

async function deleteSnapshot(id, domain) {
  if (!confirm('Permanently delete this snapshot image?')) return;
  try {
    const res = await fetch(`/api/snapshots/${id}`, { method: 'DELETE' });
    if (res.ok) {
      loadSnapshots(domain);
    }
  } catch (err) {
    console.error('Failed to delete snapshot', err);
  }
}

// File Manager & Code Editor
async function loadFiles(domain, subPath) {
  if (!domain) return;
  currentDomain = domain;
  currentBrowsePath = subPath || '';

  renderBreadcrumb(currentBrowsePath);

  const tbody = document.getElementById('filesTableBody');
  tbody.innerHTML = '<tr><td colspan="4" class="text-muted">Loading files...</td></tr>';

  try {
    const res = await fetch(`/api/files?domain=${encodeURIComponent(domain)}&path=${encodeURIComponent(currentBrowsePath)}`);
    if (!res.ok) return;
    cachedFiles = await res.json();
    renderFileList(cachedFiles);
  } catch (err) {
    tbody.innerHTML = '<tr><td colspan="4" class="text-muted">Failed to load directory</td></tr>';
  }
}

function handleFilterFiles(e) {
  const query = e.target.value.toLowerCase().trim();
  if (!query) {
    renderFileList(cachedFiles);
    return;
  }
  const filtered = cachedFiles.filter(f => f.name.toLowerCase().includes(query));
  renderFileList(filtered);
}

function renderFileList(files) {
  const tbody = document.getElementById('filesTableBody');
  if (!files || files.length === 0) {
    tbody.innerHTML = '<tr><td colspan="4" class="text-muted">Directory is empty or no files match search</td></tr>';
    return;
  }

  tbody.innerHTML = files.map(f => {
    const sizeStr = f.is_dir ? '-' : formatBytes(f.size);
    const rowClass = f.is_dir ? 'file-row-dir' : '';
    const clickAction = f.is_dir
      ? `onclick="loadFiles('${escapeHtml(currentDomain)}', '${escapeHtml(f.path)}')"`
      : `onclick="openEditor('${escapeHtml(currentDomain)}', '${escapeHtml(f.path)}')"`

    const isImage = /\.(png|jpe?g|gif|svg|webp|ico)$/i.test(f.name);

    return `
      <tr>
        <td class="${rowClass}" ${clickAction}>
          ${f.is_dir ? '[DIR]' : '[FILE]'} ${escapeHtml(f.name)}
        </td>
        <td>${sizeStr}</td>
        <td>${new Date(f.mod_time).toLocaleString()}</td>
        <td>
          ${!f.is_dir ? `<button onclick="openEditor('${escapeHtml(currentDomain)}', '${escapeHtml(f.path)}')" class="btn btn-secondary btn-sm">Edit</button>` : ''}
          ${isImage ? `<button onclick="previewImage('${escapeHtml(currentDomain)}', '${escapeHtml(f.path)}')" class="btn btn-secondary btn-sm">Preview</button>` : ''}
          ${!f.is_dir ? `<a href="/api/files/download?domain=${encodeURIComponent(currentDomain)}&path=${encodeURIComponent(f.path)}" class="btn btn-secondary btn-sm" download>Download</a>` : ''}
          <button onclick="handleRenameEntry('${escapeHtml(currentDomain)}', '${escapeHtml(f.path)}')" class="btn btn-secondary btn-sm">Rename</button>
          <button onclick="deleteFileEntry('${escapeHtml(currentDomain)}', '${escapeHtml(f.path)}')" class="btn btn-danger btn-sm">Delete</button>
        </td>
      </tr>
    `;
  }).join('');
}

function renderBreadcrumb(subPath) {
  const container = document.getElementById('fileBreadcrumb');
  const parts = subPath ? subPath.split('/').filter(Boolean) : [];

  let html = `<span class="breadcrumb-item" onclick="loadFiles('${escapeHtml(currentDomain)}', '')">root</span>`;
  let accumulated = '';

  parts.forEach((p, idx) => {
    accumulated += (idx === 0 ? '' : '/') + p;
    html += ` / <span class="breadcrumb-item" onclick="loadFiles('${escapeHtml(currentDomain)}', '${escapeHtml(accumulated)}')">${escapeHtml(p)}</span>`;
  });

  container.innerHTML = html;
}

function previewImage(domain, filePath) {
  const modal = document.getElementById('imagePreviewModal');
  const img = document.getElementById('imagePreviewElement');
  const title = document.getElementById('imagePreviewTitle');

  title.innerText = `Preview: ${filePath}`;
  img.src = `/api/files/download?domain=${encodeURIComponent(domain)}&path=${encodeURIComponent(filePath)}`;
  modal.style.display = 'flex';
}

async function openEditor(domain, filePath) {
  currentDomain = domain;
  currentFilePath = filePath;

  const card = document.getElementById('editorCard');
  const statusEl = document.getElementById('editorSaveStatus');
  const currentFileEl = document.getElementById('editorCurrentFile');
  const textarea = document.getElementById('editorTextarea');

  currentFileEl.innerText = `${domain} : ${filePath}`;
  statusEl.innerText = 'Loading...';
  card.style.display = 'block';
  textarea.value = '';

  card.scrollIntoView({ behavior: 'smooth' });

  try {
    const res = await fetch(`/api/files/content?domain=${encodeURIComponent(domain)}&path=${encodeURIComponent(filePath)}`);
    const data = await res.json();
    if (!res.ok) {
      statusEl.innerText = data.error || 'Failed to open file';
      return;
    }
    textarea.value = data.content;
    statusEl.innerText = 'Ready';
  } catch (err) {
    statusEl.innerText = 'Network error';
  }
}

function closeEditor() {
  document.getElementById('editorCard').style.display = 'none';
  currentFilePath = '';
}

async function handleSaveEditorContent() {
  if (!currentDomain || !currentFilePath) return;

  const statusEl = document.getElementById('editorSaveStatus');
  const textarea = document.getElementById('editorTextarea');
  statusEl.innerText = 'Saving...';

  try {
    const res = await fetch('/api/files/content', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        domain: currentDomain,
        path: currentFilePath,
        content: textarea.value
      })
    });
    const data = await res.json();
    if (!res.ok) {
      statusEl.innerText = data.error || 'Save failed';
    } else {
      statusEl.innerText = `Saved at ${new Date().toLocaleTimeString()}`;
      loadFiles(currentDomain, currentBrowsePath);
    }
  } catch (err) {
    statusEl.innerText = 'Network error';
  }
}

async function handleCreateFile() {
  const domain = document.getElementById('fileDomainSelect').value;
  if (!domain) return;
  const name = prompt('Enter new file name:');
  if (!name) return;

  const fullPath = currentBrowsePath ? `${currentBrowsePath}/${name}` : name;
  try {
    const res = await fetch('/api/files/create', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ domain, path: fullPath, is_dir: false })
    });
    if (res.ok) {
      loadFiles(domain, currentBrowsePath);
      openEditor(domain, fullPath);
    } else {
      const data = await res.json();
      alert(data.error || 'Failed to create file');
    }
  } catch (err) {
    alert('Network error');
  }
}

async function handleCreateFolder() {
  const domain = document.getElementById('fileDomainSelect').value;
  if (!domain) return;
  const name = prompt('Enter new folder name:');
  if (!name) return;

  const fullPath = currentBrowsePath ? `${currentBrowsePath}/${name}` : name;
  try {
    const res = await fetch('/api/files/create', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ domain, path: fullPath, is_dir: true })
    });
    if (res.ok) {
      loadFiles(domain, currentBrowsePath);
    } else {
      const data = await res.json();
      alert(data.error || 'Failed to create folder');
    }
  } catch (err) {
    alert('Network error');
  }
}

async function handleRenameEntry(domain, oldPath) {
  const newName = prompt(`Enter new path or name for ${oldPath}:`, oldPath);
  if (!newName || newName === oldPath) return;

  try {
    const res = await fetch('/api/files/rename', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ domain, old_path: oldPath, new_path: newName })
    });
    if (res.ok) {
      loadFiles(domain, currentBrowsePath);
      if (currentFilePath === oldPath) {
        currentFilePath = newName;
        document.getElementById('editorCurrentFile').innerText = `${domain} : ${newName}`;
      }
    } else {
      const data = await res.json();
      alert(data.error || 'Rename failed');
    }
  } catch (err) {
    alert('Network error renaming entry');
  }
}

async function handleDeleteFileEntry(domain, filePath) {
  if (!confirm(`Delete ${filePath}?`)) return;
  try {
    const res = await fetch(`/api/files?domain=${encodeURIComponent(domain)}&path=${encodeURIComponent(filePath)}`, {
      method: 'DELETE'
    });
    if (res.ok) {
      loadFiles(domain, currentBrowsePath);
      if (currentFilePath === filePath) closeEditor();
    } else {
      const data = await res.json();
      alert(data.error || 'Delete failed');
    }
  } catch (err) {
    alert('Network error');
  }
}

async function handleUploadSingleFile(e) {
  const file = e.target.files[0];
  if (!file) return;
  const domain = document.getElementById('fileDomainSelect').value;
  if (!domain) return;

  const formData = new FormData();
  formData.append('domain', domain);
  formData.append('path', currentBrowsePath);
  formData.append('file', file);

  try {
    const res = await fetch('/api/files/upload', {
      method: 'POST',
      body: formData
    });
    if (res.ok) {
      loadFiles(domain, currentBrowsePath);
    } else {
      const data = await res.json();
      alert(data.error || 'Upload failed');
    }
  } catch (err) {
    alert('Network error uploading file');
  } finally {
    e.target.value = '';
  }
}

async function loadCertificates() {
  const tbody = document.getElementById('certsTableBody');
  try {
    const res = await fetch('/api/certificates');
    if (!res.ok) return;
    const certs = await res.json();
    if (!certs || certs.length === 0) {
      tbody.innerHTML = '<tr><td colspan="5" class="text-muted">No certificates registered</td></tr>';
      return;
    }

    tbody.innerHTML = certs.map(c => `
      <tr>
        <td><strong>${escapeHtml(c.domain)}</strong></td>
        <td><code>${escapeHtml(c.cert_path)}</code></td>
        <td>${escapeHtml(c.issuer)}</td>
        <td>${new Date(c.expires_at).toLocaleDateString()}</td>
        <td><span class="badge badge-success">${escapeHtml(c.status)}</span></td>
      </tr>
    `).join('');
  } catch (err) {
    tbody.innerHTML = '<tr><td colspan="5" class="text-muted">Failed to load certificates</td></tr>';
  }
}

async function loadLogs() {
  const tbody = document.getElementById('logsTableBody');
  try {
    const res = await fetch('/api/logs');
    if (!res.ok) return;
    const logs = await res.json();
    if (!logs || logs.length === 0) {
      tbody.innerHTML = '<tr><td colspan="5" class="text-muted">No operations logged</td></tr>';
      return;
    }

    tbody.innerHTML = logs.map(l => {
      let badgeClass = 'badge-success';
      if (l.status === 'FAILED') badgeClass = 'badge-failed';

      return `
        <tr>
          <td>${new Date(l.created_at).toLocaleTimeString()}</td>
          <td>${escapeHtml(l.from_server)}</td>
          <td>${escapeHtml(l.to_server)}</td>
          <td><span class="badge ${badgeClass}">${escapeHtml(l.status)}</span></td>
          <td>${escapeHtml(l.error_message || 'None')}</td>
        </tr>
      `;
    }).join('');
  } catch (err) {
    tbody.innerHTML = '<tr><td colspan="5" class="text-muted">Failed to load logs</td></tr>';
  }
}

function formatBytes(bytes) {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}

function escapeHtml(str) {
  if (!str) return '';
  return str.replace(/[&<>"']/g, m => ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;'
  })[m]);
}
