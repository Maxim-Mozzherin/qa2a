let adminTicketsCache = [];
let ticketsPollInterval = null;
let globalBadgeInterval = null;

const catMap = { 
    'ttk': 'ТТК / Меню', 
    'writeoff': 'Списание / Склад', 
    'invoice': 'Накладная', 
    'inventory': 'Инвентаризация', 
    'other': 'Общее' 
};

function startGlobalBadgePolling() {
    if (globalBadgeInterval) clearInterval(globalBadgeInterval);
    pollGlobalBadge();
    globalBadgeInterval = setInterval(pollGlobalBadge, 15000); // Check every 15s
}

async function pollGlobalBadge() {
    try {
        const token = getAuthToken();
        const res = await fetch(`api/accounting/tickets?company_id=0`, {
            headers: token ? { 'Authorization': 'Bearer ' + token } : {}
        });
        if (!res.ok) return;
        const tickets = await res.json() || [];
        const newCount = tickets.filter(t => t.status === 'new').length;
        const badge = document.getElementById('global-header-badge');
        if (badge) {
            if (newCount > 0) {
                badge.innerText = newCount;
                badge.classList.remove('hidden');
            } else {
                badge.classList.add('hidden');
            }
        }
    } catch (e) {}
}

function openGlobalInboxModal() {
    const modal = document.getElementById('global-inbox-modal');
    if (modal) modal.classList.remove('hidden');
    loadGlobalInbox();
}

function closeGlobalInboxModal() {
    const modal = document.getElementById('global-inbox-modal');
    if (modal) modal.classList.add('hidden');
}

async function loadGlobalInbox() {
    const container = document.getElementById('global-inbox-content');
    if (!container) return;
    container.innerHTML = '<div class="p-8 text-center text-slate-500 text-xs animate-pulse">Загрузка глобальных входящих заявок...</div>';
    try {
        const token = getAuthToken();
        const res = await fetch(`api/accounting/tickets?company_id=0`, {
            headers: token ? { 'Authorization': 'Bearer ' + token } : {}
        });
        if (!res.ok) throw new Error(await res.text());
        const tickets = await res.json() || [];
        const activeTickets = tickets.filter(t => t.status === 'new' || t.status === 'in_progress');
        
        // Update header badge
        const newCount = tickets.filter(t => t.status === 'new').length;
        const badge = document.getElementById('global-header-badge');
        if (badge) {
            if (newCount > 0) {
                badge.innerText = newCount;
                badge.classList.remove('hidden');
            } else {
                badge.classList.add('hidden');
            }
        }

        if (activeTickets.length === 0) {
            container.innerHTML = '<div class="p-12 text-center text-slate-400 text-sm">🎉 Нет входящих заявок! Все задачи обработаны.</div>';
            return;
        }

        container.innerHTML = `<div class="space-y-4">${activeTickets.map(t => renderTicketCard(t)).join('')}</div>`;
        loadSecureTicketMedia(container);
    } catch (err) {
        container.innerHTML = `<div class="p-6 text-center text-rose-500 text-xs font-semibold">❌ Ошибка загрузки входящих заявок: ${err.message}</div>`;
    }
}

async function loadAdminTickets(isBackground = false) {
    const container = document.getElementById('tickets-admin-list');
    const companyId = els.company?.value;
    if (!companyId) {
        if (container) {
            container.innerHTML = '<div class="p-8 text-center text-slate-500 text-xs">Сначала выберите заведение в верхней панели</div>';
        }
        adminTicketsCache = [];
        return;
    }

    if (!isBackground && container) {
        container.innerHTML = '<div class="p-8 text-center text-slate-500 text-xs animate-pulse">Загрузка истории заявок...</div>';
    }
    try {
        const token = getAuthToken();
        const res = await fetch(`api/accounting/tickets?company_id=${companyId}`, {
            headers: token ? { 'Authorization': 'Bearer ' + token } : {}
        });
        if (!res.ok) throw new Error(await res.text());
        adminTicketsCache = await res.json() || [];
        filterAdminTickets();
    } catch (err) { 
        if (!isBackground && container) {
            container.innerHTML = `<div class="p-6 text-center text-rose-500 text-xs font-semibold">❌ Ошибка: ${err.message}</div>`; 
        }
    }
}

function startTicketsPolling() {
    stopTicketsPolling();
    loadAdminTickets();
    ticketsPollInterval = setInterval(() => loadAdminTickets(true), 10000);
}

function stopTicketsPolling() {
    if (ticketsPollInterval) {
        clearInterval(ticketsPollInterval);
        ticketsPollInterval = null;
    }
}

function loadSecureTicketMedia(container) {
    if (!container) return;
    const token = getAuthToken();
    container.querySelectorAll('[data-ticket-media]').forEach(el => {
        const src = el.getAttribute('data-ticket-media');
        if (!src) return;
        el.removeAttribute('data-ticket-media');
        fetch(src, { headers: token ? { 'Authorization': 'Bearer ' + token } : {} })
            .then(res => res.ok ? res.blob() : null)
            .then(blob => {
                if (blob) {
                    const blobUrl = URL.createObjectURL(blob);
                    el.src = blobUrl;
                    el.style.opacity = '1';
                    const parentLink = el.closest('a');
                    if (parentLink) parentLink.href = blobUrl;
                }
            })
            .catch(() => {});
    });
}

function renderTicketCard(t) {
    const formattedId = String(t.id).padStart(5, '0');

    let mediaHtml = '';
    try {
        const paths = typeof t.media_paths === 'string' ? JSON.parse(t.media_paths || '[]') : (t.media_paths || []);
        if (Array.isArray(paths) && paths.length > 0) {
            const items = paths.map(p => {
                const isVideo = p.endsWith('.mp4') || p.endsWith('.mov');
                let normPath = p;
                if (normPath.startsWith('/uploads/')) {
                    normPath = '/api' + normPath;
                } else if (!normPath.startsWith('/api/')) {
                    normPath = '/api/uploads/tickets/' + normPath;
                }
                if (isVideo) {
                    return `<video data-ticket-media="${normPath}" controls class="h-28 w-auto max-w-[200px] object-cover rounded-lg border border-slate-700 bg-black opacity-50"></video>`;
                } else {
                    return `<a href="javascript:void(0)" target="_blank"><img data-ticket-media="${normPath}" class="h-24 w-auto object-cover rounded-lg border border-slate-700 hover:opacity-80 transition-opacity opacity-50"></a>`;
                }
            }).join('');
            mediaHtml = `<div class="mt-3 flex flex-wrap gap-2">${items}</div>`;
        }
    } catch(e) {}

    return `
        <div class="bg-[#090d16] border border-slate-800 rounded-xl p-5 transition-all-300 hover:border-slate-700">
            <div class="flex flex-wrap items-center justify-between gap-2 border-b border-slate-800/80 pb-3">
                <div class="flex items-center gap-2">
                    <span class="font-mono text-xs font-black text-brand-400 bg-brand-500/10 px-2 py-1 rounded">#${formattedId}</span>
                    <span class="font-bold text-white text-xs">${escapeHtml(t.company_name)}</span>
                    <span class="text-[10px] text-slate-400 font-semibold bg-slate-800 px-2 py-0.5 rounded">
                        ${escapeHtml(catMap[t.category] || t.category)}
                    </span>
                </div>
                <div class="text-[10px] text-slate-500">
                    ${new Date(t.created_at).toLocaleString('ru-RU')} • Автор: <b class="text-slate-300">${escapeHtml(t.user_name)}</b>
                </div>
            </div>

            <div class="w-full h-auto text-xs text-slate-200 leading-relaxed whitespace-pre-wrap break-words bg-[#111827]/60 p-4 rounded-xl border border-slate-800/40 mt-3 mb-4 shadow-inner">
                ${escapeHtml(t.description)}
                ${mediaHtml}
            </div>

            <div class="grid grid-cols-1 md:grid-cols-12 gap-3 items-end bg-[#111827] p-3 rounded-xl border border-slate-800/50">
                <div class="md:col-span-3">
                    <label class="block text-[9px] font-bold text-slate-400 uppercase tracking-widest mb-1.5">Сменить статус</label>
                    <select id="ticket_status_${t.id}" class="w-full bg-[#090d16] border border-slate-700 text-white rounded-lg p-2.5 text-xs outline-none focus:border-brand-500 font-semibold">
                        <option value="new" ${t.status === 'new' ? 'selected' : ''}>🟡 Новая</option>
                        <option value="in_progress" ${t.status === 'in_progress' ? 'selected' : ''}>🔵 В работе</option>
                        <option value="resolved" ${t.status === 'resolved' ? 'selected' : ''}>🟢 Выполнена</option>
                        <option value="rejected" ${t.status === 'rejected' ? 'selected' : ''}>🔴 Отклонена</option>
                    </select>
                </div>
                <div class="md:col-span-6">
                    <label class="block text-[9px] font-bold text-slate-400 uppercase tracking-widest mb-1.5">Ответное сообщение (push в Telegram)</label>
                    <input type="text" id="ticket_comment_${t.id}" value="${escapeHtml(t.accountant_comment)}" placeholder="Например: Списание скорректировано, ТТК добавлена..." class="w-full bg-[#090d16] border border-slate-700 text-white rounded-lg p-2.5 text-xs outline-none focus:border-brand-500">
                </div>
                <div class="md:col-span-3">
                    <button onclick="saveTicketResolution(${t.id})" class="w-full bg-slate-800 hover:bg-brand-600 active:bg-brand-700 text-white p-2.5 rounded-lg text-xs font-bold transition-all-300 border border-slate-700 shadow-sm flex items-center justify-center gap-1.5">
                        💾 Применить
                    </button>
                </div>
            </div>
        </div>
    `;
}

function filterAdminTickets() {
    const container = document.getElementById('tickets-admin-list');
    if (!container) return;

    const companyId = els.company?.value;
    if (!companyId) {
        container.innerHTML = '<div class="p-8 text-center text-slate-500 text-xs">Сначала выберите заведение в верхней панели</div>';
        return;
    }

    const searchStr = (document.getElementById('ticket-search-input')?.value || "").toLowerCase().trim();
    const statusFilterEl = document.getElementById('ticket-status-filter');
    const statusFilter = statusFilterEl ? statusFilterEl.value : "all";

    let filtered = adminTicketsCache.filter(t => {
        let matchStatus = true;
        if (statusFilter === 'active') matchStatus = (t.status === 'new' || t.status === 'in_progress');
        else if (statusFilter === 'new') matchStatus = (t.status === 'new');
        else if (statusFilter === 'resolved') matchStatus = (t.status === 'resolved' || t.status === 'rejected');

        let matchSearch = true;
        if (searchStr !== "") {
            const searchId = searchStr.replace('#', '');
            matchSearch = String(t.id).includes(searchId) || 
                          (t.company_name || '').toLowerCase().includes(searchStr) || 
                          (t.description || '').toLowerCase().includes(searchStr) ||
                          (t.user_name || '').toLowerCase().includes(searchStr);
        }
        return matchStatus && matchSearch;
    });

    if (filtered.length === 0) {
        container.innerHTML = '<div class="p-8 text-center text-slate-500 text-xs">Заявок не найдено 🎉</div>';
        return;
    }

    container.innerHTML = filtered.map(t => renderTicketCard(t)).join('');
    loadSecureTicketMedia(container);
}

async function saveTicketResolution(ticketId) {
    const statusEl = document.getElementById(`ticket_status_${ticketId}`);
    const commentEl = document.getElementById(`ticket_comment_${ticketId}`);
    if (!statusEl) return;
    const status = statusEl.value;
    const comment = commentEl ? commentEl.value : "";
    try {
        const token = getAuthToken();
        const res = await fetch('api/accounting/tickets/resolve', {
            method: 'POST', 
            headers: { 
                'Content-Type': 'application/json',
                'Authorization': 'Bearer ' + token
            },
            body: JSON.stringify({ ticket_id: ticketId, status, comment })
        });
        if (!res.ok) throw new Error(await res.text());
        if (status === 'resolved' || status === 'rejected') {
            alert("✅ Задача закрыта! Автору отправлено уведомление в Telegram.");
        }
        
        // Refresh whichever view is open
        const modal = document.getElementById('global-inbox-modal');
        if (modal && !modal.classList.contains('hidden')) {
            loadGlobalInbox();
            pollGlobalBadge();
        }
        const ticketsSection = document.getElementById('bugh-section-tickets');
        if (ticketsSection && !ticketsSection.classList.contains('hidden')) {
            loadAdminTickets(true);
        }
    } catch (err) { 
        alert("❌ Ошибка: " + err.message); 
    }
}

window.openGlobalInboxModal = openGlobalInboxModal;
window.closeGlobalInboxModal = closeGlobalInboxModal;
window.loadGlobalInbox = loadGlobalInbox;
window.loadAdminTickets = loadAdminTickets;
window.filterAdminTickets = filterAdminTickets;
window.saveTicketResolution = saveTicketResolution;
window.startTicketsPolling = startTicketsPolling;
window.stopTicketsPolling = stopTicketsPolling;
window.startGlobalBadgePolling = startGlobalBadgePolling;
window.pollGlobalBadge = pollGlobalBadge;

