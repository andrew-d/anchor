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
// Placeholder components to be implemented in subsequent tasks

function Dashboard() {
    return html`<div class="container"><p>Dashboard placeholder</p></div>`;
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
