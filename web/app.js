let allTasks = [];
let filterText = "";
let selectedTaskId = null;
let currentUser = null;
let currentAdminTab = "orgs";
let activeAdminUserId = null;

// Settings and Config Management
let serverUrl = localStorage.getItem('kiwi_server_url') || 'http://localhost:8080';
let authToken = localStorage.getItem('kiwi_auth_token') || '';

// Initialize app components
document.addEventListener('DOMContentLoaded', () => {
    document.getElementById('setting-server-url').value = serverUrl;
    document.getElementById('setting-auth-token').value = authToken;
    document.getElementById('login-server-url').value = serverUrl;
    document.getElementById('login-api-key').value = authToken;

    // Check auth status
    validateAuth();
});

// Close dropdown on click outside
window.addEventListener('click', (e) => {
    const dropdown = document.getElementById('user-dropdown');
    const avatar = document.getElementById('user-avatar-initials');
    if (dropdown && dropdown.classList.contains('active') && e.target !== avatar) {
        dropdown.classList.remove('active');
    }
});

// Validate auth token
async function validateAuth() {
    if (!authToken) {
        showLoginOverlay(true);
        return;
    }

    try {
        const response = await fetch(`${serverUrl}/auth/validate`, {
            headers: { 'Authorization': `Bearer ${authToken}` }
        });

        if (response.status === 401) {
            showLoginOverlay(true);
            document.getElementById('login-error-msg').style.display = 'block';
            document.getElementById('login-error-msg').textContent = 'Invalid API key or session expired.';
            return;
        }

        if (!response.ok) throw new Error('Auth server error');

        const data = await response.json();
        currentUser = data;

        // Render UI based on role
        showLoginOverlay(false);
        setupUserUI();
        
        // Initial fetch and start interval
        fetchTasks();
        fetchUsage();
        setInterval(fetchTasks, 2000);
        setInterval(fetchUsage, 10000); // usage every 10s
    } catch (err) {
        console.error("Auth validation failed:", err);
        showConnectionError();
    }
}

// Manage UI elements for logged-in user
function setupUserUI() {
    if (!currentUser) return;

    // Set user badge
    document.getElementById('user-badge-container').style.display = 'flex';
    const name = currentUser.name || currentUser.email;
    const initials = name.split('@')[0].substring(0, 2).toUpperCase();
    document.getElementById('user-avatar-initials').textContent = initials;
    document.getElementById('user-display-name').textContent = currentUser.name || 'Member';
    document.getElementById('user-display-email').textContent = currentUser.email;
    document.getElementById('user-display-org').textContent = `Org: ${currentUser.org_id}`;

    // Show admin console toggle if user has admin privileges
    if (currentUser.role === 'admin') {
        document.getElementById('admin-toggle-btn').style.display = 'flex';
    } else {
        document.getElementById('admin-toggle-btn').style.display = 'none';
    }

    // Show tenant details in usage bar
    document.getElementById('tenant-usage-bar').style.display = 'flex';
    document.getElementById('usage-org-name').textContent = currentUser.org_id;
}

function showLoginOverlay(show) {
    const overlay = document.getElementById('login-overlay');
    if (show) {
        overlay.classList.add('active');
    } else {
        overlay.classList.remove('active');
        document.getElementById('login-error-msg').style.display = 'none';
    }
}

function toggleUserDropdown() {
    const dropdown = document.getElementById('user-dropdown');
    dropdown.classList.toggle('active');
}

function logout() {
    authToken = '';
    currentUser = null;
    localStorage.removeItem('kiwi_auth_token');
    showLoginOverlay(true);
    document.getElementById('user-badge-container').style.display = 'none';
    document.getElementById('admin-toggle-btn').style.display = 'none';
    document.getElementById('tenant-usage-bar').style.display = 'none';
}

async function login() {
    const urlInput = document.getElementById('login-server-url').value.trim();
    const keyInput = document.getElementById('login-api-key').value.trim();
    const errorEl = document.getElementById('login-error-msg');

    if (!urlInput || !keyInput) {
        errorEl.style.display = 'block';
        errorEl.textContent = 'Please provide both server URL and API access key.';
        return;
    }

    serverUrl = urlInput.replace(/\/$/, "");
    authToken = keyInput;
    errorEl.style.display = 'none';

    try {
        const response = await fetch(`${serverUrl}/auth/validate`, {
            headers: { 'Authorization': `Bearer ${authToken}` }
        });

        if (response.status === 401) {
            errorEl.style.display = 'block';
            errorEl.textContent = 'Invalid API key credentials.';
            return;
        }

        if (!response.ok) throw new Error('Server connection error');

        const data = await response.json();
        currentUser = data;

        // Persist config
        localStorage.setItem('kiwi_server_url', serverUrl);
        localStorage.setItem('kiwi_auth_token', authToken);

        // Update settings form fields
        document.getElementById('setting-server-url').value = serverUrl;
        document.getElementById('setting-auth-token').value = authToken;

        showLoginOverlay(false);
        setupUserUI();
        
        fetchTasks();
        fetchUsage();
    } catch (err) {
        errorEl.style.display = 'block';
        errorEl.textContent = 'Could not reach server. Verify the URL is correct.';
    }
}

function toggleSettingsPanel() {
    const overlay = document.getElementById('settings-overlay');
    overlay.classList.toggle('active');
}

function saveSettings() {
    const urlInput = document.getElementById('setting-server-url').value.trim();
    const tokenInput = document.getElementById('setting-auth-token').value.trim();
    
    if (urlInput) {
        serverUrl = urlInput.replace(/\/$/, "");
        localStorage.setItem('kiwi_server_url', serverUrl);
    }
    if (tokenInput) {
        authToken = tokenInput;
        localStorage.setItem('kiwi_auth_token', authToken);
    }
    
    toggleSettingsPanel();
    validateAuth(); // Validate connection immediately
}

// Fetch usage and cost statistics
async function fetchUsage() {
    if (!authToken) return;
    try {
        const response = await fetch(`${serverUrl}/usage`, {
            headers: { 'Authorization': `Bearer ${authToken}` }
        });
        if (!response.ok) return;
        const usage = await response.json();

        // Calculate success rate
        const total = usage.task_count || 0;
        const success = usage.success_count || 0;
        const rate = total > 0 ? Math.round((success / total) * 100) : 0;

        document.getElementById('usage-task-stats').textContent = `${total} tasks`;
        document.getElementById('usage-success-rate').textContent = `${rate}%`;
        document.getElementById('usage-month-spent').textContent = `$${(usage.total_cost || 0).toFixed(2)}`;

        // Fetch Limits from the server to compute progress bar
        const limitsResponse = await fetch(`${serverUrl}/tasks`, { // default GET /tasks is scoped to org
            headers: { 'Authorization': `Bearer ${authToken}` }
        });
        // We can check limits from tasks, but a better approach is reading limits dynamically or setting a default limit cap.
        // Let's hardcode limit reference of $50/month for rendering progress bar if limits are not easily queryable.
        // Or we can use the default limits cap ($100.0) from limits.go.
        const maxLimit = 100.0;
        document.getElementById('usage-budget-limits').textContent = `Limit: $${maxLimit.toFixed(2)}`;
        const percent = Math.min((usage.total_cost || 0) / maxLimit * 100, 100);
        document.getElementById('usage-progress-bar').style.width = `${percent}%`;
        
        if (percent > 85) {
            document.getElementById('usage-progress-bar').style.background = 'var(--accent-red)';
        } else if (percent > 60) {
            document.getElementById('usage-progress-bar').style.background = 'var(--accent-orange)';
        } else {
            document.getElementById('usage-progress-bar').style.background = 'var(--accent-teal)';
        }
    } catch (err) {
        console.error("Failed to load usage stats:", err);
    }
}

// Fetch tasks from server
async function fetchTasks() {
    if (!authToken) return;
    try {
        const response = await fetch(`${serverUrl}/tasks`, {
            headers: { 'Authorization': `Bearer ${authToken}` }
        });
        
        if (response.status === 401) {
            showAuthError();
            return;
        }
        
        if (!response.ok) throw new Error('Network response was not ok');
        const data = await response.json();
        
        allTasks = data || [];
        renderBoard();
        
        if (selectedTaskId) {
            const activeTask = allTasks.find(t => t.id === selectedTaskId);
            if (activeTask) {
                updateLogsTerminal(activeTask);
            }
        }
    } catch (err) {
        console.error("Failed to fetch tasks:", err);
        showConnectionError();
    }
}

function showAuthError() {
    const cols = ['backlog', 'running', 'paused', 'success', 'failed'];
    cols.forEach(col => {
        const el = document.getElementById('cards-' + col);
        if (el) el.innerHTML = '<div class="empty-column-text" style="color:var(--accent-red)">401 Unauthorized<br><span style="font-size:0.65rem">Key invalid or revoked.</span></div>';
    });
}

function showConnectionError() {
    const cols = ['backlog', 'running', 'paused', 'success', 'failed'];
    cols.forEach(col => {
        const el = document.getElementById('cards-' + col);
        if (el) el.innerHTML = '<div class="empty-column-text" style="color:var(--accent-orange)">Kiwi Server Offline<br><span style="font-size:0.65rem">Check connection to daemon at localhost.</span></div>';
    });
}

// Render entire board columns & update stats
function renderBoard() {
    const columns = {
        'backlog': [],
        'running': [],
        'paused': [],
        'success': [],
        'failed': []
    };

    let runningCount = 0;
    let successCount = 0;
    let failedCount = 0;
    let totalCost = 0;

    allTasks.forEach(task => {
        const status = (task.status || '').toLowerCase();
        
        if (status === 'running') runningCount++;
        else if (status === 'success') successCount++;
        else if (status === 'failed') failedCount++;
        
        totalCost += task.cost || 0;

        if (status === 'running') columns.running.push(task);
        else if (status === 'success') columns.success.push(task);
        else if (status === 'failed') columns.failed.push(task);
        else if (status === 'paused') columns.paused.push(task);
        else columns.backlog.push(task);
    });

    document.getElementById('stat-running').textContent = runningCount;
    document.getElementById('stat-success').textContent = successCount;
    document.getElementById('stat-failed').textContent = failedCount;
    document.getElementById('stat-cost').textContent = '$' + totalCost.toFixed(2);

    for (const [colName, taskList] of Object.entries(columns)) {
        const colContainer = document.getElementById('cards-' + colName);
        const badgeElement = document.getElementById('badge-' + colName);
        
        colContainer.innerHTML = '';
        
        const filteredList = taskList.filter(task => {
            if (!filterText) return true;
            const text = filterText.toLowerCase();
            return task.id.toLowerCase().includes(text) || task.file_path.toLowerCase().includes(text) || (task.task || '').toLowerCase().includes(text);
        });

        badgeElement.textContent = filteredList.length;

        if (filteredList.length === 0) {
            colContainer.innerHTML = '<div class="empty-column-text">No tasks</div>';
            continue;
        }

        filteredList.forEach(task => {
            const elapsed = getElapsedTime(task.created_at);
            const costVal = (task.cost || 0).toFixed(2);
            const statusLower = (task.status || 'backlog').toLowerCase();
            
            const card = document.createElement('div');
            card.className = "task-card " + statusLower;
            card.onclick = () => showLogs(task.id);
            
            card.innerHTML = 
                '<div class="card-header">' +
                    '<span class="card-id">#' + task.id + '</span>' +
                    '<span class="card-status-badge status-' + statusLower + '">' + task.status + '</span>' +
                '</div>' +
                '<div class="card-body">' +
                    '<div style="font-weight:600;margin-bottom:0.25rem">' + escapeHtml(task.task || 'Execution Task') + '</div>' +
                    '<div class="card-file-path">' +
                        '<svg style="width:12px;height:12px;fill:currentColor" viewBox="0 0 24 24"><path d="M13.17 2H6c-1.1 0-2 .9-2 2v16c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V8.83c0-.53-.21-1.04-.59-1.41l-4.83-4.83c-.37-.38-.88-.59-1.41-.59zM13 9V3.5L18.5 9H13z"/></svg>' +
                        escapeHtml(task.file_path) +
                    '</div>' +
                '</div>' +
                '<div class="card-footer">' +
                    '<span>' + elapsed + '</span>' +
                    '<span class="card-cost">$' + costVal + '</span>' +
                '</div>';

            colContainer.appendChild(card);
        });
    }
}

// Format elapsed/created time
function getElapsedTime(createdAtStr) {
    if (!createdAtStr) return 'N/A';
    const created = new Date(createdAtStr);
    const diffMs = new Date() - created;
    const diffSec = Math.floor(diffMs / 1000);
    
    if (diffSec < 60) return diffSec + 's elapsed';
    const diffMin = Math.floor(diffSec / 60);
    const remainingSec = diffSec % 60;
    return diffMin + 'm ' + remainingSec + 's elapsed';
}

function escapeHtml(str) {
    if (!str) return '';
    return str
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;")
        .replace(/'/g, "&#039;");
}

function applyFilter() {
    filterText = document.getElementById('search-input').value;
    renderBoard();
}

function showLogs(taskId) {
    selectedTaskId = taskId;
    const task = allTasks.find(t => t.id === taskId);
    if (!task) return;

    document.getElementById('modal-task-id').textContent = task.id;
    document.getElementById('modal-task-path').textContent = task.file_path;
    document.getElementById('modal-task-title').textContent = task.task || 'Task Logs';
    
    const overlay = document.getElementById('logs-modal');
    overlay.classList.add('active');
    
    updateLogsTerminal(task);
}

function updateLogsTerminal(task) {
    const statusElement = document.getElementById('terminal-status');
    statusElement.textContent = "STATUS: " + task.status;
    statusElement.style.color = '';
    if (task.status === 'RUNNING') statusElement.style.color = 'var(--accent-blue)';
    else if (task.status === 'SUCCESS') statusElement.style.color = 'var(--accent-green)';
    else if (task.status === 'FAILED') statusElement.style.color = 'var(--accent-red)';
    else if (task.status === 'PAUSED') statusElement.style.color = 'var(--accent-orange)';

    const termContent = document.getElementById('terminal-content');
    const terminal = document.getElementById('terminal-logs');
    const shouldScroll = terminal.scrollHeight - terminal.clientHeight <= terminal.scrollTop + 50;

    termContent.textContent = task.logs || '[No logs received yet...]';

    if (shouldScroll) {
        terminal.scrollTop = terminal.scrollHeight;
    }
}

function hideModal() {
    selectedTaskId = null;
    document.getElementById('logs-modal').classList.remove('active');
}

function closeModal(event) {
    if (event.target.id === 'logs-modal') {
        hideModal();
    }
}

/* ==========================================================================
   Kiwi Administration Console JS logic
   ========================================================================== */

function toggleAdminPanel(show) {
    const modal = document.getElementById('admin-modal');
    if (show) {
        modal.classList.add('active');
        adminLoadOrgs();
    } else {
        modal.classList.remove('active');
    }
}

function switchAdminTab(tabName) {
    currentAdminTab = tabName;
    document.querySelectorAll('.admin-tab').forEach(btn => btn.classList.remove('active'));
    document.querySelectorAll('.admin-tab-content').forEach(c => c.classList.remove('active'));

    document.getElementById('tab-' + tabName).classList.add('active');
    document.getElementById('content-' + tabName).classList.add('active');

    if (tabName === 'orgs') {
        adminLoadOrgs();
    } else if (tabName === 'users') {
        adminLoadOrgsSelect('admin-users-org-select').then(() => adminLoadUsers());
    } else if (tabName === 'providers') {
        adminLoadOrgsSelect('admin-providers-org-select').then(() => adminLoadProviderConfig());
    } else if (tabName === 'audit') {
        adminLoadOrgsSelect('admin-audit-org-select').then(() => adminLoadAuditLogs());
    }
}

// Load organizations into table
async function adminLoadOrgs() {
    try {
        const response = await fetch(`${serverUrl}/admin/orgs`, {
            headers: { 'Authorization': `Bearer ${authToken}` }
        });
        if (!response.ok) throw new Error();
        const orgs = await response.json();
        const listEl = document.getElementById('admin-orgs-list');
        listEl.innerHTML = '';

        if (!orgs || orgs.length === 0) {
            listEl.innerHTML = '<tr><td colspan="3" style="text-align:center">No organizations registered.</td></tr>';
            return;
        }

        orgs.forEach(org => {
            const row = document.createElement('tr');
            row.innerHTML = `<td><code>${org.id}</code></td><td>${escapeHtml(org.name)}</td><td>${new Date(org.created_at).toLocaleString()}</td>`;
            listEl.appendChild(row);
        });
    } catch (err) {
        console.error("Failed to load orgs in admin console:", err);
    }
}

// Load organizations into dropdown selection
async function adminLoadOrgsSelect(elementId) {
    try {
        const response = await fetch(`${serverUrl}/admin/orgs`, {
            headers: { 'Authorization': `Bearer ${authToken}` }
        });
        if (!response.ok) throw new Error();
        const orgs = await response.json();
        const selectEl = document.getElementById(elementId);
        selectEl.innerHTML = '';

        if (!orgs || orgs.length === 0) {
            const opt = document.createElement('option');
            opt.textContent = "No orgs available";
            selectEl.appendChild(opt);
            return;
        }

        orgs.forEach(org => {
            const opt = document.createElement('option');
            opt.value = org.id;
            opt.textContent = org.name;
            selectEl.appendChild(opt);
        });
    } catch (err) {
        console.error("Failed to load orgs in select:", err);
    }
}

// Create organization
async function adminCreateOrg() {
    const nameInput = document.getElementById('new-org-name');
    const name = nameInput.value.trim();
    if (!name) return;

    try {
        const response = await fetch(`${serverUrl}/admin/orgs`, {
            method: 'POST',
            headers: {
                'Authorization': `Bearer ${authToken}`,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ name })
        });
        if (!response.ok) throw new Error();
        nameInput.value = '';
        adminLoadOrgs();
    } catch (err) {
        alert("Failed to create organization");
    }
}

// Load users in selected organization
async function adminLoadUsers() {
    const orgSelect = document.getElementById('admin-users-org-select');
    const orgID = orgSelect.value;
    if (!orgID) return;

    // Reset keys panel
    document.getElementById('admin-keys-section').style.display = 'none';
    activeAdminUserId = null;

    try {
        const response = await fetch(`${serverUrl}/admin/orgs/${orgID}/users`, {
            headers: { 'Authorization': `Bearer ${authToken}` }
        });
        if (!response.ok) throw new Error();
        const users = await response.json();
        const listEl = document.getElementById('admin-users-list');
        listEl.innerHTML = '';

        if (!users || users.length === 0) {
            listEl.innerHTML = '<tr><td colspan="5" style="text-align:center">No users registered in this organization.</td></tr>';
            return;
        }

        users.forEach(user => {
            const row = document.createElement('tr');
            row.innerHTML = `
                <td><code>${user.id}</code></td>
                <td>${escapeHtml(user.name)}</td>
                <td>${user.email}</td>
                <td><span class="status-badge" style="background:${user.role === 'admin' ? 'var(--accent-red)' : 'rgba(255,255,255,0.05)'}">${user.role}</span></td>
                <td><button class="admin-table-action-btn" onclick="adminShowKeys('${user.id}', '${escapeHtml(user.name)}')">Manage Keys</button></td>
            `;
            listEl.appendChild(row);
        });
    } catch (err) {
        console.error("Failed to load users:", err);
    }
}

// Create user
async function adminCreateUser() {
    const orgSelect = document.getElementById('admin-users-org-select');
    const orgID = orgSelect.value;
    const nameInput = document.getElementById('new-user-name');
    const emailInput = document.getElementById('new-user-email');
    const roleSelect = document.getElementById('new-user-role');

    const name = nameInput.value.trim();
    const email = emailInput.value.trim();
    const role = roleSelect.value;

    if (!orgID || !name || !email) return;

    try {
        const response = await fetch(`${serverUrl}/admin/orgs/${orgID}/users`, {
            method: 'POST',
            headers: {
                'Authorization': `Bearer ${authToken}`,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ name, email, role })
        });
        if (!response.ok) throw new Error();
        nameInput.value = '';
        emailInput.value = '';
        adminLoadUsers();
    } catch (err) {
        alert("Failed to register user");
    }
}

// Show API keys for user
async function adminShowKeys(userID, name) {
    activeAdminUserId = userID;
    document.getElementById('admin-keys-title').textContent = `API Keys for ${name}`;
    document.getElementById('admin-keys-section').style.display = 'block';
    adminLoadKeys();
}

// Load keys for selected user
async function adminLoadKeys() {
    if (!activeAdminUserId) return;
    const orgSelect = document.getElementById('admin-users-org-select');
    const orgID = orgSelect.value;

    try {
        const response = await fetch(`${serverUrl}/admin/orgs/${orgID}/users/${activeAdminUserId}/keys`, {
            headers: { 'Authorization': `Bearer ${authToken}` }
        });
        if (!response.ok) throw new Error();
        const keys = await response.json();
        const listEl = document.getElementById('admin-keys-list');
        listEl.innerHTML = '';

        if (!keys || keys.length === 0) {
            listEl.innerHTML = '<tr><td colspan="6" style="text-align:center">No API keys generated.</td></tr>';
            return;
        }

        keys.forEach(key => {
            const isRevoked = !!key.revoked_at;
            const isExpired = key.expires_at && new Date(key.expires_at) < new Date();
            let statusMarkup = '<span class="card-status-badge status-success">Active</span>';
            if (isRevoked) statusMarkup = '<span class="card-status-badge status-failed">Revoked</span>';
            else if (isExpired) statusMarkup = '<span class="card-status-badge status-paused">Expired</span>';

            const row = document.createElement('tr');
            row.innerHTML = `
                <td><code>${key.id}</code></td>
                <td><code>${key.token_preview || '********************'}</code></td>
                <td>${new Date(key.created_at).toLocaleString()}</td>
                <td>${key.expires_at ? new Date(key.expires_at).toLocaleString() : 'Never'}</td>
                <td>${statusMarkup}</td>
                <td>
                    ${isRevoked ? '' : `<button class="admin-table-action-btn" style="background:rgba(239,68,68,0.2);color:var(--accent-red)" onclick="adminRevokeKey('${key.id}')">Revoke</button>`}
                </td>
            `;
            listEl.appendChild(row);
        });
    } catch (err) {
        console.error("Failed to load user keys:", err);
    }
}

// Generate API key
async function adminCreateKey() {
    if (!activeAdminUserId) return;
    const orgSelect = document.getElementById('admin-users-org-select');
    const orgID = orgSelect.value;

    try {
        const response = await fetch(`${serverUrl}/admin/orgs/${orgID}/users/${activeAdminUserId}/keys`, {
            method: 'POST',
            headers: {
                'Authorization': `Bearer ${authToken}`,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ days_valid: 30 }) // default 30 days expiration
        });
        if (!response.ok) throw new Error();
        const key = await response.json();

        // Prompt the generated token to the admin as they only get to see it once!
        alert(`API Key generated successfully!\n\nIMPORTANT: Copy this key now. It will not be shown again.\n\nKey: ${key.token}`);
        adminLoadKeys();
    } catch (err) {
        alert("Failed to generate API Key");
    }
}

// Revoke API Key
async function adminRevokeKey(keyID) {
    if (!activeAdminUserId || !confirm("Are you sure you want to revoke this API Key? It will immediately stop working.")) return;
    const orgSelect = document.getElementById('admin-users-org-select');
    const orgID = orgSelect.value;

    try {
        const response = await fetch(`${serverUrl}/admin/orgs/${orgID}/users/${activeAdminUserId}/keys/${keyID}`, {
            method: 'DELETE',
            headers: { 'Authorization': `Bearer ${authToken}` }
        });
        if (!response.ok) throw new Error();
        adminLoadKeys();
    } catch (err) {
        alert("Failed to revoke API key");
    }
}

// Load provider config for LLM tab
async function adminLoadProviderConfig() {
    const orgSelect = document.getElementById('admin-providers-org-select');
    const orgID = orgSelect.value;
    if (!orgID) return;

    // Reset fields
    document.getElementById('provider-api-key').value = '';
    document.getElementById('provider-actor-model').value = '';
    document.getElementById('provider-critic-model').value = '';
    document.getElementById('provider-success-msg').style.display = 'none';

    try {
        const response = await fetch(`${serverUrl}/admin/orgs/${orgID}/provider`, {
            headers: { 'Authorization': `Bearer ${authToken}` }
        });
        if (!response.ok) return; // Silent fallback if config doesn't exist yet
        const config = await response.json();

        document.getElementById('provider-actor-model').value = config.actor_model || '';
        document.getElementById('provider-critic-model').value = config.critic_model || '';
    } catch (err) {
        console.error("Failed to load provider config:", err);
    }
}

// Save provider config
async function adminSaveProviderConfig() {
    const orgSelect = document.getElementById('admin-providers-org-select');
    const orgID = orgSelect.value;
    const key = document.getElementById('provider-api-key').value.trim();
    const actor = document.getElementById('provider-actor-model').value.trim();
    const critic = document.getElementById('provider-critic-model').value.trim();

    if (!orgID) return;

    try {
        const response = await fetch(`${serverUrl}/admin/orgs/${orgID}/provider`, {
            method: 'PUT',
            headers: {
                'Authorization': `Bearer ${authToken}`,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({
                api_key: key,
                actor_model: actor,
                critic_model: critic,
                provider_name: "anthropic"
            })
        });

        if (!response.ok) throw new Error();
        const successEl = document.getElementById('provider-success-msg');
        successEl.style.display = 'block';
        successEl.textContent = 'Configuration saved and API Key encrypted successfully!';
        
        // Reset key field for safety
        document.getElementById('provider-api-key').value = '';
        setTimeout(() => { successEl.style.display = 'none'; }, 4000);
    } catch (err) {
        alert("Failed to save provider configuration");
    }
}

async function adminLoadAuditLogs() {
    const orgSelect = document.getElementById('admin-audit-org-select');
    const orgID = orgSelect.value;
    if (!orgID) return;

    try {
        const response = await fetch(`${serverUrl}/admin/orgs/${orgID}/audit`, {
            headers: { 'Authorization': `Bearer ${authToken}` }
        });
        if (!response.ok) throw new Error();
        const logs = await response.json();
        const listEl = document.getElementById('admin-audit-list');
        listEl.innerHTML = '';

        if (!logs || logs.length === 0) {
            listEl.innerHTML = '<tr><td colspan="6" style="text-align:center">No audit logs recorded for this organization.</td></tr>';
            return;
        }

        logs.forEach(log => {
            const row = document.createElement('tr');
            row.innerHTML = `
                <td><code>${new Date(log.created_at).toLocaleString()}</code></td>
                <td><code>${escapeHtml(log.client_ip || 'N/A')}</code></td>
                <td>${escapeHtml(log.user_email || 'System')}</td>
                <td><span class="status-badge" style="background:rgba(255,255,255,0.05);color:var(--text-primary)">${log.action}</span></td>
                <td><span style="color:var(--accent-blue)">${log.resource}</span> (ID: <code>${log.resource_id}</code>)</td>
                <td>${escapeHtml(log.details)}</td>
            `;
            listEl.appendChild(row);
        });
    } catch (err) {
        console.error("Failed to load audit logs:", err);
    }
}
