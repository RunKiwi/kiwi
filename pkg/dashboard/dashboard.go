package dashboard

import (
	"net/http"
)

// DashboardHTML is a self-contained premium HTML/CSS/JS dashboard for Kiwi.
const DashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Kiwi - Control Plane Dashboard</title>
    <!-- Inter & JetBrains Mono Fonts -->
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@300;400;500;600;700&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet">
    <style>
        :root {
            --bg-color: #0f111a;
            --card-bg: rgba(26, 29, 46, 0.65);
            --border-color: rgba(255, 255, 255, 0.08);
            --text-primary: #f3f4f6;
            --text-secondary: #9ca3af;
            --accent-blue: #3b82f6;
            --accent-teal: #14b8a6;
            --accent-orange: #f59e0b;
            --accent-green: #10b981;
            --accent-red: #ef4444;
            --glass-blur: blur(12px);
        }

        * {
            box-sizing: border-box;
            margin: 0;
            padding: 0;
        }

        body {
            background-color: var(--bg-color);
            color: var(--text-primary);
            font-family: 'Inter', sans-serif;
            min-height: 100vh;
            display: flex;
            flex-direction: column;
            overflow-x: hidden;
            background-image: 
                radial-gradient(at 0% 0%, rgba(59, 130, 246, 0.15) 0px, transparent 50%),
                radial-gradient(at 100% 0%, rgba(20, 184, 166, 0.1) 0px, transparent 50%),
                radial-gradient(at 50% 100%, rgba(239, 68, 68, 0.05) 0px, transparent 50%);
        }

        header {
            padding: 1.5rem 2rem;
            background: rgba(15, 17, 26, 0.7);
            backdrop-filter: var(--glass-blur);
            border-bottom: 1px solid var(--border-color);
            position: sticky;
            top: 0;
            z-index: 50;
        }

        .header-container {
            max-width: 1400px;
            margin: 0 auto;
            display: flex;
            flex-direction: column;
            gap: 1rem;
        }

        @media(min-width: 768px) {
            .header-container {
                flex-direction: row;
                align-items: center;
                justify-content: space-between;
            }
        }

        .logo-section h1 {
            font-size: 1.5rem;
            font-weight: 700;
            letter-spacing: -0.05em;
            background: linear-gradient(to right, #3b82f6, #14b8a6);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            display: flex;
            align-items: center;
            gap: 0.5rem;
        }

        .logo-section p {
            font-size: 0.75rem;
            color: var(--text-secondary);
            margin-top: 0.2rem;
        }

        .header-controls {
            display: flex;
            align-items: center;
            gap: 1rem;
            flex-wrap: wrap;
        }

        .search-bar {
            position: relative;
        }

        .search-bar input {
            background: rgba(255, 255, 255, 0.05);
            border: 1px solid var(--border-color);
            border-radius: 9999px;
            padding: 0.5rem 1rem 0.5rem 2.5rem;
            color: var(--text-primary);
            font-family: inherit;
            font-size: 0.875rem;
            width: 260px;
            transition: all 0.2s ease;
        }

        .search-bar input:focus {
            outline: none;
            border-color: var(--accent-blue);
            background: rgba(255, 255, 255, 0.08);
            box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.2);
        }

        .search-bar svg {
            position: absolute;
            left: 0.85rem;
            top: 50%;
            transform: translateY(-50%);
            width: 1rem;
            height: 1rem;
            fill: var(--text-secondary);
            pointer-events: none;
        }

        .stats-summary {
            display: flex;
            align-items: center;
            gap: 1.5rem;
            background: rgba(255, 255, 255, 0.03);
            border: 1px solid var(--border-color);
            padding: 0.5rem 1.25rem;
            border-radius: 9999px;
            font-size: 0.8rem;
        }

        .stat-item {
            display: flex;
            align-items: center;
            gap: 0.4rem;
        }

        .stat-dot {
            width: 6px;
            height: 6px;
            border-radius: 50%;
            display: inline-block;
        }

        main {
            flex: 1;
            padding: 2rem;
            max-width: 1400px;
            width: 100%;
            margin: 0 auto;
            display: flex;
            flex-direction: column;
            gap: 2rem;
        }

        .kanban-grid {
            display: grid;
            grid-template-columns: repeat(1, minmax(0, 1fr));
            gap: 1.5rem;
            align-items: start;
        }

        @media(min-width: 640px) {
            .kanban-grid {
                grid-template-columns: repeat(2, minmax(0, 1fr));
            }
        }

        @media(min-width: 1024px) {
            .kanban-grid {
                grid-template-columns: repeat(5, minmax(0, 1fr));
            }
        }

        .kanban-column {
            background: rgba(17, 19, 31, 0.6);
            border: 1px solid var(--border-color);
            border-radius: 12px;
            display: flex;
            flex-direction: column;
            max-height: 80vh;
            backdrop-filter: var(--glass-blur);
        }

        .column-header {
            padding: 1rem;
            border-bottom: 1px solid var(--border-color);
            display: flex;
            align-items: center;
            justify-content: space-between;
        }

        .column-title {
            font-size: 0.875rem;
            font-weight: 600;
            text-transform: uppercase;
            letter-spacing: 0.05em;
            display: flex;
            align-items: center;
            gap: 0.5rem;
        }

        .column-badge {
            background: rgba(255, 255, 255, 0.08);
            padding: 0.15rem 0.5rem;
            border-radius: 9999px;
            font-size: 0.75rem;
            font-weight: 500;
        }

        .column-cards {
            padding: 1rem;
            display: flex;
            flex-direction: column;
            gap: 1rem;
            overflow-y: auto;
            flex: 1;
            min-height: 150px;
        }

        .task-card {
            background: var(--card-bg);
            border: 1px solid var(--border-color);
            border-radius: 8px;
            padding: 1rem;
            cursor: pointer;
            transition: all 0.25s cubic-bezier(0.4, 0, 0.2, 1);
            position: relative;
            overflow: hidden;
            display: flex;
            flex-direction: column;
            gap: 0.75rem;
        }

        .task-card::before {
            content: '';
            position: absolute;
            top: 0;
            left: 0;
            width: 3px;
            height: 100%;
            background: transparent;
            transition: background-color 0.2s ease;
        }

        .task-card:hover {
            transform: translateY(-2px);
            border-color: rgba(255, 255, 255, 0.15);
            box-shadow: 0 10px 20px -10px rgba(0, 0, 0, 0.5);
            background: rgba(30, 34, 54, 0.85);
        }

        .task-card.running:hover { box-shadow: 0 10px 20px -10px rgba(59, 130, 246, 0.15); }
        .task-card.success:hover { box-shadow: 0 10px 20px -10px rgba(16, 185, 129, 0.15); }
        .task-card.failed:hover { box-shadow: 0 10px 20px -10px rgba(239, 68, 68, 0.15); }
        .task-card.paused:hover { box-shadow: 0 10px 20px -10px rgba(245, 158, 11, 0.15); }

        .task-card.backlog::before { background: var(--text-secondary); }
        .task-card.running::before { background: var(--accent-blue); }
        .task-card.paused::before { background: var(--accent-orange); }
        .task-card.success::before { background: var(--accent-green); }
        .task-card.failed::before { background: var(--accent-red); }

        .card-header {
            display: flex;
            align-items: center;
            justify-content: space-between;
            font-size: 0.75rem;
        }

        .card-id {
            font-family: 'JetBrains Mono', monospace;
            font-weight: 500;
            color: var(--text-secondary);
            background: rgba(255, 255, 255, 0.05);
            padding: 0.1rem 0.4rem;
            border-radius: 4px;
        }

        .card-status-badge {
            font-size: 0.65rem;
            font-weight: 600;
            text-transform: uppercase;
            padding: 0.1rem 0.4rem;
            border-radius: 4px;
        }

        .status-backlog { background: rgba(156, 163, 175, 0.1); color: var(--text-secondary); }
        .status-running { background: rgba(59, 130, 246, 0.1); color: var(--accent-blue); }
        .status-paused { background: rgba(245, 158, 11, 0.1); color: var(--accent-orange); }
        .status-success { background: rgba(16, 185, 129, 0.1); color: var(--accent-green); }
        .status-failed { background: rgba(239, 68, 68, 0.1); color: var(--accent-red); }

        .card-body {
            font-size: 0.85rem;
            line-height: 1.4;
            color: var(--text-primary);
            word-break: break-word;
        }

        .card-file-path {
            font-size: 0.7rem;
            color: var(--text-secondary);
            display: flex;
            align-items: center;
            gap: 0.3rem;
            margin-top: 0.25rem;
        }

        .card-footer {
            display: flex;
            align-items: center;
            justify-content: space-between;
            font-size: 0.75rem;
            color: var(--text-secondary);
            border-top: 1px solid rgba(255, 255, 255, 0.05);
            padding-top: 0.5rem;
        }

        .card-cost {
            font-weight: 600;
            color: var(--accent-teal);
        }

        .modal-overlay {
            position: fixed;
            top: 0;
            left: 0;
            right: 0;
            bottom: 0;
            background: rgba(10, 11, 18, 0.85);
            backdrop-filter: blur(8px);
            z-index: 100;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 2rem;
            opacity: 0;
            pointer-events: none;
            transition: opacity 0.3s ease;
        }

        .modal-overlay.active {
            opacity: 1;
            pointer-events: all;
        }

        .modal-container {
            background: rgba(17, 19, 31, 0.95);
            border: 1px solid var(--border-color);
            border-radius: 12px;
            width: 100%;
            max-width: 850px;
            height: 80vh;
            display: flex;
            flex-direction: column;
            overflow: hidden;
            box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.5);
            transform: scale(0.95);
            transition: transform 0.3s ease;
        }

        .modal-overlay.active .modal-container {
            transform: scale(1);
        }

        .modal-header {
            padding: 1rem 1.5rem;
            border-bottom: 1px solid var(--border-color);
            display: flex;
            align-items: center;
            justify-content: space-between;
        }

        .modal-title-area h3 {
            font-size: 1.1rem;
            font-weight: 600;
        }

        .modal-title-area p {
            font-size: 0.75rem;
            color: var(--text-secondary);
            margin-top: 0.2rem;
        }

        .modal-close-btn {
            background: rgba(255, 255, 255, 0.05);
            border: 1px solid var(--border-color);
            border-radius: 6px;
            padding: 0.4rem;
            cursor: pointer;
            color: var(--text-primary);
            display: flex;
            align-items: center;
            justify-content: center;
            transition: all 0.2s ease;
        }

        .modal-close-btn:hover {
            background: rgba(239, 68, 68, 0.15);
            border-color: rgba(239, 68, 68, 0.3);
            color: var(--accent-red);
        }

        .terminal-container {
            flex: 1;
            background: #05070c;
            padding: 1.5rem;
            overflow-y: auto;
            font-family: 'JetBrains Mono', monospace;
            font-size: 0.85rem;
            color: #d1d5db;
            line-height: 1.5;
            white-space: pre-wrap;
            position: relative;
        }

        .terminal-header {
            font-size: 0.7rem;
            color: #4b5563;
            border-bottom: 1px solid #111827;
            padding-bottom: 0.5rem;
            margin-bottom: 1rem;
            display: flex;
            align-items: center;
            justify-content: space-between;
        }

        .empty-column-text {
            color: var(--text-secondary);
            font-size: 0.75rem;
            text-align: center;
            padding: 2rem 0;
            border: 1px dashed var(--border-color);
            border-radius: 6px;
        }
    </style>
</head>
<body>
    <header>
        <div class="header-container">
            <div class="logo-section">
                <h1>
                    <svg style="width:24px;height:24px;fill:currentColor" viewBox="0 0 24 24"><path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm1 17h-2v-2h2v2zm2.07-7.75l-.9.92C13.45 12.9 13 13.5 13 15h-2v-.5c0-1.1.45-2.1 1.17-2.83l1.24-1.26c.37-.36.59-.86.59-1.41 0-1.1-.9-2-2-2s-2 .9-2 2H7c0-2.76 2.24-5 5-5s5 2.24 5 5c0 1.04-.42 1.99-1.07 2.75z"/></svg>
                    KIWI CONTROL PLANE
                </h1>
                <p>Secure Agentic Sandbox Loop Orchestration</p>
            </div>
            
            <div class="header-controls">
                <div class="stats-summary">
                    <div class="stat-item"><span class="stat-dot" style="background:var(--accent-blue)"></span><span id="stat-running">0</span> Running</div>
                    <div class="stat-item"><span class="stat-dot" style="background:var(--accent-green)"></span><span id="stat-success">0</span> Success</div>
                    <div class="stat-item"><span class="stat-dot" style="background:var(--accent-red)"></span><span id="stat-failed">0</span> Failed</div>
                    <div class="stat-item" style="border-left: 1px solid var(--border-color); padding-left: 1rem;"><span id="stat-cost" style="color:var(--accent-teal);font-weight:600">$0.00</span> Total Cost</div>
                </div>
                
                <div class="search-bar">
                    <svg viewBox="0 0 24 24"><path d="M15.5 14h-.79l-.28-.27C15.41 12.59 16 11.11 16 9.5 16 5.91 13.09 3 9.5 3S3 5.91 3 9.5 5.91 16 9.5 16c1.61 0 3.09-.59 4.23-1.57l.27.28v.79l5 4.99L20.49 19l-4.99-5zm-6 0C7.01 14 5 11.99 5 9.5S7.01 5 9.5 5 14 7.01 14 9.5 11.99 14 9.5 14z"/></svg>
                    <input type="text" id="search-input" placeholder="Filter by ID or path..." oninput="applyFilter()">
                </div>
            </div>
        </div>
    </header>

    <main>
        <div class="kanban-grid">
            <!-- Backlog -->
            <div class="kanban-column" id="col-backlog">
                <div class="column-header">
                    <div class="column-title"><span style="color:var(--text-secondary)">●</span> Backlog</div>
                    <div class="column-badge" id="badge-backlog">0</div>
                </div>
                <div class="column-cards" id="cards-backlog"></div>
            </div>

            <!-- Running -->
            <div class="kanban-column" id="col-running">
                <div class="column-header">
                    <div class="column-title"><span style="color:var(--accent-blue)">●</span> Running</div>
                    <div class="column-badge" id="badge-running">0</div>
                </div>
                <div class="column-cards" id="cards-running"></div>
            </div>

            <!-- Paused -->
            <div class="kanban-column" id="col-paused">
                <div class="column-header">
                    <div class="column-title"><span style="color:var(--accent-orange)">●</span> Paused</div>
                    <div class="column-badge" id="badge-paused">0</div>
                </div>
                <div class="column-cards" id="cards-paused"></div>
            </div>

            <!-- Success -->
            <div class="kanban-column" id="col-success">
                <div class="column-header">
                    <div class="column-title"><span style="color:var(--accent-green)">●</span> Success</div>
                    <div class="column-badge" id="badge-success">0</div>
                </div>
                <div class="column-cards" id="cards-success"></div>
            </div>

            <!-- Failed -->
            <div class="kanban-column" id="col-failed">
                <div class="column-header">
                    <div class="column-title"><span style="color:var(--accent-red)">●</span> Failed</div>
                    <div class="column-badge" id="badge-failed">0</div>
                </div>
                <div class="column-cards" id="cards-failed"></div>
            </div>
        </div>
    </main>

    <!-- Modal for Logs -->
    <div class="modal-overlay" id="logs-modal" onclick="closeModal(event)">
        <div class="modal-container" onclick="event.stopPropagation()">
            <div class="modal-header">
                <div class="modal-title-area">
                    <h3 id="modal-task-title">Task Live Logs</h3>
                    <p id="modal-task-sub">ID: <span id="modal-task-id" style="font-family:monospace">N/A</span> | File: <span id="modal-task-path">N/A</span></p>
                </div>
                <button class="modal-close-btn" onclick="hideModal()">
                    <svg style="width:20px;height:20px" viewBox="0 0 24 24"><path fill="currentColor" d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z"/></svg>
                </button>
            </div>
            <div class="terminal-container" id="terminal-logs">
                <div class="terminal-header">
                    <span>KIWI TERMINAL v1.0.0</span>
                    <span id="terminal-status">STATUS: IDLE</span>
                </div>
                <div id="terminal-content"></div>
            </div>
        </div>
    </div>

    <script>
        let allTasks = [];
        let filterText = "";
        let selectedTaskId = null;

        // Fetch tasks from server
        async function fetchTasks() {
            try {
                const token = localStorage.getItem('kiwi_auth_token') || 'kiwi-auth-token-1234';
                const response = await fetch('/tasks', {
                    headers: {
                        'Authorization': 'Bearer ' + token
                    }
                });
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
            }
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

        fetchTasks();
        setInterval(fetchTasks, 2000);
    </script>
</body>
</html>`

// HandleDashboard serves the dashboard HTML.
func HandleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(DashboardHTML))
}
