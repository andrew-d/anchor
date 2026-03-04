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

// --- Components (Dashboard, AgentDetail) ---

function Dashboard() {
    const [data, setData] = useState(null);
    const [error, setError] = useState(null);

    useEffect(() => {
        fetchJSON('/api/agents')
            .then(setData)
            .catch(e => setError(e.message));
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
                                <a href="#/agents/${a.id}">${a.hostname}</a>
                                ${' '}
                                ${a.tags.map(t => html`<span class="tag">${t.name}</span>`)}
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
    return html`<div class="container"><p>Agent detail placeholder for ID: ${id}</p></div>`;
}

// --- App Shell ---

function App() {
    const route = useHashRoute();

    // Route matching
    if (route.startsWith('/agents/')) {
        const id = route.slice('/agents/'.length);
        return html`<${AgentDetail} id=${id} />`;
    }
    return html`<${Dashboard} />`;
}

render(html`<${App} />`, document.getElementById('app'));
