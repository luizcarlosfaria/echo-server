// Painel realtime do Echo Server.
// Recebe resumos via WebSocket (/_echo/ws) e busca o detalhe completo sob demanda (/_echo/api/requests/{id}).
(() => {
    'use strict';

    const root = document.getElementById('echo-dashboard');
    const base = root.dataset.base;                      // ex.: "/_echo/"
    const capacity = Number(root.dataset.capacity) || 1000;

    const el = {
        list: document.getElementById('list'),
        empty: document.getElementById('empty'),
        detail: document.getElementById('detail'),
        filter: document.getElementById('filter'),
        onlyProblems: document.getElementById('only-problems'),
        problemCount: document.getElementById('problem-count'),
        pause: document.getElementById('pause'),
        clear: document.getElementById('clear'),
        count: document.getElementById('count'),
        total: document.getElementById('total'),
        dot: document.getElementById('status-dot'),
        status: document.getElementById('status-text'),
    };

    const items = new Map();     // id -> resumo (dedupe entre snapshot e eventos)
    let paused = false;
    let pending = [];            // eventos recebidos enquanto pausado
    let selectedId = null;
    let reconnectDelay = 1000;

    const METHOD_COLORS = {
        GET: 'bg-blue-100 text-blue-800', POST: 'bg-green-100 text-green-800', PUT: 'bg-amber-100 text-amber-800',
        PATCH: 'bg-purple-100 text-purple-800', DELETE: 'bg-red-100 text-red-800',
    };

    // Severidade do problema -> [badge, borda lateral da linha]
    // "!" (important): o divide-gray-100 da <ul> pinta a borda dos filhos com seletor mais específico.
    const SEVERITY_STYLES = {
        error: ['bg-red-100 text-red-800', 'border-l-4 !border-l-red-500'],
        warning: ['bg-amber-100 text-amber-800', 'border-l-4 !border-l-amber-400'],
        info: ['bg-gray-100 text-gray-700', 'border-l-4 !border-l-gray-300'],
    };

    const hasProblems = item => (item.problems?.length ?? 0) > 0;

    // ---------- helpers de DOM (sempre textContent: o conteúdo vem de requests arbitrários) ----------
    function h(tag, className, text) {
        const node = document.createElement(tag);
        if (className) node.className = className;
        if (text !== undefined && text !== null) node.textContent = String(text);
        return node;
    }

    function formatBytes(n) {
        if (n < 1024) return `${n} B`;
        if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
        return `${(n / 1024 / 1024).toFixed(1)} MB`;
    }

    function formatTime(iso) {
        const d = new Date(iso);
        return d.toLocaleTimeString('pt-BR', { hour12: false }) + '.' + String(d.getMilliseconds()).padStart(3, '0');
    }

    // ---------- status de conexão ----------
    function setStatus(state) {
        const map = {
            connecting: ['bg-yellow-400', 'conectando…'],
            online: ['bg-green-400', paused ? 'pausado' : 'ao vivo'],
            offline: ['bg-red-500', 'desconectado — tentando reconectar'],
        };
        const [color, text] = map[state];
        el.dot.className = `inline-block h-3 w-3 rounded-full ${color}`;
        el.status.textContent = text;
    }

    // ---------- lista ----------
    function matchesFilter(item) {
        if (el.onlyProblems.checked && !hasProblems(item)) return false;
        const term = el.filter.value.trim().toLowerCase();
        if (!term) return true;
        return [item.method, item.path, item.queryString, item.remoteIp, item.contentType, item.host, item.bodyKind, String(item.id), ...(item.problems ?? [])]
            .some(v => v && String(v).toLowerCase().includes(term));
    }

    function renderRow(item) {
        const [badgeStyle, border] = SEVERITY_STYLES[item.problemSeverity] ?? ['', 'border-l-4 !border-l-transparent'];
        const li = h('li', `px-4 py-3 cursor-pointer hover:bg-gray-50 flex items-start gap-3 ${border}`);
        li.dataset.id = item.id;
        li.setAttribute('role', 'option');
        li.setAttribute('aria-selected', String(item.id === selectedId));
        if (item.id === selectedId) li.classList.add('bg-indigo-50');

        const badge = h('span', `shrink-0 w-16 text-center rounded px-2 py-0.5 text-xs font-semibold ${METHOD_COLORS[item.method] ?? 'bg-gray-100 text-gray-800'}`, item.method);
        const main = h('div', 'min-w-0 flex-1');
        main.append(h('div', 'text-sm font-medium text-gray-900 truncate', item.path + (item.queryString || '')));

        const meta = [`#${item.id}`, formatTime(item.timestamp)];
        if (item.bodyKind !== 'none') meta.push(`${item.bodyKind} · ${formatBytes(item.bodySize)}${item.bodyTruncated ? ' (truncado)' : ''}`);
        if (item.remoteIp) meta.push(item.remoteIp);
        main.append(h('div', 'text-xs text-gray-500 truncate', meta.join(' · ')));

        if (hasProblems(item)) {
            const tags = h('div', 'mt-1 flex flex-wrap gap-1');
            item.problems.forEach(code => tags.append(h('span', `rounded px-1.5 py-0.5 text-[11px] font-medium ${badgeStyle}`, `⚠ ${code}`)));
            main.append(tags);
        }

        li.append(badge, main);
        li.addEventListener('click', () => select(item.id));
        return li;
    }

    function renderList() {
        const sorted = [...items.values()].sort((a, b) => b.id - a.id).filter(matchesFilter);
        el.list.replaceChildren(...sorted.map(renderRow));
        el.empty.hidden = items.size > 0;
        el.count.textContent = items.size;
        el.problemCount.textContent = [...items.values()].filter(hasProblems).length;
    }

    function addItem(item, totalReceived) {
        items.set(item.id, item);
        // espelha o expurgo do servidor: mantém só os N mais recentes
        if (items.size > capacity) {
            const oldest = [...items.keys()].sort((a, b) => a - b).slice(0, items.size - capacity);
            oldest.forEach(id => items.delete(id));
        }
        if (totalReceived !== undefined) el.total.textContent = totalReceived;
    }

    // ---------- detalhe ----------
    function section(title, content) {
        const wrap = h('div', 'border-t border-gray-200');
        wrap.append(h('h3', 'px-6 py-2 bg-gray-50 text-xs font-medium uppercase tracking-wider text-gray-500', title), content);
        return wrap;
    }

    function kvTable(obj) {
        const entries = Object.entries(obj ?? {});
        if (entries.length === 0) return h('p', 'px-6 py-3 text-sm text-gray-500', 'Nenhum dado para ser exibido');
        const table = h('table', 'min-w-full divide-y divide-gray-100');
        const tbody = h('tbody');
        for (const [key, value] of entries) {
            const tr = h('tr');
            tr.append(
                h('td', 'px-6 py-2 align-top whitespace-nowrap text-sm font-medium text-gray-900', key),
                h('td', 'px-6 py-2 text-sm text-gray-600 break-all', Array.isArray(value) ? value.join(', ') : value));
            tbody.append(tr);
        }
        table.append(tbody);
        return table;
    }

    function problemList(problems) {
        const ul = h('ul', 'divide-y divide-gray-100');
        for (const problem of problems) {
            const li = h('li', 'px-6 py-2 text-sm flex flex-wrap items-start gap-2');
            li.append(
                h('span', `rounded px-2 py-0.5 text-xs font-semibold ${SEVERITY_STYLES[problem.severity]?.[0] ?? ''}`, problem.severity),
                h('code', 'font-semibold text-gray-900', problem.code),
                h('span', 'flex-1 min-w-[12rem] text-gray-600', problem.message));
            ul.append(li);
        }
        return ul;
    }

    function pre(text, extra = '') {
        return h('pre', `px-6 py-4 text-sm text-gray-800 overflow-x-auto whitespace-pre-wrap break-all ${extra}`, text);
    }

    function renderDetail(req) {
        const body = req.body ?? { kind: 'none' };
        const header = h('div', 'px-6 py-4 flex flex-wrap items-center gap-3');
        header.append(
            h('span', `rounded px-2 py-0.5 text-sm font-semibold ${METHOD_COLORS[req.method] ?? 'bg-gray-100 text-gray-800'}`, req.method),
            h('span', 'text-sm font-medium text-gray-900 break-all', `${req.scheme}://${req.host}${req.pathBase}${req.path}${req.queryString}`));

        const overview = kvTable({
            Id: req.id, Timestamp: new Date(req.timestamp).toLocaleString('pt-BR'), Protocol: req.protocol,
            RemoteIp: req.remoteIp ?? '', ContentType: req.contentType ?? '', ContentLength: req.contentLength ?? '',
        });

        const bodyBox = h('div');
        const summary = `${body.kind} · ${formatBytes(body.size ?? 0)}${body.truncated ? ' · truncado' : ''}${body.charset ? ' · ' + body.charset : ''}`;
        bodyBox.append(h('p', 'px-6 pt-3 text-xs text-gray-500', summary));
        if (body.form) bodyBox.append(kvTable(body.form));
        if (body.files?.length) {
            bodyBox.append(kvTable(Object.fromEntries(body.files.map(f => [f.name, `${f.fileName} · ${f.contentType ?? '?'} · ${formatBytes(f.length)}`]))));
        }
        if (body.json !== undefined) bodyBox.append(pre(JSON.stringify(body.json, null, 2), 'bg-gray-50'));
        else if (body.text !== undefined && body.text !== null) bodyBox.append(pre(body.text, 'bg-gray-50'));
        if (body.base64) bodyBox.append(pre(body.base64, 'bg-gray-50 text-xs text-gray-600'));
        if (body.kind === 'none') bodyBox.append(h('p', 'px-6 py-3 text-sm text-gray-500', 'Sem body.'));

        const sections = [header];
        if (req.problems?.length) sections.push(section(`Problemas (${req.problems.length})`, problemList(req.problems)));

        el.detail.replaceChildren(...sections,
            section('Request', overview),
            section('Headers', kvTable(req.headers)),
            section('Query', kvTable(req.query)),
            section('Body', bodyBox));
    }

    // Só troca o destaque das linhas: re-renderizar a lista inteira (até 1000 itens) a cada tecla seria caro.
    function highlight() {
        for (const li of el.list.children) {
            const selected = Number(li.dataset.id) === selectedId;
            li.classList.toggle('bg-indigo-50', selected);
            li.setAttribute('aria-selected', String(selected));
        }
    }

    let detailTimer = null;

    // fromKeyboard: rola a linha para a área visível e adia a busca do detalhe (debounce),
    // para que segurar PageDown não dispare uma requisição por linha percorrida.
    function select(id, { fromKeyboard = false } = {}) {
        selectedId = id;
        highlight();
        if (fromKeyboard) el.list.querySelector(`li[data-id="${id}"]`)?.scrollIntoView({ block: 'nearest' });

        el.detail.replaceChildren(h('p', 'px-6 py-10 text-center text-sm text-gray-500', 'Carregando…'));
        clearTimeout(detailTimer);
        detailTimer = setTimeout(() => loadDetail(id), fromKeyboard ? 150 : 0);
    }

    async function loadDetail(id) {
        try {
            const response = await fetch(`${base}api/requests/${id}`, { headers: { Accept: 'application/json' } });
            if (selectedId !== id) return; // o usuário já selecionou outro enquanto carregava
            if (response.status === 404) {
                el.detail.replaceChildren(h('p', 'px-6 py-10 text-center text-sm text-gray-500', 'Este request já foi expurgado do histórico.'));
                return;
            }
            renderDetail(await response.json());
        } catch (err) {
            if (selectedId === id)
                el.detail.replaceChildren(h('p', 'px-6 py-10 text-center text-sm text-red-600', `Falha ao carregar: ${err}`));
        }
    }

    // ---------- navegação por teclado ----------
    // Home/End: primeiro (mais recente) / último. PageUp/PageDown: uma "página" da lista. Setas: uma linha.
    const NAV_KEYS = new Set(['Home', 'End', 'PageUp', 'PageDown', 'ArrowUp', 'ArrowDown']);

    function rowsPerPage() {
        const row = el.list.firstElementChild;
        if (!row || row.offsetHeight === 0) return 10;
        return Math.max(1, Math.floor(el.list.clientHeight / row.offsetHeight) - 1);
    }

    function isTyping(target) {
        return target instanceof Element
            && target.matches('input:not([type=checkbox]):not([type=button]):not([type=submit]), textarea, select, [contenteditable=""], [contenteditable="true"]');
    }

    document.addEventListener('keydown', event => {
        if (!NAV_KEYS.has(event.key) || event.altKey || event.ctrlKey || event.metaKey || isTyping(event.target)) return;

        const ids = [...el.list.children].map(li => Number(li.dataset.id));
        if (ids.length === 0) return;
        event.preventDefault(); // não rola a página: as teclas passam a navegar na lista

        const current = ids.indexOf(selectedId);
        const page = rowsPerPage();
        const moves = {
            Home: () => 0,
            End: () => ids.length - 1,
            PageUp: () => current - page,
            PageDown: () => current + page,
            ArrowUp: () => current - 1,
            ArrowDown: () => current + 1,
        };
        // Sem seleção (ou selecionado escondido pelo filtro): qualquer tecla relativa começa do topo.
        const target = current < 0 && event.key !== 'End' ? 0 : moves[event.key]();
        const next = Math.min(Math.max(target, 0), ids.length - 1);

        if (ids[next] !== selectedId) select(ids[next], { fromKeyboard: true });
    });

    // ---------- WebSocket ----------
    function handleMessage(message) {
        switch (message.type) {
            case 'snapshot':
                items.clear();
                message.items.forEach(item => addItem(item));
                el.total.textContent = message.totalReceived;
                renderList();
                break;
            case 'request':
                if (paused) { pending.push(message); return; }
                addItem(message.item, message.totalReceived);
                renderList();
                break;
            case 'cleared':
                items.clear();
                pending = [];
                renderList();
                break;
        }
    }

    function connect() {
        setStatus('connecting');
        const url = new URL(`${base}ws`, location.href);
        url.protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
        const socket = new WebSocket(url);

        socket.onopen = () => { reconnectDelay = 1000; setStatus('online'); };
        socket.onmessage = event => handleMessage(JSON.parse(event.data));
        socket.onclose = () => {
            setStatus('offline');
            setTimeout(connect, reconnectDelay);
            reconnectDelay = Math.min(reconnectDelay * 2, 15000);
        };
    }

    // ---------- controles ----------
    el.filter.addEventListener('input', renderList);
    el.onlyProblems.addEventListener('change', renderList);

    el.pause.addEventListener('click', () => {
        paused = !paused;
        el.pause.textContent = paused ? 'Retomar' : 'Pausar';
        if (!paused) {
            pending.forEach(m => addItem(m.item, m.totalReceived));
            pending = [];
            renderList();
        }
        setStatus('online');
    });

    el.clear.addEventListener('click', async () => {
        if (!confirm('Remover todos os requests do histórico deste servidor?')) return;
        await fetch(`${base}api/requests`, { method: 'DELETE' });
        selectedId = null;
        el.detail.replaceChildren(h('p', 'px-6 py-10 text-center text-sm text-gray-500', 'Selecione um request para ver os detalhes.'));
    });

    connect();
})();
