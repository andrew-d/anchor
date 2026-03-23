import { h, render } from 'preact';
import { useState, useEffect, useRef } from 'preact/hooks';
import htm from 'htm';
import {
    formatTime, formatRelativeTime, formatDuration, formatTimeAgo,
    createConnectionTracker,
} from './util.js';

const html = htm.bind(h);

// === Theme ===

function getTheme() {
    return document.documentElement.getAttribute('data-theme') || 'light';
}

function setTheme(theme) {
    document.documentElement.setAttribute('data-theme', theme);
    localStorage.setItem('anchor-theme', theme);
}

function toggleTheme() {
    setTheme(getTheme() === 'dark' ? 'light' : 'dark');
}

// === Connection Tracking ===

const connection = createConnectionTracker();

function useConnectionStatus() {
    const [status, setStatus] = useState(connection.status);
    useEffect(() => {
        const update = () => setStatus(connection.status);
        connection.listeners.add(update);
        return () => connection.listeners.delete(update);
    }, []);
    return status;
}

// === Router ===

function useHashRoute() {
    const [route, setRoute] = useState(window.location.hash.slice(1) || '/');

    useEffect(() => {
        const onHashChange = () => setRoute(window.location.hash.slice(1) || '/');
        window.addEventListener('hashchange', onHashChange);
        return () => window.removeEventListener('hashchange', onHashChange);
    }, []);

    return route;
}

// === API Helpers ===

async function apiRequest(url, options = {}) {
    let res;
    try {
        res = await fetch(url, options);
    } catch (e) {
        connection.failure();
        throw new Error('Network error: server unreachable');
    }
    connection.success();
    return res;
}

async function fetchJSON(url) {
    const res = await apiRequest(url);
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
    return res.json();
}

async function parseError(res) {
    try {
        const body = await res.json();
        if (body.error) return new Error(body.error);
    } catch (_) {}
    return new Error(`${res.status} ${res.statusText}`);
}

async function postJSON(url, body) {
    const res = await apiRequest(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body)
    });
    if (!res.ok) throw await parseError(res);
    return res.json();
}

async function putJSON(url, body) {
    const res = await apiRequest(url, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body)
    });
    if (!res.ok) throw await parseError(res);
    return res.json();
}

async function deleteJSON(url) {
    const res = await apiRequest(url, { method: 'DELETE' });
    if (!res.ok) throw await parseError(res);
    return res.json();
}

// === Shared Components ===

function ErrorBanner({ message, onDismiss }) {
    return html`
        <div class="error-banner">
            <span>${message}</span>
            <button onClick=${onDismiss} class="error-banner-dismiss" title="Dismiss">✕</button>
        </div>
    `;
}

function Badge({ status }) {
    return html`<span class="badge badge--${status}">${status}</span>`;
}

function ConnectionDot({ status }) {
    const labels = {
        connected: 'Connected',
        degraded: 'Connection unstable',
        disconnected: 'Connection lost'
    };
    return html`<span class="conn-dot conn-dot--${status}" title=${labels[status] || 'Unknown'}></span>`;
}

function ThemeToggle() {
    const [, setTick] = useState(0);
    const handleClick = () => {
        toggleTheme();
        setTick(t => t + 1);
    };
    return html`<button class="theme-toggle" onClick=${handleClick} title=${`Switch to ${getTheme() === 'dark' ? 'light' : 'dark'} mode`} aria-label="Toggle theme"></button>`;
}

function EmptyState({ children }) {
    return html`<div class="empty-state">${children}</div>`;
}

function ErrorState({ error, onRetry }) {
    return html`
        <div class="error-state">
            <p class="error-message">Something went wrong</p>
            <p class="error-detail">${error}</p>
            ${onRetry && html`<button class="btn btn--primary" onClick=${onRetry}>Retry</button>`}
        </div>
    `;
}

function LoadingState() {
    return html`<div class="loading-state">Loading...</div>`;
}

// === Dashboard ===

function Dashboard() {
    const [data, setData] = useState(null);
    const [error, setError] = useState(null);

    const load = () => {
        fetchJSON('/api/agents')
            .then(d => { setData(d); setError(null); })
            .catch(e => setError(e.message));
    };

    useEffect(() => {
        load();
        const id = setInterval(load, 10000);
        return () => clearInterval(id);
    }, []);

    if (!data && error) return html`<div class="container"><a href="#/" class="back-link">← Dashboard</a><${ErrorState} error=${error} onRetry=${load} /></div>`;
    if (!data) return html`<div class="container"><${LoadingState} /></div>`;

    const agents = data.agents || [];
    const counts = { unhealthy: 0, stale: 0, healthy: 0 };
    agents.forEach(a => { counts[a.health] = (counts[a.health] || 0) + 1; });

    const groups = [
        { key: 'unhealthy', label: 'Unhealthy', agents: agents.filter(a => a.health === 'unhealthy') },
        { key: 'stale', label: 'Stale', agents: agents.filter(a => a.health === 'stale') },
        { key: 'healthy', label: 'Healthy', agents: agents.filter(a => a.health === 'healthy') },
    ];

    return html`
        <div class="container fade-in">
            ${error && html`<${ErrorBanner} message=${error} onDismiss=${() => setError(null)} />`}

            <div class="health-summary">
                <div class="health-cell health-cell--error ${counts.unhealthy > 0 ? 'health-cell--active' : ''}">
                    <span class="health-count">${counts.unhealthy}</span>
                    <span class="health-label">Unhealthy</span>
                </div>
                <div class="health-cell health-cell--warn ${counts.stale > 0 ? 'health-cell--active' : ''}" title="Agents that haven't checked in within ${formatDuration(2 * (data.poll_interval_seconds || 300))}">
                    <span class="health-count">${counts.stale}</span>
                    <span class="health-label">Stale</span>
                    <span class="health-hint">Not checked in</span>
                </div>
                <div class="health-cell health-cell--ok">
                    <span class="health-count">${counts.healthy}</span>
                    <span class="health-label">Healthy</span>
                </div>
            </div>

            ${agents.length === 0 && html`
                <${EmptyState}>
                    <p>No agents have checked in yet.</p>
                    <p>Run <code>anchor agent</code> on a host to connect.</p>
                <//>
            `}

            ${groups.filter(g => g.agents.length > 0).map(g => html`
                <div class="status-group group-${g.key}">
                    <div class="status-group-header">${g.label} (${g.agents.length})</div>
                    <div class="agent-list">
                        ${g.agents.map(a => html`
                            <a href="#/agents/${a.id}" class="agent-card agent-card--${a.health}">
                                <div class="agent-card-info">
                                    <div class="agent-card-name">${a.display_name || a.hostname}</div>
                                    <div class="agent-card-meta">${a.remote_ip} · ${a.id.slice(-8)}</div>
                                </div>
                                <div class="agent-card-tags">
                                    ${(a.tags || []).map(t => html`<span class="tag-pill tag-pill--small">${t.name}</span>`)}
                                </div>
                                <div class="agent-card-stats">
                                    <span>${a.module_count} module${a.module_count !== 1 ? 's' : ''}</span>
                                    ${a.error_count > 0 && html`<span class="badge badge--error">${a.error_count} err</span>`}
                                </div>
                            </a>
                        `)}
                    </div>
                </div>
            `)}
        </div>
    `;
}

// === Agent Detail ===

function AgentDetail({ id }) {
    const [data, setData] = useState(null);
    const [error, setError] = useState(null);
    const [actionError, setActionError] = useState(null);
    const [expanded, setExpanded] = useState(null);
    const [allTags, setAllTags] = useState([]);
    const [effectiveModules, setEffectiveModules] = useState([]);
    const [allModules, setAllModules] = useState([]);
    const [selectedTag, setSelectedTag] = useState('');
    const [selectedModule, setSelectedModule] = useState('');
    const [isAdding, setIsAdding] = useState(false);
    const [editingName, setEditingName] = useState(false);
    const [nameInput, setNameInput] = useState('');
    const nameRef = useRef(null);

    const loadData = () => {
        Promise.all([
            fetchJSON(`/api/agents/${id}`).then(setData),
            fetchJSON('/api/tags').then(d => setAllTags(d.tags || [])),
            fetchJSON(`/api/agents/${id}/modules`).then(d => setEffectiveModules(d.modules || [])),
            fetchJSON('/api/modules').then(d => setAllModules(d.modules || []))
        ]).catch(e => setError(e.message));
    };

    useEffect(() => {
        loadData();
        const intervalId = setInterval(loadData, 10000);
        return () => clearInterval(intervalId);
    }, [id]);

    // Auto-expand error modules on first load
    useEffect(() => {
        if (data?.module_results && expanded === null) {
            const initial = {};
            data.module_results.forEach(mr => {
                if (mr.status === 'error') initial[mr.module_name] = true;
            });
            setExpanded(initial);
        }
    }, [data]);

    // Focus name input when editing starts
    useEffect(() => {
        if (editingName && nameRef.current) {
            nameRef.current.focus();
            nameRef.current.select();
        }
    }, [editingName]);

    const toggleExpand = (name) => {
        setExpanded(prev => ({ ...prev, [name]: !prev[name] }));
    };

    const handleAddTag = async (e) => {
        e.preventDefault();
        if (!selectedTag) return;
        setIsAdding(true);
        try {
            // Fetch current state to avoid races with polling
            const current = await fetchJSON(`/api/agents/${id}`);
            const agentTags = current.tags.map(t => t.id).concat([parseInt(selectedTag)]);
            await putJSON(`/api/agents/${id}/tags`, { tag_ids: agentTags });
            setSelectedTag('');
            const [updatedData, modules] = await Promise.all([
                fetchJSON(`/api/agents/${id}`),
                fetchJSON(`/api/agents/${id}/modules`)
            ]);
            setData(updatedData);
            setEffectiveModules(modules.modules || []);
        } catch (e) {
            setActionError(e.message);
        } finally {
            setIsAdding(false);
        }
    };

    const handleRemoveTag = async (tagId) => {
        try {
            // Fetch current state to avoid races with polling
            const current = await fetchJSON(`/api/agents/${id}`);
            const agentTags = current.tags.filter(t => t.id !== tagId).map(t => t.id);
            await putJSON(`/api/agents/${id}/tags`, { tag_ids: agentTags });
            const [updatedData, modules] = await Promise.all([
                fetchJSON(`/api/agents/${id}`),
                fetchJSON(`/api/agents/${id}/modules`)
            ]);
            setData(updatedData);
            setEffectiveModules(modules.modules || []);
        } catch (e) {
            setActionError(e.message);
        }
    };

    const handleAddModule = async (e) => {
        e.preventDefault();
        if (!selectedModule) return;
        setIsAdding(true);
        try {
            await postJSON('/api/assignments', { module_name: selectedModule, agent_id: id });
            setSelectedModule('');
            const modules = await fetchJSON(`/api/agents/${id}/modules`);
            setEffectiveModules(modules.modules || []);
        } catch (e) {
            setActionError(e.message);
        } finally {
            setIsAdding(false);
        }
    };

    const handleRemoveModule = async (assignmentId) => {
        if (!confirm('Remove this module assignment?')) return;
        try {
            await deleteJSON(`/api/assignments/${assignmentId}`);
            const modules = await fetchJSON(`/api/agents/${id}/modules`);
            setEffectiveModules(modules.modules || []);
        } catch (e) {
            setActionError(e.message);
        }
    };

    const startEditName = () => {
        setNameInput(data?.agent?.display_name || '');
        setEditingName(true);
    };

    const saveName = async () => {
        try {
            const value = nameInput.trim();
            await putJSON(`/api/agents/${id}/name`, { display_name: value || null });
            const updatedData = await fetchJSON(`/api/agents/${id}`);
            setData(updatedData);
            setEditingName(false);
        } catch (e) {
            setActionError(e.message);
        }
    };

    const cancelEditName = () => setEditingName(false);

    const handleNameKeyDown = (e) => {
        if (e.key === 'Enter') saveName();
        if (e.key === 'Escape') cancelEditName();
    };

    if (!data && error) return html`<div class="container"><a href="#/" class="back-link">← Dashboard</a><${ErrorState} error=${error} onRetry=${loadData} /></div>`;
    if (!data) return html`<div class="container"><a href="#/" class="back-link">← Dashboard</a><${LoadingState} /></div>`;

    const { agent, tags, module_results } = data;
    const assignedTagIds = new Set(tags.map(t => t.id));
    const availableTags = allTags.filter(t => !assignedTagIds.has(t.id));
    const time = formatTime(agent.last_seen_at);

    const renderModuleSource = (m) => {
        if (m.source === 'direct') return 'Direct';
        if (m.tag_id) return html`<a href="#/tags/${m.tag_id}">${m.source}</a>`;
        return m.source;
    };

    return html`
        <div class="container fade-in">
            <a href="#/" class="back-link">← Dashboard</a>
            ${actionError && html`<${ErrorBanner} message=${actionError} onDismiss=${() => setActionError(null)} />`}

            ${editingName ? html`
                <div class="inline-edit">
                    <input ref=${nameRef} type="text" value=${nameInput}
                        onInput=${e => setNameInput(e.target.value)}
                        onKeyDown=${handleNameKeyDown}
                        placeholder="Display name (empty to reset)" />
                    <button class="btn btn--primary btn--sm" onClick=${saveName}>Save</button>
                    <button class="btn btn--secondary btn--sm" onClick=${cancelEditName}>Cancel</button>
                </div>
            ` : html`
                <h2 class="page-title editable" onClick=${startEditName} title="Click to edit name">
                    ${agent.display_name || agent.hostname}
                </h2>
            `}

            <div class="info-grid" style="margin-top: 0.75rem;">
                <div class="info-item">
                    <div class="info-label">UUID</div>
                    <div class="info-value"><code>${agent.id}</code></div>
                </div>
                <div class="info-item">
                    <div class="info-label">Remote IP</div>
                    <div class="info-value">${agent.remote_ip}</div>
                </div>
                <div class="info-item">
                    <div class="info-label">OS / Arch</div>
                    <div class="info-value">${agent.os} / ${agent.arch}</div>
                </div>
                <div class="info-item">
                    <div class="info-label">Distro</div>
                    <div class="info-value">${agent.distro}</div>
                </div>
                <div class="info-item">
                    <div class="info-label">Last Seen</div>
                    <div class="info-value">
                        <span class="hint-hover" title=${time.utc}>
                            ${formatRelativeTime(agent.last_seen_at)}
                        </span>
                    </div>
                </div>
            </div>

            <!-- Tags Section -->
            <div class="section">
                <div class="section-title">Tags</div>
                ${tags.length > 0 ? html`
                    <div class="tag-pills">
                        ${tags.map(t => html`
                            <span class="tag-pill">
                                ${t.name}
                                <button class="tag-pill-remove" onClick=${() => handleRemoveTag(t.id)} title="Remove tag">✕</button>
                            </span>
                        `)}
                    </div>
                ` : html`<p style="color: var(--text-tertiary); font-size: 0.875rem; margin-bottom: 0.5rem;">No tags assigned.</p>`}

                <form class="form-row" onSubmit=${handleAddTag}>
                    <select value=${selectedTag} onInput=${e => setSelectedTag(e.target.value)}
                        disabled=${isAdding || availableTags.length === 0}>
                        <option value="">${availableTags.length === 0 ? 'All tags assigned' : 'Add a tag...'}</option>
                        ${availableTags.map(t => html`<option value=${t.id}>${t.name}</option>`)}
                    </select>
                    <button class="btn btn--primary" type="submit" disabled=${isAdding || !selectedTag}>
                        ${isAdding ? 'Adding...' : 'Add'}
                    </button>
                </form>
            </div>

            <!-- Effective Modules Section -->
            <div class="section">
                <div class="section-title">Effective Modules</div>
                ${effectiveModules.length === 0 && html`<p style="color: var(--text-tertiary); font-size: 0.875rem;">No modules assigned.</p>`}
                ${effectiveModules.length > 0 && html`
                    <div class="item-list">
                        ${effectiveModules.map(m => html`
                            <div class="eff-module-item">
                                <div class="eff-module-info">
                                    <span class="module-name">${m.name}</span>
                                    <span class="module-source ${m.source === 'direct' ? 'module-source--direct' : 'module-source--tag'}">
                                        ${renderModuleSource(m)}
                                    </span>
                                </div>
                                ${m.source === 'direct' && html`
                                    <button onClick=${() => handleRemoveModule(m.assignment_id)} class="btn btn--icon btn--sm" title="Remove assignment">✕</button>
                                `}
                            </div>
                        `)}
                    </div>
                `}

                <form class="form-row" onSubmit=${handleAddModule}>
                    <select value=${selectedModule} onInput=${e => setSelectedModule(e.target.value)} disabled=${isAdding}>
                        <option value="">Assign a module...</option>
                        ${allModules.filter(m => !m.error).map(m => html`<option value=${m.filename}>${m.name} (${m.filename})</option>`)}
                    </select>
                    <button class="btn btn--primary" type="submit" disabled=${isAdding || !selectedModule}>
                        ${isAdding ? 'Assigning...' : 'Assign'}
                    </button>
                </form>
            </div>

            <!-- Module Results Section -->
            <div class="section">
                <div class="section-title">Module Results</div>
                ${module_results.length === 0 && html`<p style="color: var(--text-tertiary); font-size: 0.875rem;">No results yet.</p>`}
                ${module_results.map(mr => html`
                    <div class="module-result">
                        <div class="module-result-header" onClick=${() => toggleExpand(mr.module_name)}>
                            <span class="module-result-name">
                                <span class="expand-icon">${expanded?.[mr.module_name] ? '▾' : '▸'}</span>
                                ${mr.module_name}
                            </span>
                            <${Badge} status=${mr.status} />
                        </div>
                        ${expanded?.[mr.module_name] && html`
                            <div class="module-output">
                                ${mr.stdout && html`
                                    <div class="module-output-section">
                                        <div class="module-output-label">stdout</div>
                                        <pre>${mr.stdout}</pre>
                                    </div>
                                `}
                                ${mr.stderr && html`
                                    <div class="module-output-section">
                                        <div class="module-output-label">stderr</div>
                                        <pre>${mr.stderr}</pre>
                                    </div>
                                `}
                                ${!mr.stdout && !mr.stderr && html`
                                    <div class="module-output-section">
                                        <pre style="color: var(--text-tertiary);">No output.</pre>
                                    </div>
                                `}
                            </div>
                        `}
                    </div>
                `)}
            </div>
        </div>
    `;
}

// === Tags ===

function Tags() {
    const [tags, setTags] = useState(null);
    const [error, setError] = useState(null);
    const [actionError, setActionError] = useState(null);
    const [newTagName, setNewTagName] = useState('');
    const [isCreating, setIsCreating] = useState(false);

    const loadTags = () => {
        fetchJSON('/api/tags')
            .then(d => { setTags(d.tags || []); setError(null); })
            .catch(e => setError(e.message));
    };

    useEffect(() => { loadTags(); }, []);

    const handleCreate = async (e) => {
        e.preventDefault();
        if (!newTagName.trim()) return;
        setIsCreating(true);
        try {
            await postJSON('/api/tags', { name: newTagName.trim() });
            setNewTagName('');
            loadTags();
        } catch (e) {
            setActionError(e.message);
        } finally {
            setIsCreating(false);
        }
    };

    const handleDelete = async (id) => {
        if (!confirm('Delete this tag? This will remove it from all agents.')) return;
        try {
            await deleteJSON(`/api/tags/${id}`);
            loadTags();
        } catch (e) {
            setActionError(e.message);
        }
    };

    if (!tags && error) return html`<div class="container"><a href="#/" class="back-link">← Dashboard</a><${ErrorState} error=${error} onRetry=${loadTags} /></div>`;
    if (!tags) return html`<div class="container"><a href="#/" class="back-link">← Dashboard</a><${LoadingState} /></div>`;

    return html`
        <div class="container fade-in">
            <a href="#/" class="back-link">← Dashboard</a>
            ${actionError && html`<${ErrorBanner} message=${actionError} onDismiss=${() => setActionError(null)} />`}
            <h2 class="page-title">Tags</h2>

            <form class="form-row" onSubmit=${handleCreate} style="margin-top: 0.75rem;">
                <input type="text" placeholder="New tag name" value=${newTagName}
                    autocapitalize="none" autocorrect="off" spellcheck="false"
                    onInput=${e => setNewTagName(e.target.value)} disabled=${isCreating} />
                <button class="btn btn--primary" type="submit" disabled=${isCreating || !newTagName.trim()}>
                    ${isCreating ? 'Creating...' : 'Create Tag'}
                </button>
            </form>

            <div class="section">
                ${tags.length === 0 && html`<${EmptyState}><p>No tags created yet.</p><//>`}
                ${tags.length > 0 && html`
                    <div class="item-list">
                        ${tags.map(tag => html`
                            <div class="list-item">
                                <a href="#/tags/${tag.id}">${tag.name}</a>
                                <button onClick=${() => handleDelete(tag.id)} class="btn btn--danger btn--sm">Delete</button>
                            </div>
                        `)}
                    </div>
                `}
            </div>
        </div>
    `;
}

// === Tag Detail ===

function TagDetail({ id }) {
    const [tag, setTag] = useState(null);
    const [assignments, setAssignments] = useState([]);
    const [modules, setModules] = useState([]);
    const [error, setError] = useState(null);
    const [actionError, setActionError] = useState(null);
    const [selectedModule, setSelectedModule] = useState('');
    const [isAdding, setIsAdding] = useState(false);

    const loadData = () => {
        Promise.all([
            fetchJSON('/api/tags').then(d => {
                const t = (d.tags || []).find(x => x.id === parseInt(id));
                setTag(t || null);
            }),
            fetchJSON('/api/assignments').then(d => {
                setAssignments((d.assignments || []).filter(a => a.tag_id === parseInt(id)));
            }),
            fetchJSON('/api/modules').then(d => setModules(d.modules || []))
        ]).catch(e => setError(e.message));
    };

    useEffect(() => { loadData(); }, [id]);

    const handleAddModule = async (e) => {
        e.preventDefault();
        if (!selectedModule) return;
        setIsAdding(true);
        try {
            await postJSON('/api/assignments', { module_name: selectedModule, tag_id: parseInt(id) });
            setSelectedModule('');
            const d = await fetchJSON('/api/assignments');
            setAssignments((d.assignments || []).filter(a => a.tag_id === parseInt(id)));
        } catch (e) {
            setActionError(e.message);
        } finally {
            setIsAdding(false);
        }
    };

    const handleRemoveModule = async (assignmentId) => {
        if (!confirm('Remove this module assignment?')) return;
        try {
            await deleteJSON(`/api/assignments/${assignmentId}`);
            setAssignments(assignments.filter(a => a.id !== assignmentId));
        } catch (e) {
            setActionError(e.message);
        }
    };

    if (!tag && error) return html`<div class="container"><a href="#/tags" class="back-link">← Tags</a><${ErrorState} error=${error} onRetry=${loadData} /></div>`;
    if (!tag) return html`<div class="container"><a href="#/tags" class="back-link">← Tags</a><${LoadingState} /></div>`;

    return html`
        <div class="container fade-in">
            <a href="#/tags" class="back-link">← Tags</a>
            ${actionError && html`<${ErrorBanner} message=${actionError} onDismiss=${() => setActionError(null)} />`}
            <h2 class="page-title">${tag.name}</h2>

            <div class="section">
                <div class="section-title">Modules</div>
                ${assignments.length === 0 && html`<p style="color: var(--text-tertiary); font-size: 0.875rem;">No modules assigned to this tag.</p>`}
                ${assignments.length > 0 && html`
                    <div class="item-list">
                        ${assignments.map(a => html`
                            <div class="list-item">
                                <span class="module-name">${a.module_name}</span>
                                <button onClick=${() => handleRemoveModule(a.id)} class="btn btn--danger btn--sm">Remove</button>
                            </div>
                        `)}
                    </div>
                `}

                <form class="form-row" onSubmit=${handleAddModule}>
                    <select value=${selectedModule} onInput=${e => setSelectedModule(e.target.value)} disabled=${isAdding}>
                        <option value="">Add a module...</option>
                        ${modules.filter(m => !m.error).map(m => html`<option value=${m.filename}>${m.name} (${m.filename})</option>`)}
                    </select>
                    <button class="btn btn--primary" type="submit" disabled=${isAdding || !selectedModule}>
                        ${isAdding ? 'Adding...' : 'Add'}
                    </button>
                </form>
            </div>
        </div>
    `;
}

// === Modules List ===

function ModulesList() {
    const [modules, setModules] = useState(null);
    const [error, setError] = useState(null);

    const load = () => {
        fetchJSON('/api/modules')
            .then(d => { setModules(d.modules || []); setError(null); })
            .catch(e => setError(e.message));
    };

    useEffect(() => { load(); }, []);

    if (!modules && error) return html`<div class="container"><a href="#/" class="back-link">← Dashboard</a><${ErrorState} error=${error} onRetry=${load} /></div>`;
    if (!modules) return html`<div class="container"><a href="#/" class="back-link">← Dashboard</a><${LoadingState} /></div>`;

    const errorCount = modules.filter(m => m.error).length;

    return html`
        <div class="container fade-in">
            <a href="#/" class="back-link">← Dashboard</a>
            <h2 class="page-title">Modules</h2>

            ${errorCount > 0 && html`
                <div class="alert alert--error">
                    ${errorCount} module${errorCount !== 1 ? 's' : ''} failed to load
                </div>
            `}

            ${modules.length === 0 && html`
                <${EmptyState}>
                    <p>No modules loaded.</p>
                    <p>Place scripts in the modules directory to get started.</p>
                <//>
            `}

            ${modules.map(m => m.error ? html`
                <div class="module-item module-item--error">
                    <div class="module-item-header">
                        <h4>${m.filename}</h4>
                        <${Badge} status="error" />
                    </div>
                    <p class="module-error-message">${m.error}</p>
                </div>
            ` : html`
                <div class="module-item">
                    <div class="module-item-header">
                        <h4>${m.name}</h4>
                        <span class="module-filename">${m.filename}</span>
                    </div>
                    ${m.description && html`<p class="module-description">${m.description}</p>`}
                </div>
            `)}
        </div>
    `;
}

// === Help ===

function Help() {
    const [config, setConfig] = useState(null);

    useEffect(() => {
        fetchJSON('/api/config').then(setConfig).catch(() => {});
    }, []);

    const pollInterval = config ? formatDuration(config.poll_interval_seconds) : '...';
    const staleThreshold = config ? formatDuration(config.stale_threshold_seconds) : '...';
    const modulesDir = config ? config.modules_dir : '...';

    return html`
        <div class="container fade-in">
            <a href="#/" class="back-link">← Dashboard</a>
            <h2 class="page-title">Help</h2>

            <div class="section">
                <div class="section-title">Overview</div>
                <div class="help-content">
                    <p>
                        Anchor is a configuration management system. A central <strong>server</strong> distributes
                        shell-script <strong>modules</strong> to lightweight <strong>agents</strong> running on
                        your hosts. Agents check in every <strong>${pollInterval}</strong>, download their
                        assigned modules, run them, and report results back.
                    </p>
                </div>
            </div>

            <div class="section">
                <div class="section-title">Agents</div>
                <div class="help-content">
                    <p>
                        An agent is a host running <code>anchor agent</code>. On each check-in it reports basic
                        system information (hostname, OS, architecture, distro) and receives its assigned modules.
                    </p>
                    <dl class="help-dl">
                        <dt>Display name</dt>
                        <dd>Click an agent's name on the detail page to set a friendly display name. Leave it
                        blank to revert to the hostname.</dd>
                        <dt>UUID</dt>
                        <dd>Each agent has a stable UUID generated on first run. The last 8 characters are shown
                        on the dashboard for quick identification.</dd>
                    </dl>
                </div>
            </div>

            <div class="section">
                <div class="section-title">Health Status</div>
                <div class="help-content">
                    <p>The dashboard groups agents by health. Health is computed by the server on each request:</p>
                    <dl class="help-dl">
                        <dt><span class="badge badge--error">error</span> Unhealthy</dt>
                        <dd>At least one module reported an error on its most recent run. Check the agent's
                        module results for details.</dd>
                        <dt><span class="badge badge--changed">stale</span> Stale</dt>
                        <dd>The agent has not checked in within <strong>${staleThreshold}</strong> (twice the
                        poll interval of ${pollInterval}). This usually means the agent process has stopped,
                        the host is down, or there is a network issue.</dd>
                        <dt><span class="badge badge--ok">ok</span> Healthy</dt>
                        <dd>The agent checked in recently and no modules have errors.</dd>
                    </dl>
                    <p>Unhealthy takes priority: an agent with errors is always shown as unhealthy even if it
                    is also stale.</p>
                </div>
            </div>

            <div class="section">
                <div class="section-title">Modules</div>
                <div class="help-content">
                    <p>
                        A module is a shell script placed in the server's modules directory
                        (<code>${modulesDir}</code>). Each module accepts a command argument:
                    </p>
                    <dl class="help-dl">
                        <dt><code>metadata</code></dt>
                        <dd>Outputs JSON with <code>name</code> and <code>description</code> fields.
                        These are used for display in the UI.</dd>
                        <dt><code>apply</code></dt>
                        <dd>Applies the configuration. Should be idempotent (safe to run repeatedly).</dd>
                    </dl>
                    <p>Exit codes for <code>apply</code>:</p>
                    <dl class="help-dl">
                        <dt><code>0</code> — ok</dt>
                        <dd>No changes were needed; configuration is already correct.</dd>
                        <dt><code>80</code> — changed</dt>
                        <dd>Changes were applied successfully.</dd>
                        <dt>Anything else — error</dt>
                        <dd>Something went wrong. stdout and stderr are captured and shown in the UI.</dd>
                    </dl>
                    <p>
                        The module <strong>filename</strong> (e.g., <code>00_base</code>) is its identifier.
                        Modules are sorted and executed in filename order, so numeric prefixes control
                        execution order.
                    </p>
                </div>
            </div>

            <div class="section">
                <div class="section-title">Artifacts</div>
                <div class="help-content">
                    <p>
                        A module can have an associated <code>${'<filename>.d/'}</code> directory containing
                        supporting files (config templates, binaries, etc.). These are distributed to agents
                        automatically and made available as a <code>files/</code> subdirectory when the module
                        runs.
                    </p>
                    <p>
                        File permissions are preserved through the pipeline. Scripts access artifacts via
                        relative paths (e.g., <code>./files/nginx.conf</code>).
                    </p>
                </div>
            </div>

            <div class="section">
                <div class="section-title">Tags</div>
                <div class="help-content">
                    <p>
                        Tags let you group agents and assign modules to many agents at once. When you assign a
                        module to a tag, every agent with that tag receives the module.
                    </p>
                    <p>
                        On the agent detail page, the <strong>Effective Modules</strong> section shows all
                        modules an agent will receive, with the source of each assignment:
                    </p>
                    <dl class="help-dl">
                        <dt>Direct</dt>
                        <dd>Assigned specifically to this agent. Can be removed from the agent's page.</dd>
                        <dt>${'tag:<name>'}</dt>
                        <dd>Inherited from a tag. Remove the tag from the agent or the module from the tag
                        to unassign.</dd>
                    </dl>
                    <p>
                        If a module is assigned both directly and via a tag, only the direct assignment is
                        shown (the module runs once, not twice).
                    </p>
                </div>
            </div>

            <div class="section">
                <div class="section-title">Module Signing</div>
                <div class="help-content">
                    <p>
                        Modules can be signed with ed25519 keys for integrity verification.
                    </p>
                    <dl class="help-dl">
                        <dt>Generating keys</dt>
                        <dd><code>anchor keygen -o mykey</code> creates <code>mykey.key</code> (private) and
                        <code>mykey.pub</code> (public) in Anchor PEM format.</dd>
                        <dt>Signing modules</dt>
                        <dd><code>anchor sign -k mykey.key module_script</code> creates a
                        <code>module_script.sig</code> sidecar file.</dd>
                        <dt>Verifying on agents</dt>
                        <dd>Pass <code>--verify-key</code> or <code>--verify-key-url</code> when running the
                        agent. Agents will reject modules with missing or invalid signatures.</dd>
                    </dl>
                    <p>
                        In addition to Anchor PEM keys, standard OpenSSH ed25519 keys are also accepted.
                        You can sign with an SSH private key and verify with the corresponding public key.
                        Only ed25519 keys are supported; other key types are rejected.
                    </p>
                    <p>
                        The <code>--verify-key-url</code> flag accepts any URL that returns keys in SSH
                        <code>authorized_keys</code> format. GitHub publishes users' public keys at
                        <code>https://github.com/USERNAME.keys</code>, so you can use this to trust modules
                        signed by a GitHub user's ed25519 key:
                    </p>
                    <p>
                        <code>anchor agent --verify-key-url https://github.com/USERNAME.keys ...</code>
                    </p>
                    <p>
                        URL-fetched keys are cached locally for offline fallback. Non-ed25519 keys in the
                        response are silently ignored.
                    </p>
                    <p>
                        If a <code>.sig</code> file is malformed, the associated module is treated as a
                        load error.
                    </p>
                </div>
            </div>

            <div class="section">
                <div class="section-title">Connection Indicator</div>
                <div class="help-content">
                    <p>
                        The small dot in the header shows the UI's connection to the server:
                    </p>
                    <dl class="help-dl">
                        <dt>Green</dt>
                        <dd>Connected. Data is current.</dd>
                        <dt>Amber</dt>
                        <dd>Recent request failed. Data may be slightly stale. The UI will retry automatically.</dd>
                        <dt>Red</dt>
                        <dd>Multiple consecutive failures. The server may be down or unreachable. A banner
                        appears below the header showing when data was last updated.</dd>
                    </dl>
                </div>
            </div>
        </div>
    `;
}

// === App Shell ===

function Header({ route, connStatus }) {
    const isActive = (path) => {
        if (path === '/') return route === '/' || route === '' || route.startsWith('/agents/');
        return route.startsWith(path);
    };

    return html`
        <header class="app-header">
            <div class="header-inner">
                <a href="#/" class="header-logo">Anchor</a>
                <nav class="header-nav">
                    <a href="#/" class="nav-link ${isActive('/') && !isActive('/tags') && !isActive('/modules') && !isActive('/help') ? 'nav-link--active' : ''}">Dashboard</a>
                    <a href="#/tags" class="nav-link ${isActive('/tags') ? 'nav-link--active' : ''}">Tags</a>
                    <a href="#/modules" class="nav-link ${isActive('/modules') ? 'nav-link--active' : ''}">Modules</a>
                    <a href="#/help" class="nav-link ${isActive('/help') ? 'nav-link--active' : ''}">Help</a>
                </nav>
                <div class="header-actions">
                    <${ConnectionDot} status=${connStatus} />
                    <${ThemeToggle} />
                </div>
            </div>
        </header>
    `;
}

function App() {
    const route = useHashRoute();
    const connStatus = useConnectionStatus();

    let page;
    if (route === '/' || route === '') {
        page = html`<${Dashboard} />`;
    } else if (route === '/tags') {
        page = html`<${Tags} />`;
    } else if (route.startsWith('/tags/')) {
        const id = route.slice('/tags/'.length);
        page = html`<${TagDetail} id=${id} />`;
    } else if (route === '/modules') {
        page = html`<${ModulesList} />`;
    } else if (route === '/help') {
        page = html`<${Help} />`;
    } else if (route.startsWith('/agents/')) {
        const id = route.slice('/agents/'.length);
        page = html`<${AgentDetail} id=${id} />`;
    } else {
        page = html`
            <div class="container">
                <${EmptyState}>
                    <p>Page not found.</p>
                    <p><a href="#/">Go to Dashboard</a></p>
                <//>
            </div>
        `;
    }

    return html`
        <${Header} route=${route} connStatus=${connStatus} />
        ${connStatus !== 'connected' && html`
            <div class="stale-banner">
                Connection issues — data may be stale. Last update: ${formatTimeAgo(connection.lastSuccess)}
            </div>
        `}
        ${page}
    `;
}

render(html`<${App} />`, document.getElementById('app'));
