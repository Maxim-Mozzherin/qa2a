const tg = window.Telegram?.WebApp || {};
const API = window.location.origin + '/api';

// Глобальные переменные состояния
let currentUser = null;
let userToken = null;
let currentCompanyId = localStorage.getItem('selected_company_id');
let allMemberships = [];
let catalog = [];
let locations = [];

// Инициализация Telegram WebApp
if (tg.ready) {
    tg.ready();
    tg.expand();
}

document.addEventListener('DOMContentLoaded', initApp);

/**
 * Инициализация приложения: Авторизация и первичная загрузка данных
 */
async function initApp() {
    try {
        const initData = tg.initData || "";
        const reqData = initData ? { initData } : { demo_id: 999, demo_name: "Boss" };

        const res = await fetch(API + '/auth', {
            method: 'POST', 
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(reqData)
        });

        if (!res.ok) {
            const errText = await res.text();
            throw new Error(`Сервер ответил: ${res.status} - ${errText}`);
        }

        const data = await res.json();
        
        currentUser = data.user;
        userToken = currentUser.tg_id;
        allMemberships = data.memberships || [];

        if (allMemberships.length === 0) {
            document.getElementById('onboarding').style.display = 'block';
            document.getElementById('main-app').style.display = 'none';
        } else {
            // Выбираем ID компании: сохраненный в localStorage ИЛИ первый из доступных
            const savedId = localStorage.getItem('selected_company_id');
            const found = allMemberships.find(m => m.company_id == savedId);
            
            currentCompanyId = found ? parseInt(savedId) : allMemberships[0].company_id;
            localStorage.setItem('selected_company_id', currentCompanyId);
            
            // UI переключение
            document.getElementById('onboarding').style.display = 'none';
            document.getElementById('main-app').style.display = 'block';
            
            const current = allMemberships.find(m => m.company_id == currentCompanyId);
            document.getElementById('orgName').textContent = current ? current.company_name : "Бизнес";
            
            // ЗАГРУЗКА ДАННЫХ В ПРАВИЛЬНОМ ПОРЯДКЕ
            await loadAllData();
            renderCompanyList();
            loadHistory(); // Теперь точно есть currentCompanyId
            
            // Если вкладка Команда активна — грузим и её
            if (document.getElementById('nav-team').classList.contains('active')) {
                loadTeamData();
            }
        }
    } catch (e) {
        console.error("Critical Init Error:", e);
        alert("Ошибка входа: " + e.message);
    }
}

/**
 * Вспомогательная функция для формирования заголовков запроса
 */
function getHeaders() { 
    return { 
        'Content-Type': 'application/json', 
        'X-Telegram-ID': String(userToken || ""), 
        'X-Company-ID': String(currentCompanyId || localStorage.getItem('selected_company_id') || "") 
    }; 
}

/**
 * Загрузка каталога товаров и списка складов
 */
async function loadAllData() {
    try {
        const [resP, resL] = await Promise.all([
            fetch(API + '/positions', { headers: getHeaders() }),
            fetch(API + '/locations', { headers: getHeaders() })
        ]);

        if (resP.ok) catalog = await resP.json() || [];
        if (resL.ok) {
            locations = await resL.json() || [];
            const opts = '<option value="">Выбрать склад...</option>' + 
                         locations.map(l => `<option value="${l.id}">${l.name}</option>`).join('');
            
            // Заполняем все выпадающие списки складов в шторках
            ['w_location', 'p_location', 'tr_location', 'tr_to_location'].forEach(id => {
                const el = document.getElementById(id);
                if (el) el.innerHTML = opts;
            });
        }
    } catch (e) {
        console.error("Data loading failed:", e);
    }
}

/**
 * Логика «живого» поиска товаров (Fuzzy Search)
 */
function filterSearch(prefix) {
    const q = document.getElementById(prefix + '_search').value.toLowerCase();
    const div = document.getElementById(prefix + '_results');
    
    if (q.length < 1) {
        div.style.display = 'none';
        return;
    }

    const matches = catalog.filter(p => p.name.toLowerCase().includes(q));
    
    if (matches.length > 0) {
        div.innerHTML = matches.map(p => `
            <div class="search-item" onclick="selectPos('${prefix}','${p.name}','${p.unit}')">
                ${p.name} <small style="color:gray">(${p.unit})</small>
            </div>
        `).join('');
        div.style.display = 'block';
    } else {
        div.style.display = 'none';
    }
}

/**
 * Выбор товара из результатов поиска
 */
function selectPos(prefix, name, unit) {
    document.getElementById(prefix + '_name').value = name;
    document.getElementById(prefix + '_search').value = name;
    document.getElementById(prefix + '_results').style.display = 'none';
    
    const unitDisplay = document.getElementById(prefix + '_unit_display');
    if (unitDisplay) unitDisplay.textContent = unit;
}

/**
 * Отправка операции (списание, перенос, заявка)
 */
async function submitOperation(type, prefix) {
    const name = document.getElementById(prefix + '_name').value;
    const qty = document.getElementById(prefix + '_qty').value;
    const locId = document.getElementById(prefix + '_location')?.value;

    if (!name || !qty || (!locId && type !== 'procurement')) {
        alert("Заполните все обязательные поля!");
        return;
    }

    try {
        const body = {
            position_name: name,
            quantity: parseFloat(qty),
            unit: document.getElementById(prefix + '_unit_display')?.textContent || 'шт',
            type: type,
            location_id: parseInt(locId) || 0
        };

        const res = await fetch(API + '/operations', {
            method: 'POST',
            headers: getHeaders(),
            body: JSON.stringify(body)
        });

        if (res.ok) {
            closeDrawer();
            loadHistory();
            if (tg.HapticFeedback) tg.HapticFeedback.notificationOccurred('success');
            // Очистка полей
            document.getElementById(prefix + '_search').value = '';
            document.getElementById(prefix + '_qty').value = '';
        }
    } catch (e) {
        alert("Ошибка отправки");
    }
}

/**
 * Вкладки (Tabs)
 */
function switchTab(id) {
    document.querySelectorAll('.tab-content').forEach(t => t.style.display = 'none');
    document.getElementById('tab-' + id).style.display = 'block';
    
    document.querySelectorAll('.nav-item').forEach(i => i.classList.remove('active'));
    const activeNav = document.getElementById('nav-' + id);
    if (activeNav) activeNav.classList.add('active');

    if (id === 'stock') loadStock();
    if (id === 'admin') loadAdminLists();
    if (id === 'team') loadTeamData();
}

/**
 * Загрузка и группировка остатков по складам
 */
async function loadStock() {
    try {
        const res = await fetch(API + '/balances', { headers: getHeaders() });
        const data = await res.json() || [];
        const container = document.getElementById('stockList');

        const grouped = data.reduce((acc, b) => {
            const locName = locations.find(l => l.id === b.location_id)?.name || "Склад";
            if (!acc[locName]) acc[locName] = [];
            acc[locName].push(b);
            return acc;
        }, {});

        container.innerHTML = Object.keys(grouped).map(loc => `
            <div style="margin-bottom:20px">
                <div class="section-title" style="color:var(--accent)">📍 ${loc}</div>
                ${grouped[loc].map(b => `
                    <div class="pulse-item">
                        <div>${b.position_name}</div>
                        <div class="pulse-val">${b.quantity} ${b.unit}</div>
                    </div>
                `).join('')}
            </div>
        `).join('') || "На складе пусто";
    } catch (e) { console.error(e); }
}

/**
 * Загрузка истории событий
 */
async function loadHistory() {
    try {
        const res = await fetch(API + '/operations', { headers: getHeaders() });
        const data = await res.json() || [];
        const feed = document.getElementById('historyFeed');
        
        feed.innerHTML = data.map(op => `
            <div class="pulse-item">
                <div class="pulse-info">
                    <div>${op.position_name}</div>
                    <span>${op.user_name} • ${new Date(op.created_at).toLocaleTimeString([], {hour:'2-digit', minute:'2-digit'})}</span>
                </div>
                <div class="pulse-val" style="color:${op.quantity < 0 ? '#EF4444' : '#10B981'}">
                    ${op.quantity} ${op.unit}
                </div>
            </div>
        `).join('') || "Нет событий";
    } catch (e) { console.error(e); }
}

/**
 * Функции управления (Admin/Owner)
 */
async function submitNewPosition() {
    const name = document.getElementById('p_name').value;
    const unit = document.getElementById('p_unit').value;
    const qty = document.getElementById('p_init_qty').value;
    const loc = document.getElementById('p_location').value;

    if (!name || !loc) { alert("Имя и склад обязательны!"); return; }

    const res = await fetch(API + '/positions', {
        method: 'POST',
        headers: getHeaders(),
        body: JSON.stringify({ 
            name, unit, 
            initial_quantity: parseFloat(qty) || 0, 
            location_id: parseInt(loc) 
        })
    });

    if (res.ok) {
        closeDrawer();
        await loadAllData();
        loadAdminLists();
    }
}

async function submitNewLocation() {
    const name = document.getElementById('loc_name').value;
    if (!name) return;

    const res = await fetch(API + '/locations', {
        method: 'POST',
        headers: getHeaders(),
        body: JSON.stringify({ name })
    });

    if (res.ok) {
        closeDrawer();
        await loadAllData();
        loadAdminLists();
    }
}

async function loadAdminLists() {
    document.getElementById('adminLocationList').innerHTML = (locations || []).map(l => `
        <div class="pulse-item"><div>${l.name}</div></div>
    `).join('') || "Нет складов";

    const resP = await fetch(API + '/positions', { headers: getHeaders() });
    const pos = await resP.json() || [];
    document.getElementById('adminPositionList').innerHTML = pos.map(p => `
        <div class="pulse-item"><div>${p.name}</div><span>${p.unit}</span></div>
    `).join('') || "Нет товаров";
}

/**
 * Управление командой
 */
async function loadTeamData() {
    const listEl = document.getElementById('memberList');
    if (!listEl) return;
    listEl.innerHTML = "Загрузка...";

    try {
        // 1. Грузим код
        const resC = await fetch(API + '/invite-code', { headers: getHeaders() });
        const dataC = await resC.json();
        document.getElementById('displayInviteCode').textContent = dataC.code || "---";

        // 2. Грузим людей
        const resM = await fetch(API + '/members', { headers: getHeaders() });
        const members = await resM.json() || [];

        // 3. ПРОВЕРКА ПРАВ: Находим роль текущего юзера в этой компании
        const myMembership = allMemberships.find(m => m.company_id == currentCompanyId);
        const isOwner = myMembership && myMembership.role === 'owner';

        listEl.innerHTML = members.map(m => {
            const isNotMe = m.user_id != currentUser.id;
            return `
                <div class="pulse-item">
                    <div class="pulse-info">
                        <div>${m.user_name || 'Сотрудник'}</div>
                        <span>Роль: ${translateRole(m.role)}</span>
                    </div>
                    ${isOwner && isNotMe ? 
                        `<button onclick="changeRole(${m.user_id},'${m.role}')" class="btn-tiny">РОЛЬ</button>` 
                        : ''}
                </div>
            `;
        }).join('');
    } catch (e) { 
        console.error(e);
        listEl.innerHTML = "Ошибка загрузки команды";
    }
}

function translateRole(role) {
    const roles = { 'owner': 'Владелец', 'admin': 'Админ', 'manager': 'Менеджер', 'user': 'Сотрудник' };
    return roles[role] || role;
}

window.copyInviteCode = () => {
    const code = document.getElementById('displayInviteCode').textContent;
    if (code && code !== "---") {
        navigator.clipboard.writeText(code);
        tg.showAlert("Код скопирован: " + code);
    }
};

async function changeRole(userId, currentRole) {
    const newRole = prompt(`Текущая роль: ${currentRole}\nВведите новую (admin, manager, user):`);
    if (newRole) {
        await fetch(API + '/members', {
            method: 'PUT',
            headers: getHeaders(),
            body: JSON.stringify({ user_id: userId, role: newRole })
        });
        loadTeamData();
    }
}

/**
 * Переключатель бизнесов
 */
function renderCompanyList() {
    const list = document.getElementById('companyList');
    if (!list) return;
    list.innerHTML = allMemberships.map(m => `
        <div class="pulse-item" onclick="selectCompany(${m.company_id})" 
             style="cursor:pointer; border:${m.company_id == currentCompanyId ? '1px solid var(--accent)' : '1px solid var(--border)'}">
            <div>${m.company_name}</div>
        </div>
    `).join('');
}

function selectCompany(id) {
    localStorage.setItem('selected_company_id', id);
    location.reload();
}

/**
 * Модальные окна
 */
function openDrawer(id) {
    document.getElementById('overlay').style.display = 'block';
    document.getElementById('drawer_' + id).classList.add('open');
}

function closeDrawer() {
    document.querySelectorAll('.drawer').forEach(d => d.classList.remove('open'));
    document.getElementById('overlay').style.display = 'none';
}

// Привязка функций к объекту window для доступа из HTML (onclick)
window.selectCompany = selectCompany;
window.openDrawer = openDrawer;
window.closeDrawer = closeDrawer;
window.switchTab = switchTab;
window.submitOperation = submitOperation;
window.submitNewPosition = submitNewPosition;
window.submitNewLocation = submitNewLocation;
window.filterSearch = filterSearch;
window.selectPos = selectPos;
window.changeRole = changeRole;

window.createNewBusiness = async () => {
    const n = prompt("Название вашего заведения:");
    if (n) {
        const res = await fetch(API + '/companies', { 
            method: 'POST', 
            headers: getHeaders(), 
            body: JSON.stringify({ name: n }) 
        });
        if (res.ok) {
            const d = await res.json();
            localStorage.setItem('selected_company_id', d.id);
            location.reload();
        }
    }
};

window.joinByCode = async () => {
    const c = prompt("Введите код приглашения (QA-XXXX):");
    if (c) {
        const res = await fetch(API + '/join', { 
            method: 'POST', 
            headers: getHeaders(), 
            body: JSON.stringify({ code: c.toUpperCase() }) 
        });
        if (res.ok) location.reload();
        else alert("Код не найден или вы уже состоите в этом бизнесе");
    }
};

window.showInviteCode = async () => {
    const res = await fetch(API + '/invite-code', { headers: getHeaders() });
    const data = await res.json();
    alert("Ваш личный код для приглашения сотрудников: " + data.code);
};