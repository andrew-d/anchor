import { h, render } from 'preact';
import { useState, useEffect } from 'preact/hooks';
import htm from 'htm';

const html = htm.bind(h);

// --- Router ---

function useHashRoute() {
    const [route, setRoute] = useState(window.location.hash.slice(1) || '/');

    useEffect(() => {
        const onHashChange = () => setRoute(window.location.hash.slice(1) || '/');
        window.addEventListener('hashchange', onHashChange);
        return () => window.removeEventListener('hashchange', onHashChange);
    }, []);

    return route;
}

// --- API helpers ---

async function fetchJSON(url) {
    const res = await fetch(url);
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
    return res.json();
}

async function postJSON(url, body) {
    const res = await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body)
    });
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
    return res.json();
}

async function putJSON(url, body) {
    const res = await fetch(url, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body)
    });
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
    return res.json();
}

async function deleteJSON(url) {
    const res = await fetch(url, { method: 'DELETE' });
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
    return res.json();
}

// --- Helpers ---

function formatTime(unixSeconds) {
    const d = new Date(unixSeconds * 1000);
    return { local: d.toLocaleString(), utc: d.toUTCString() };
}

// --- Components (Dashboard, AgentDetail) ---

function Dashboard() {
    const [data, setData] = useState(null);
    const [error, setError] = useState(null);

    useEffect(() => {
        const load = () => fetchJSON('/api/agents').then(setData).catch(e => setError(e.message));
        load();
        const id = setInterval(load, 10000);
        return () => clearInterval(id);
    }, []);

    if (error) return html`<div class="container"><p>Error: ${error}</p></div>`;
    if (!data) return html`<div class="container"><p>Loading...</p></div>`;

    const groups = [
        { key: 'unhealthy', label: 'Unhealthy', agents: data.agents.filter(a => a.health === 'unhealthy') },
        { key: 'stale', label: 'Stale', agents: data.agents.filter(a => a.health === 'stale') },
        { key: 'healthy', label: 'Healthy', agents: data.agents.filter(a => a.health === 'healthy') },
    ];

    return html`
        <div class="container">
            ${groups.filter(g => g.agents.length > 0).map(g => html`
                <div class="status-group group-${g.key}">
                    <h2>${g.label} (${g.agents.length})</h2>
                    ${g.agents.map(a => html`
                        <div class="agent-card">
                            <div>
                                <a href="#/agents/${a.id}">${a.display_name || a.hostname}</a>
                                ${' '}
                                ${a.tags.map(t => html`<span class="tag">${t.name}</span>`)}
                                <div class="agent-subtitle">${a.remote_ip} · ${a.id.slice(-4)}</div>
                            </div>
                            <div>
                                <span>${a.module_count} modules</span>
                                ${a.error_count > 0 && html`<span class="badge badge-error">${a.error_count} errors</span>`}
                            </div>
                        </div>
                    `)}
                </div>
            `)}
        </div>
    `;
}

function AgentDetail({ id }) {
    const [data, setData] = useState(null);
    const [error, setError] = useState(null);
    const [expanded, setExpanded] = useState({});
    const [allTags, setAllTags] = useState([]);
    const [effectiveModules, setEffectiveModules] = useState([]);
    const [allModules, setAllModules] = useState([]);
    const [selectedTag, setSelectedTag] = useState('');
    const [selectedModule, setSelectedModule] = useState('');
    const [isAdding, setIsAdding] = useState(false);
    const [editingName, setEditingName] = useState(false);
    const [nameInput, setNameInput] = useState('');

    const loadData = () => {
        Promise.all([
            fetchJSON(`/api/agents/${id}`).then(setData),
            fetchJSON('/api/tags').then(data => setAllTags(data.tags || [])),
            fetchJSON(`/api/agents/${id}/modules`).then(data => setEffectiveModules(data.modules || [])),
            fetchJSON('/api/modules').then(data => setAllModules(data.modules || []))
        ]).catch(e => setError(e.message));
    };

    useEffect(() => {
        loadData();
        const intervalId = setInterval(loadData, 10000);
        return () => clearInterval(intervalId);
    }, [id]);

    const toggleExpand = (name) => {
        setExpanded(prev => ({ ...prev, [name]: !prev[name] }));
    };

    const handleAddTag = async (e) => {
        e.preventDefault();
        if (!selectedTag) return;
        setIsAdding(true);
        try {
            const agentTags = data.tags.map(t => t.id).concat([parseInt(selectedTag)]);
            await putJSON(`/api/agents/${id}/tags`, { tag_ids: agentTags });
            setSelectedTag('');
            const updatedData = await fetchJSON(`/api/agents/${id}`);
            setData(updatedData);
            const modules = await fetchJSON(`/api/agents/${id}/modules`);
            setEffectiveModules(modules.modules || []);
        } catch (e) {
            setError(e.message);
        } finally {
            setIsAdding(false);
        }
    };

    const handleRemoveTag = async (tagId) => {
        if (!confirm('Remove this tag?')) return;
        try {
            const agentTags = data.tags.filter(t => t.id !== tagId).map(t => t.id);
            await putJSON(`/api/agents/${id}/tags`, { tag_ids: agentTags });
            const updatedData = await fetchJSON(`/api/agents/${id}`);
            setData(updatedData);
            const modules = await fetchJSON(`/api/agents/${id}/modules`);
            setEffectiveModules(modules.modules || []);
        } catch (e) {
            setError(e.message);
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
            setError(e.message);
        } finally {
            setIsAdding(false);
        }
    };

    const handleRemoveModule = async (assignmentId) => {
        if (!confirm('Remove this assignment?')) return;
        try {
            await deleteJSON(`/api/assignments/${assignmentId}`);
            const modules = await fetchJSON(`/api/agents/${id}/modules`);
            setEffectiveModules(modules.modules || []);
        } catch (e) {
            setError(e.message);
        }
    };

    const handleSaveName = async () => {
        try {
            const value = nameInput.trim();
            await putJSON(`/api/agents/${id}/name`, { display_name: value || null });
            const updatedData = await fetchJSON(`/api/agents/${id}`);
            setData(updatedData);
            setEditingName(false);
        } catch (e) {
            setError(e.message);
        }
    };

    if (error) return html`<div class="container"><p>Error: ${error}</p></div>`;
    if (!data) return html`<div class="container"><p>Loading...</p></div>`;

    const { agent, tags, module_results } = data;

    // Tags that are already assigned to this agent
    const assignedTagIds = new Set(tags.map(t => t.id));
    const availableTags = allTags.filter(t => !assignedTagIds.has(t.id));

    return html`
        <div class="container">
            <p><a href="#/">← Dashboard</a></p>
            <h2>${agent.display_name || agent.hostname}</h2>
            <div class="agent-name-edit">
                ${editingName ? html`
                    <input type="text" value=${nameInput} onInput=${e => setNameInput(e.target.value)} placeholder="Display name (empty to reset)" />
                    <button onClick=${handleSaveName}>Save</button>
                    <button onClick=${() => setEditingName(false)} class="btn-cancel">Cancel</button>
                ` : html`
                    <button onClick=${() => { setNameInput(agent.display_name || ''); setEditingName(true); }}>Edit name</button>
                `}
            </div>

            <dl class="agent-info">
                <dt>UUID</dt><dd><code>${agent.id}</code></dd>
                <dt>Remote IP</dt><dd>${agent.remote_ip}</dd>
                <dt>OS / Arch</dt><dd>${agent.os} / ${agent.arch}</dd>
                <dt>Distro</dt><dd>${agent.distro}</dd>
                <dt>Last Seen</dt><dd><span class="hint-hover" title=${formatTime(agent.last_seen_at).utc}>${formatTime(agent.last_seen_at).local}</span></dd>
            </dl>

            <h3>Tags</h3>
            ${tags.length === 0 && html`<p>No tags assigned.</p>`}
            ${tags.length > 0 && html`
                <div class="tag-assignment-list">
                    ${tags.map(t => html`
                        <div class="tag-assignment-item">
                            <span>${t.name}</span>
                            <button onClick=${() => handleRemoveTag(t.id)} class="btn-delete">Remove</button>
                        </div>
                    `)}
                </div>
            `}

            ${availableTags.length > 0 && html`
                <div class="form-section">
                    <form onSubmit=${handleAddTag}>
                        <select value=${selectedTag} onInput=${e => setSelectedTag(e.target.value)} disabled=${isAdding}>
                            <option value="">Add a tag...</option>
                            ${availableTags.map(t => html`<option value=${t.id}>${t.name}</option>`)}
                        </select>
                        <button type="submit" disabled=${isAdding || !selectedTag}>${isAdding ? 'Adding...' : 'Add Tag'}</button>
                    </form>
                </div>
            `}

            <h3>Effective Module Set</h3>
            ${effectiveModules.length === 0 && html`<p>No modules assigned.</p>`}
            ${effectiveModules.length > 0 && html`
                <div class="effective-modules-list">
                    ${effectiveModules.map(m => html`
                        <div class="effective-module-item">
                            <div>
                                <span class="module-name">${m.name}</span>
                                <span class="module-source" style=${m.source === 'direct' ? 'background: #dbeafe; color: #1e40af;' : 'background: #f3e8ff; color: #6b21a8;'}>
                                    ${m.source === 'direct' ? 'Direct' : m.source}
                                </span>
                            </div>
                            ${m.source === 'direct' && html`
                                <button onClick=${() => handleRemoveModule(m.assignment_id)} class="btn-delete">Remove</button>
                            `}
                        </div>
                    `)}
                </div>
            `}

            <div class="form-section">
                <form onSubmit=${handleAddModule}>
                    <select value=${selectedModule} onInput=${e => setSelectedModule(e.target.value)} disabled=${isAdding}>
                        <option value="">Assign a module...</option>
                        ${allModules.map(m => html`<option value=${m.filename}>${m.name}</option>`)}
                    </select>
                    <button type="submit" disabled=${isAdding || !selectedModule}>${isAdding ? 'Adding...' : 'Assign Module'}</button>
                </form>
            </div>

            <h3>Module Results</h3>
            ${module_results.map(mr => html`
                <div class="module-result">
                    <div class="module-result-header" onClick=${() => toggleExpand(mr.module_name)}>
                        <span>${mr.module_name}</span>
                        <span class="badge badge-${mr.status}">${mr.status}</span>
                    </div>
                    ${expanded[mr.module_name] && html`
                        <div class="module-output">
                            ${mr.stdout && html`<div><strong>stdout:</strong><pre>${mr.stdout}</pre></div>`}
                            ${mr.stderr && html`<div><strong>stderr:</strong><pre>${mr.stderr}</pre></div>`}
                        </div>
                    `}
                </div>
            `)}
        </div>
    `;
}

// --- Tags Component ---

function Tags() {
    const [tags, setTags] = useState(null);
    const [error, setError] = useState(null);
    const [newTagName, setNewTagName] = useState('');
    const [isCreating, setIsCreating] = useState(false);

    const loadTags = () => {
        fetchJSON('/api/tags')
            .then(data => setTags(data.tags || []))
            .catch(e => setError(e.message));
    };

    useEffect(() => {
        loadTags();
    }, []);

    const handleCreate = async (e) => {
        e.preventDefault();
        if (!newTagName.trim()) return;
        setIsCreating(true);
        try {
            await postJSON('/api/tags', { name: newTagName });
            setNewTagName('');
            loadTags();
        } catch (e) {
            setError(e.message);
        } finally {
            setIsCreating(false);
        }
    };

    const handleDelete = async (id) => {
        if (!confirm('Delete this tag?')) return;
        try {
            await deleteJSON(`/api/tags/${id}`);
            loadTags();
        } catch (e) {
            setError(e.message);
        }
    };

    if (error) return html`<div class="container"><p>Error: ${error}</p></div>`;
    if (!tags) return html`<div class="container"><p>Loading...</p></div>`;

    return html`
        <div class="container">
            <p><a href="#/">← Dashboard</a></p>
            <h2>Tags</h2>

            <div class="form-section">
                <form onSubmit=${handleCreate}>
                    <input
                        type="text"
                        placeholder="New tag name"
                        value=${newTagName}
                        onInput=${e => setNewTagName(e.target.value)}
                        disabled=${isCreating}
                    />
                    <button type="submit" disabled=${isCreating}>${isCreating ? 'Creating...' : 'Create Tag'}</button>
                </form>
            </div>

            ${tags.length === 0 && html`<p>No tags yet.</p>`}
            ${tags.length > 0 && html`
                <div class="tag-list">
                    ${tags.map(tag => html`
                        <div class="tag-item">
                            <a href="#/tags/${tag.id}">${tag.name}</a>
                            <button onClick=${() => handleDelete(tag.id)} class="btn-delete">Delete</button>
                        </div>
                    `)}
                </div>
            `}
        </div>
    `;
}

function TagDetail({ id }) {
    const [tag, setTag] = useState(null);
    const [assignments, setAssignments] = useState([]);
    const [modules, setModules] = useState([]);
    const [error, setError] = useState(null);
    const [selectedModule, setSelectedModule] = useState('');
    const [isAdding, setIsAdding] = useState(false);

    useEffect(() => {
        Promise.all([
            fetchJSON('/api/tags').then(data => {
                const t = data.tags.find(x => x.id === parseInt(id));
                setTag(t);
            }),
            fetchJSON('/api/assignments').then(data => {
                setAssignments((data.assignments || []).filter(a => a.tag_id === parseInt(id)));
            }),
            fetchJSON('/api/modules').then(data => setModules(data.modules || []))
        ]).catch(e => setError(e.message));
    }, [id]);

    const handleAddModule = async (e) => {
        e.preventDefault();
        if (!selectedModule) return;
        setIsAdding(true);
        try {
            await postJSON('/api/assignments', { module_name: selectedModule, tag_id: parseInt(id) });
            setSelectedModule('');
            const data = await fetchJSON('/api/assignments');
            setAssignments((data.assignments || []).filter(a => a.tag_id === parseInt(id)));
        } catch (e) {
            setError(e.message);
        } finally {
            setIsAdding(false);
        }
    };

    const handleRemoveModule = async (assignmentId) => {
        if (!confirm('Remove this assignment?')) return;
        try {
            await deleteJSON(`/api/assignments/${assignmentId}`);
            setAssignments(assignments.filter(a => a.id !== assignmentId));
        } catch (e) {
            setError(e.message);
        }
    };

    if (error) return html`<div class="container"><p>Error: ${error}</p></div>`;
    if (!tag) return html`<div class="container"><p>Loading...</p></div>`;

    return html`
        <div class="container">
            <p><a href="#/tags">← Tags</a></p>
            <h2>${tag.name}</h2>

            <h3>Modules</h3>
            ${assignments.length === 0 && html`<p>No modules assigned to this tag.</p>`}
            ${assignments.length > 0 && html`
                <div class="assignment-list">
                    ${assignments.map(a => html`
                        <div class="assignment-item">
                            <span>${a.module_name}</span>
                            <button onClick=${() => handleRemoveModule(a.id)} class="btn-delete">Remove</button>
                        </div>
                    `)}
                </div>
            `}

            <div class="form-section">
                <form onSubmit=${handleAddModule}>
                    <select value=${selectedModule} onInput=${e => setSelectedModule(e.target.value)} disabled=${isAdding}>
                        <option value="">Select a module...</option>
                        ${modules.map(m => html`<option value=${m.filename}>${m.name}</option>`)}
                    </select>
                    <button type="submit" disabled=${isAdding || !selectedModule}>${isAdding ? 'Adding...' : 'Add Module'}</button>
                </form>
            </div>
        </div>
    `;
}

// --- Modules Component ---

function ModulesList() {
    const [modules, setModules] = useState(null);
    const [error, setError] = useState(null);

    useEffect(() => {
        fetchJSON('/api/modules')
            .then(data => setModules(data.modules || []))
            .catch(e => setError(e.message));
    }, []);

    if (error) return html`<div class="container"><p>Error: ${error}</p></div>`;
    if (!modules) return html`<div class="container"><p>Loading...</p></div>`;

    return html`
        <div class="container">
            <p><a href="#/">← Dashboard</a></p>
            <h2>Modules</h2>

            ${modules.length === 0 && html`<p>No modules loaded.</p>`}
            ${modules.length > 0 && html`
                <div class="module-list">
                    ${modules.map(m => html`
                        <div class="module-item">
                            <div class="module-item-header">
                                <h4>${m.name}</h4>
                                <span class="module-filename">${m.filename}</span>
                            </div>
                            <p class="module-description">${m.description}</p>
                        </div>
                    `)}
                </div>
            `}
        </div>
    `;
}

// --- App Shell ---

function Header() {
    return html`
        <header>
            <div style="max-width: 960px; margin: 0 auto; padding: 0 1rem; display: flex; justify-content: space-between; align-items: center;">
                <h1><a href="#/" style="color: white; text-decoration: none;">Anchor</a></h1>
                <nav style="display: flex; gap: 2rem;">
                    <a href="#/" style="color: white; text-decoration: none;">Dashboard</a>
                    <a href="#/tags" style="color: white; text-decoration: none;">Tags</a>
                    <a href="#/modules" style="color: white; text-decoration: none;">Modules</a>
                </nav>
            </div>
        </header>
    `;
}

function App() {
    const route = useHashRoute();

    // Route matching
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
    } else if (route.startsWith('/agents/')) {
        const id = route.slice('/agents/'.length);
        page = html`<${AgentDetail} id=${id} />`;
    } else {
        page = html`<${Dashboard} />`;
    }

    return html`
        <${Header} />
        ${page}
    `;
}

render(html`<${App} />`, document.getElementById('app'));
