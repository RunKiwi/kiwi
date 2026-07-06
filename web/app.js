let allTasks = [];
let filterText = "";
let selectedTaskId = null;

// Settings and Config Management
let serverUrl = localStorage.getItem('kiwi_server_url') || 'http://localhost:8080';
let authToken = localStorage.getItem('kiwi_auth_token') || 'kiwi-auth-token-1234';

// Initialize settings inputs
document.addEventListener('DOMContentLoaded', () => {
    document.getElementById('setting-server-url').value = serverUrl;
    document.getElementById('setting-auth-token').value = authToken;
    
    // Initial fetch and start interval
    fetchTasks();
    setInterval(fetchTasks, 2000);
});

function toggleSettingsPanel() {
    const overlay = document.getElementById('settings-overlay');
    overlay.classList.toggle('active');
}

function saveSettings() {
    const urlInput = document.getElementById('setting-server-url').value.trim();
    const tokenInput = document.getElementById('setting-auth-token').value.trim();
    
    if (urlInput) {
        serverUrl = urlInput.replace(/\/$/, ""); // Strip trailing slash
        localStorage.setItem('kiwi_server_url', serverUrl);
    }
    if (tokenInput) {
        authToken = tokenInput;
        localStorage.setItem('kiwi_auth_token', authToken);
    }
    
    toggleSettingsPanel();
    fetchTasks(); // Refresh immediately
}

// Fetch tasks from server
async function fetchTasks() {
    try {
        const response = await fetch(`${serverUrl}/tasks`, {
            headers: {
                'Authorization': `Bearer ${authToken}`
            }
        });
        
        if (response.status === 401) {
            console.warn("Unauthorized API access - check settings bearer token");
            // If unauthorized, fallback to show error or highlight settings
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
    // Show 401 warning in columns
    const cols = ['backlog', 'running', 'paused', 'success', 'failed'];
    cols.forEach(col => {
        const el = document.getElementById('cards-' + col);
        if (el) el.innerHTML = '<div class="empty-column-text" style="color:var(--accent-red)">401 Unauthorized<br><span style="font-size:0.65rem">Check settings bearer token</span></div>';
    });
}

function showConnectionError() {
    // Show offline connection warning in columns
    const cols = ['backlog', 'running', 'paused', 'success', 'failed'];
    cols.forEach(col => {
        const el = document.getElementById('cards-' + col);
        if (el) el.innerHTML = '<div class="empty-column-text" style="color:var(--accent-orange)">Offline / Server Connection Refused<br><span style="font-size:0.65rem">Check if Kiwi daemon is running</span></div>';
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
