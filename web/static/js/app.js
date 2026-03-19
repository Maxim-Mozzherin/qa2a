const tg = window.Telegram?.WebApp || {};
const API = window.location.origin + '/api';

let currentUser = null;
let userToken = null;
let currentCompanyId = localStorage.getItem('selected_company_id');
let allMemberships = [];
let catalog = [];
let locations = [];

if (tg.ready) {
    tg.ready();
    tg.expand();
}

document.addEventListener('DOMContentLoaded', initApp);

async function initApp() {
    try {
        const initData = tg.initData || "";
        const reqData = initData ? { initData } : { demo_id: 999, demo_name: "Boss" };

        const res = await fetch(API + '/auth', {
            method: 'POST', 
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(reqData)
        });

        const data = await res.json();
        currentUser = data.user;
        userToken = currentUser.tg_id;
        allMemberships = data.memberships || [];

        if (allMemberships.length === 0) {
            document.getElementById('onboarding').style.display = 'block';
            document.getElementById('main-app').style.display = 'none';
        } else {
            const savedId = localStorage.getItem('selected_company_id');
            const found = allMemberships.find(m => m.company_id == savedId);
            currentCompanyId = found ? parseInt(savedId) : allMemberships[0].company_id;
            localStorage.setItem('selected_company_id', currentCompanyId);
            
            document.getElementById('onboarding').style.display = 'none';
            document.getElementById('main-app').style.display = 'block';
            
            const current = allMemberships.find(m => m.company_id == currentCompanyId);
            document.getElementById('orgName').textContent = current ? current.company_name : "Бизнес";
            
            await loadAllData();
            renderCompanyList();
            loadHistory();
            
            if (document.getElementById('nav-team').classList.contains('active')) {
                loadTeamData();
            }
        }
    } catch (e) {
        console.error("Init Error:", e);
    }
}

function getHeaders() { 
    return { 
        'Content-Type': 'application/json', 
        'X-Telegram-ID': String(userToken || ""), 
        'X-Company-ID': String(currentCompanyId || "") 
    }; 
}

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
            ['w_location', 'p_location', 'tr_location', 'tr_to_location'].forEach(id => {
                const el = document.getElementById(id);
                if (el) el.innerHTML = opts;
            });
        }
    } catch (e) {}
}

/**
 * Логика «живого» поиска товаров
 */
function filterSearch(prefix) {
    const q = document.getElementById(prefix + '_search').value.toLowerCase();
    const div = document.getElementById(prefix + '_results');
    
    // Очищаем поля и прячем плашку при новом вводе
    document.getElementById(prefix + '_name').value = '';
    const unitDisplay = document.getElementById(prefix + '_unit_display');
    if (unitDisplay) unitDisplay.value = '';
    
    const warningEl = document.getElementById(prefix + '_ghost_warning');
    if (warningEl) warningEl.style.display = 'none'; // Прячем плашку пока он печатает

    let ghostFlag = document.getElementById(prefix + '_is_unlisted');
    if (!ghostFlag) {
        ghostFlag = document.createElement('input');
        ghostFlag.type = 'hidden';
        ghostFlag.id = prefix + '_is_unlisted';
        document.getElementById(prefix + '_search').parentNode.appendChild(ghostFlag);
    }
    ghostFlag.value = 'false';

    if (q.length < 1) {
        div.style.display = 'none';
        return;
    }

    const matches = catalog.filter(p => p.name.toLowerCase().includes(q));
    
    let html = matches.map(p => `
        <div class="search-item" onclick="selectPos('${prefix}','${p.name}','${p.unit}')">
            ${p.name} <small style="color:gray">(${p.unit})</small>
        </div>
    `).join('');

    // Кнопка призрака всегда внизу
    html += `
        <div class="search-item" style="color: var(--accent); font-weight: 800; cursor: pointer;" onclick="selectGhostPos('${prefix}', '${document.getElementById(prefix + '_search').value}')">
            + Ввести вручную: "${document.getElementById(prefix + '_search').value}"
        </div>
    `;

    div.innerHTML = html;
    div.style.display = 'block';
}

/**
 * Выбор товара из каталога (ОФИЦИАЛЬНЫЙ)
 */
function selectPos(prefix, name, unit) {
    document.getElementById(prefix + '_name').value = name;
    document.getElementById(prefix + '_search').value = name;
    document.getElementById(prefix + '_results').style.display = 'none';
    
    const unitDisplay = document.getElementById(prefix + '_unit_display');
    if (unitDisplay) unitDisplay.value = unit; 

    let ghostFlag = document.getElementById(prefix + '_is_unlisted');
    if(ghostFlag) ghostFlag.value = 'false';

    // СКРЫВАЕМ ПЛАШКУ
    const warningEl = document.getElementById(prefix + '_ghost_warning');
    if (warningEl) warningEl.style.display = 'none';
}

/**
 * Выбор товара, которого нет в базе (ПРИЗРАК)
 */
function selectGhostPos(prefix, name) {
    const unit = prompt(`Укажите единицу измерения для "${name}"\n(например: шт, кг, л, упак):`);
    if (!unit) return; 

    document.getElementById(prefix + '_name').value = name;
    document.getElementById(prefix + '_search').value = name + ' (Введено вручную)';
    document.getElementById(prefix + '_results').style.display = 'none';
    
    const unitDisplay = document.getElementById(prefix + '_unit_display');
    if (unitDisplay) unitDisplay.value = unit;

    let ghostFlag = document.getElementById(prefix + '_is_unlisted');
    if(ghostFlag) ghostFlag.value = 'true';

    // ПОКАЗЫВАЕМ ПЛАШКУ
    const warningEl = document.getElementById(prefix + '_ghost_warning');
    if (warningEl) warningEl.style.display = 'block';
}

async function submitOperation(type, prefix) {
    const name = document.getElementById(prefix + '_name').value;
    const qty = document.getElementById(prefix + '_qty').value;
    const locId = document.getElementById(prefix + '_location')?.value;
    const toLocId = document.getElementById(prefix + '_to_location')?.value;
    const unit = document.getElementById(prefix + '_unit_display')?.value || '';
    const isUnlisted = document.getElementById(prefix + '_is_unlisted')?.value === 'true';
    let comment = "";

    // 1. Проверка на "Призрака"
    if (isUnlisted) {
        comment = prompt(`Вы списываете товар вне каталога ("${name}").\nУкажите причину (обязательно):`);
        if (!comment) {
            alert("Для внекаталожного товара причина обязательна!");
            return;
        }
    }

    // 2. Базовая валидация
    if (!name || !qty) { alert("Заполните поля!"); return; }

    // 3. Формирование тела запроса
    const body = {
        position_name: name,
        quantity: parseFloat(qty),
        unit: unit,
        type: type,
        location_id: parseInt(locId) || 0,
        is_unlisted: isUnlisted,
        comment: comment
    };

    if (type === 'transfer') {
        if (!toLocId) { alert("Выберите склад-получатель!"); return; }
        body.to_location_id = parseInt(toLocId);
    }

    // 4. Отправка на сервер
    try {
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
            document.getElementById(prefix + '_name').value = '';
            document.getElementById(prefix + '_qty').value = '';
            document.getElementById(prefix + '_unit_display').value = '';
            
            // Прячем плашку после УСПЕШНОЙ отправки!
            const warningEl = document.getElementById(prefix + '_ghost_warning');
            if (warningEl) warningEl.style.display = 'none';
            
        } else {
            const txt = await res.text();
            alert("Ошибка: " + txt);
        }
    } catch (e) { 
        alert("Сбой сети"); 
    }
}
/**
 * Отправка операции переноса между складами
 */
async function submitTransfer(prefix) {
    const name = document.getElementById(prefix + '_name').value;
    const qty = document.getElementById(prefix + '_qty').value;
    const fromLocId = document.getElementById(prefix + '_location').value;
    const toLocId = document.getElementById(prefix + '_to_location').value;
    const unit = document.getElementById(prefix + '_unit_display').value || '';

    if (!name || !unit) { alert("Выберите товар из списка!"); return; }
    if (!qty || parseFloat(qty) <= 0) { alert("Укажите корректное количество!"); return; }
    if (!fromLocId || !toLocId) { alert("Выберите оба склада!"); return; }
    if (fromLocId === toLocId) { alert("Склады отправления и назначения должны различаться!"); return; }

    try {
        const body = {
            position_name: name,
            quantity: parseFloat(qty),
            unit: unit,
            type: 'transfer',
            location_id: parseInt(fromLocId),
            to_location_id: parseInt(toLocId)
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
            document.getElementById(prefix + '_name').value = '';
            document.getElementById(prefix + '_qty').value = '';
            document.getElementById(prefix + '_unit_display').value = '';
        } else {
            const err = await res.text();
            alert("Ошибка переноса: " + err);
        }
    } catch (e) {
        alert("Ошибка сети");
    }
}
function switchTab(id) {
    document.querySelectorAll('.tab-content').forEach(t => t.style.display = 'none');
    document.getElementById('tab-' + id).style.display = 'block';
    document.querySelectorAll('.nav-item').forEach(i => i.classList.remove('active'));
    document.getElementById('nav-' + id).classList.add('active');
    if (id === 'stock') loadStock();
    if (id === 'admin') loadAdminLists();
    if (id === 'team') loadTeamData();
    if (id === 'admin') {
        loadAdminLists();
        loadPendingRequests();
        loadGhostItemsForAdmin(); // Загрузка призраков для админа при открытии вкладки "Админка"
    }
}

async function loadStock() {
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
                <div class="pulse-item"><div>${b.position_name}</div><div class="pulse-val">${b.quantity} ${b.unit}</div></div>
            `).join('')}
        </div>
    `).join('') || "На складе пусто";
}

async function loadHistory() {
    const res = await fetch(API + '/operations', { headers: getHeaders() });
    const data = await res.json() || [];
    document.getElementById('historyFeed').innerHTML = data.map(op => `
        <div class="pulse-item">
            <div class="pulse-info">
                <div>${op.position_name} <small>(${op.type === 'transfer' ? '🔄' : '📉'})</small></div>
                <span>${op.user_name} • ${new Date(op.created_at).toLocaleTimeString([], {hour:'2-digit', minute:'2-digit'})}</span>
            </div>
            <div class="pulse-val" style="color:${op.type === 'transfer' ? 'var(--accent)' : (op.quantity < 0 ? '#EF4444' : '#10B981')}">
                ${op.quantity} ${op.unit}
            </div>
        </div>
    `).join('') || "Нет событий";
}

async function submitNewPosition() {
    const name = document.getElementById('p_name').value.trim();
    const unit = document.getElementById('p_unit').value;
    const supplier = document.getElementById('p_supplier').value.trim();
    const qty = document.getElementById('p_init_qty').value;
    const loc = document.getElementById('p_location').value;

    // ГРОМКИЕ ПРОВЕРКИ
    if (!name) {
        alert("Пожалуйста, введите название товара!");
        return;
    }
    
    // Если указали количество, то ОБЯЗАТЕЛЬНО нужен склад
    if (parseFloat(qty) > 0 && !loc) {
        alert("Вы указали начальный остаток. Пожалуйста, выберите склад для его зачисления!");
        return;
    }

    try {
        const res = await fetch(API + '/positions', {
            method: 'POST',
            headers: getHeaders(),
            body: JSON.stringify({ 
                name: name, 
                unit: unit, 
                supplier: supplier, // Отправляем поставщика
                initial_quantity: parseFloat(qty) || 0, 
                location_id: parseInt(loc) || 0 
            })
        });

        if (res.ok) { 
            alert("Товар успешно добавлен в каталог!");
            closeDrawer(); 
            
            // Очищаем поля после успешного добавления
            document.getElementById('p_name').value = '';
            document.getElementById('p_supplier').value = '';
            document.getElementById('p_init_qty').value = '';
            document.getElementById('p_location').value = '';
            
            await loadAllData(); 
            loadAdminLists(); 
            
            // Если мы добавили призрака, обновляем верхний список
            if (typeof loadGhostItemsForAdmin === 'function') {
                loadGhostItemsForAdmin();
            }
        } else {
            const errTxt = await res.text();
            alert("Ошибка при создании: " + errTxt);
        }
    } catch (e) {
        alert("Ошибка сети!");
    }
}

async function submitNewLocation() {
    const name = document.getElementById('loc_name').value;
    if (!name) return;
    const res = await fetch(API + '/locations', { method: 'POST', headers: getHeaders(), body: JSON.stringify({ name }) });
    if (res.ok) { closeDrawer(); await loadAllData(); loadAdminLists(); }
}

async function loadAdminLists() {
    // Вызываем существующие загрузки
    document.getElementById('adminLocationList').innerHTML = (locations || []).map(l => `<div class="pulse-item"><div>${l.name}</div></div>`).join('') || "Нет складов";
    
    const resP = await fetch(API + '/positions', { headers: getHeaders() });
    const pos = await resP.json() || [];
    
    document.getElementById('adminPositionList').innerHTML = pos.map(p => `
        <div class="pulse-item">
            <div class="pulse-info">
                <div>${p.name}</div>
                <span style="font-size:11px; color:var(--text-muted)">${p.unit}</span>
            </div>
        </div>
    `).join('') || "Нет товаров";

    // Добавляем загрузку ИСТОРИИ заявок (одобренных)
    loadApprovedRequests();
}
async function loadApprovedRequests() {
    const container = document.getElementById('adminApprovedRequests');
    if (!container) return;

    try {
        const res = await fetch(API + '/procurements?status=approved', { headers: getHeaders() });
        const requests = await res.json() || [];

        if (requests.length === 0) {
            container.innerHTML = '<div style="font-size:12px; color:var(--text-muted);">Архив пуст</div>';
            return;
        }

        container.innerHTML = requests.map(req => `
            <div class="pulse-item">
                <div class="pulse-info">
                    <div>Заявка #${req.id}</div>
                    <span>${new Date(req.created_at).toLocaleDateString()} • ${req.user_name}</span>
                </div>
                <button class="btn-tiny" onclick="downloadPDF(${req.id})">PDF</button>
            </div>
        `).join('');
    } catch (e) { console.error(e); }
}
/*
function downloadPDF(id) {
    const url = `${API}/procurements/download/${id}`;
    // В Telegram WebApp лучше открывать через скачивание ссылки
    const anchor = document.createElement('a');
    anchor.href = url;
    // Добавляем заголовки через URL если нужно, но у нас AuthMiddleware проверяет Header. 
    // ВНИМАНИЕ: Обычный <a> не пробросит Header X-Telegram-ID.
    // Решение: Используем fetch + Blob для сохранения.
    
    fetch(url, { headers: getHeaders() })
        .then(res => res.blob())
        .then(blob => {
            const blobUrl = window.URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = blobUrl;
            a.download = `Заявка_${id}.pdf`;
            document.body.appendChild(a);
            a.click();
            a.remove();
        })
        .catch(err => alert("Ошибка при скачивании"));
}*/
function downloadPDF(id) {
    // Формируем прямую ссылку с параметрами авторизации
    const url = `${API}/procurements/download/${id}?tg_id=${userToken}&c_id=${currentCompanyId}`;
    
    // Используем нативный метод Telegram: он откроет системный просмотрщик поверх приложения
    if (tg.openLink) {
        tg.openLink(url);
    } else {
        // Запасной вариант для работы в обычном браузере на ПК
        window.open(url, '_blank');
    }
}
async function loadTeamData() {
    const listEl = document.getElementById('memberList');
    if (!listEl) return;
    
    // ЧАСТЬ 1: Загружаем инвайт-код (безопасно)
    try {
        const resC = await fetch(API + '/invite-code', { headers: getHeaders() });
        if (resC.ok) {
            const dataC = await resC.json();
            document.getElementById('displayInviteCode').textContent = dataC.code || "---";
        } else {
            document.getElementById('displayInviteCode').textContent = "Код не найден";
        }
    } catch (e) {
        document.getElementById('displayInviteCode').textContent = "Ошибка сети";
    }

    // ЧАСТЬ 2: Загружаем участников
    try {
        const resM = await fetch(API + '/members', { headers: getHeaders() });
        
        // ЕСЛИ СЕРВЕР ВЕРНУЛ ОШИБКУ — ПОКАЖЕМ АЛЕРТ!
        if (!resM.ok) {
            const errText = await resM.text();
            alert("Ошибка с сервера (участники): " + resM.status + " -> " + errText);
            listEl.innerHTML = "Ошибка загрузки: " + resM.status;
            return;
        }

        const members = await resM.json() || [];
        
        const myMembership = allMemberships.find(m => m.company_id == currentCompanyId);
        const myRole = myMembership?.role; 

        listEl.innerHTML = members.map(m => {
            const title = m.custom_title || '';
            const titleBadge = title 
                ? `<span class="role-badge">${title}</span>` 
                : '';
            
            const canEdit = (myRole === 'owner' || myRole === 'manager' || myRole === 'admin') && m.user_id != currentUser.id;
            
            // Защита: если функции translateRole нет, выводим как есть
            const displayRole = (typeof translateRole === 'function') ? translateRole(m.role) : m.role;

            return `
                <div class="pulse-item">
                    <div class="pulse-info">
                        <div style="display:flex; align-items:center; gap:8px;">
                            ${m.user_name} ${titleBadge}
                        </div>
                        <span>Роль: ${displayRole}</span>
                    </div>
                    ${canEdit ? `<button onclick="openRoleDrawer(${m.user_id}, '${m.role}', '${title}')" class="btn-tiny">РОЛЬ</button>` : ''}
                </div>
            `;
        }).join('');
    } catch (e) { 
        alert("Сбой в JS при отрисовке: " + e.message);
        listEl.innerHTML = "Ошибка скрипта";
    }
}

function renderCompanyList() {
    document.getElementById('companyList').innerHTML = allMemberships.map(m => `
        <div class="pulse-item" onclick="selectCompany(${m.company_id})" style="border:${m.company_id == currentCompanyId ? '1px solid var(--accent)' : '1px solid var(--border)'}">
            <div>${m.company_name}</div>
        </div>
    `).join('');
}

function selectCompany(id) { localStorage.setItem('selected_company_id', id); location.reload(); }

function openDrawer(id) {
    // 1. Показываем оверлей
    const overlay = document.getElementById('overlay');
    overlay.classList.add('visible');
    
    // 2. Закрываем все предыдущие шторки
    document.querySelectorAll('.drawer').forEach(d => d.classList.remove('open'));
    
    // 3. Открываем нужную шторку
    const targetDrawer = document.getElementById('drawer_' + id);
    if (targetDrawer) {
        targetDrawer.classList.add('open');
    }
}

function closeDrawer() {
    // 1. Прячем оверлей
    document.getElementById('overlay').classList.remove('visible');
    
    // 2. Закрываем все шторки
    document.querySelectorAll('.drawer').forEach(d => d.classList.remove('open'));
}

// ===== НОВЫЕ ФУНКЦИИ: СОЗДАНИЕ БИЗНЕСА И ВСТУПЛЕНИЕ ПО КОДУ =====

async function createNewBusinessFromOnboarding() {
    const nameInput = document.getElementById('onboarding_biz_name');
    const name = nameInput ? nameInput.value.trim() : '';
    
    if (!name) {
        alert("Введите название бизнеса!");
        return;
    }

    try {
        const res = await fetch(API + '/companies', {
            method: 'POST',
            headers: { 
                'Content-Type': 'application/json',
                'X-Telegram-ID': String(userToken || "")
            },
            body: JSON.stringify({ name: name })
        });

        if (res.ok) {
            const data = await res.json();
            alert(`Бизнес "${name}" успешно создан!`);
            location.reload();
        } else {
            const errText = await res.text();
            alert("Ошибка создания: " + errText);
        }
    } catch (e) {
        alert("Ошибка сети");
        console.error(e);
    }
}

async function joinByCodeFromOnboarding() {
    const code = document.getElementById('onboarding_invite_code').value.trim();
    if (!code) return alert("Введите код!");

    // Берем ID напрямую из Telegram WebApp, если JS переменная пуста
    const tgId = currentUser?.tg_id || tg.initDataUnsafe?.user?.id;
    
    try {
        const res = await fetch(API + '/join', {
            method: 'POST',
            headers: { 
                'Content-Type': 'application/json',
                'X-Telegram-ID': String(tgId) // Явно передаем ID
            },
            body: JSON.stringify({ code: code })
        });

        if (res.ok) {
            alert("Успешно!");
            location.reload();
        } else {
            const err = await res.text();
            alert("Ошибка: " + err);
        }
    } catch (e) {
        alert("Сбой сети");
    }
}

async function createNewBusiness() {
    const nameInput = document.getElementById('new_biz_name');
    const name = nameInput ? nameInput.value.trim() : prompt("Введите название бизнеса:");
    
    if (!name) {
        alert("Название не может быть пустым!");
        return;
    }

    try {
        const res = await fetch(API + '/companies', {
            method: 'POST',
            headers: { 
                'Content-Type': 'application/json',
                'X-Telegram-ID': String(userToken || "")
            },
            body: JSON.stringify({ name: name })
        });

        if (res.ok) {
            const data = await res.json();
            alert(`Бизнес "${name}" успешно создан!`);
            // Перезагружаем страницу для обновления списка бизнесов
            location.reload();
        } else {
            const errText = await res.text();
            alert("Ошибка создания: " + errText);
        }
    } catch (e) {
        alert("Ошибка сети");
        console.error(e);
    }
}

async function joinByCode() {
    const code = prompt("Введите код для вступления:");
    if (!code) return;

    try {
        const res = await fetch(API + '/join', { 
            method: 'POST', 
            headers: getHeaders(), 
            body: JSON.stringify({ code: code }) // ТУТ КЛЮЧ: "code"
        });

        if (res.ok) {
            alert("Вы успешно присоединились!");
            location.reload();
        } else {
            const err = await res.text();
            alert("Ошибка: " + err); // Теперь мы увидим "код не найден"
        }
    } catch (e) { alert("Сбой сети"); }
}

// ===== ЛОГИКА ЗАЯВОК НА ЗАКУПКУ =====
let requestCart = [];

function addReqItemToCart() {
    const name = document.getElementById('req_name').value;
    const qty = parseFloat(document.getElementById('req_qty').value);
    const unit = document.getElementById('req_unit_display').value;
    const isUnlisted = document.getElementById('req_is_unlisted')?.value === 'true'; 

    if (!name || isNaN(qty) || qty <= 0) {
        alert("Выберите товар и укажите количество");
        return;
    }

    // ИЩЕМ ТОВАР В КАТАЛОГЕ И ДОСТАЕМ ПОСТАВЩИКА
    const product = catalog.find(p => p.name === name);
    let supplierName = "Без поставщика";
    if (isUnlisted) {
        supplierName = "⚠️ Товар не учтен в каталоге";
    } else if (product && product.supplier) {
        supplierName = product.supplier;
    }

    // Правильная проверка: есть ли уже товар в корзине?
    const existing = requestCart.find(i => i.position_name === name);
    if (existing) {
        existing.quantity += qty;
    } else {
        requestCart.push({ 
            position_name: name, 
            quantity: qty, 
            unit: unit, 
            is_unlisted: isUnlisted,
            supplier: supplierName // <-- ДОБАВИЛИ ПОСТАВЩИКА
        });
    }

    document.getElementById('req_search').value = '';
    document.getElementById('req_name').value = '';
    document.getElementById('req_qty').value = '';
    document.getElementById('req_unit_display').value = '';
    
    const warningEl = document.getElementById('req_ghost_warning');
    if (warningEl) warningEl.style.display = 'none';

    renderRequestCart();
}

function renderRequestCart() {
    const list = document.getElementById('requestCartList');
    const btn = document.getElementById('btnSubmitRequest');
    
    if (requestCart.length === 0) {
        list.innerHTML = '<div style="text-align:center; color:var(--text-muted); font-size:12px; margin: 10px 0;">Список пуст</div>';
        btn.style.display = 'none';
        return;
    }

    btn.style.display = 'block';

    // 1. Сохраняем оригинальные индексы массива, чтобы удаление работало корректно
    const itemsWithIndex = requestCart.map((item, index) => ({ ...item, originalIndex: index }));

    // 2. Группируем по поставщику
    const grouped = itemsWithIndex.reduce((acc, item) => {
        const supp = item.supplier || "Без поставщика"; // Если пусто, пишем дефолт
        if (!acc[supp]) acc[supp] = [];
        acc[supp].push(item);
        return acc;
    }, {});

    // 3. Отрисовываем группы
    list.innerHTML = Object.keys(grouped).map(supp => `
        <div style="margin-bottom: 15px; border-radius: 12px; border: 1px solid var(--border); overflow: hidden;">
            <!-- Заголовок группы (Поставщик) -->
            <div style="background: var(--surface); padding: 8px 12px; border-bottom: 1px solid var(--border); font-size: 10px; font-weight: 800; color: var(--accent); text-transform: uppercase; letter-spacing: 1px;">
                📦 ${supp}
            </div>
            
            <!-- Товары этого поставщика -->
            <div style="background: var(--bg); padding: 5px;">
                ${grouped[supp].map(item => `
                    <div class="pulse-item" style="padding: 8px 12px; margin-bottom: 4px; border: none; box-shadow: 0 1px 2px rgba(0,0,0,0.02);">
                        <div class="pulse-info">
                            <div>${item.position_name}</div>
                        </div>
                        <div style="display:flex; align-items:center; gap:10px;">
                            <div class="pulse-val">${item.quantity} ${item.unit}</div>
                            <!-- Используем originalIndex для точного удаления -->
                            <button class="btn-tiny" onclick="removeReqItem(${item.originalIndex})" style="color:#EF4444; border-color:#EF4444;">X</button>
                        </div>
                    </div>
                `).join('')}
            </div>
        </div>
    `).join('');
}

function removeReqItem(index) {
    requestCart.splice(index, 1);
    renderRequestCart();
}

async function submitProcurementRequest() {
    if (requestCart.length === 0) return;

    // ВАЖНО: Формируем явный массив данных
    const payload = {
        items: requestCart.map(item => ({
            position_name: item.position_name,
            quantity: parseFloat(item.quantity),
            unit: item.unit,
            is_unlisted: !!item.is_unlisted // Принудительно в boolean
        }))
    };

    try {
        const res = await fetch(API + '/procurements', {
            method: 'POST',
            headers: getHeaders(),
            body: JSON.stringify(payload)
        });

        if (res.ok) {
            alert("Заявка успешно отправлена!");
            requestCart = []; 
            renderRequestCart();
            closeDrawer();
            if (document.getElementById('nav-admin').classList.contains('active')) {
                loadPendingRequests(); 
            }
        } else {
            const errText = await res.text();
            alert("Ошибка от сервера: " + errText);
        }
    } catch (e) {
        alert("Ошибка сети");
    }
}

// Загрузка заявок для Админа
async function loadPendingRequests() {
    const listEl = document.getElementById('adminPendingRequests');
    if (!listEl) return;
    
    try {
        const res = await fetch(API + '/procurements?status=pending', { headers: getHeaders() });
        const requests = await res.json() || [];

        if (requests.length === 0) {
            listEl.innerHTML = '<div style="font-size:12px; color:var(--text-muted); padding-bottom: 15px;">Нет новых заявок</div>';
            return;
        }

        listEl.innerHTML = requests.map(req => {
            const itemsHtml = req.items.map(i => {
                const badge = i.is_unlisted ? ` <button class="btn-tiny" onclick="openDrawer('add_pos'); document.getElementById('p_name').value='${i.position_name}';">Добавить</button>` : '';
                return `• ${i.position_name}${badge}: <b>${i.quantity} ${i.unit}</b>`;
            }).join('<br>');
            return `
                <div class="pulse-item" style="display:block; margin-bottom:12px;">
                    <div style="display:flex; justify-content:space-between; margin-bottom:8px;">
                        <span style="font-size:10px; color:var(--text-muted);">${new Date(req.created_at).toLocaleDateString()}</span>
                        <span style="font-size:12px; font-weight:700;">От: ${req.user_name}</span>
                    </div>
                    <div style="font-size:13px; line-height:1.4; margin-bottom:10px;">${itemsHtml}</div>
                    <div style="display:flex; gap:10px;">
                        <button class="btn-main" style="padding:10px; font-size:12px; background:#10B981;" onclick="updateReqStatus(${req.id}, 'approved')">Одобрить</button>
                        <button class="btn-main" style="padding:10px; font-size:12px; background:#EF4444;" onclick="updateReqStatus(${req.id}, 'rejected')">Отклонить</button>
                    </div>
                </div>
            `;
        }).join('');
    } catch (e) { console.error(e); }
}

async function updateReqStatus(reqId, status) {
    if (!confirm(`Вы уверены, что хотите ${status === 'approved' ? 'ОДОБРИТЬ' : 'ОТКЛОНИТЬ'} заявку?`)) return;

    try {
        const res = await fetch(API + '/procurements/status', {
            method: 'PUT',
            headers: getHeaders(),
            body: JSON.stringify({ request_id: reqId, status: status })
        });

        if (res.ok) {
            loadPendingRequests();
        }
    } catch (e) {
        alert("Ошибка");
    }
}
async function loadGhostItemsForAdmin() {
    const section = document.getElementById('ghost_section');
    const container = document.getElementById('adminGhostList');
    
    try {
        const res = await fetch(API + '/unlisted', { headers: getHeaders() });
        const ghosts = await res.json() || [];
        
        if (ghosts.length === 0) {
            section.style.display = 'none'; // Скрываем всю секцию целиком
            return;
        }

        section.style.display = 'block'; // Показываем, если есть призраки
        container.innerHTML = ghosts.map(name => `
            <div class="pulse-item" style="border-left: 4px solid #D97706;">
                <div>${name}</div>
                <button class="btn-tiny" onclick="openAddPosWithGhost('${name}')">Добавить</button>
            </div>
        `).join('');
    } catch(e) { console.error(e); }
}
window.openRoleDrawer = (userId, currentRole, currentTitle) => {
    document.getElementById('target_user_id').value = userId;
    document.getElementById('new_role_select').value = currentRole;
    document.getElementById('new_title_input').value = currentTitle || '';
    openDrawer('change_role');
};

async function submitRoleChange() {
    const userId = document.getElementById('target_user_id').value;
    const role = document.getElementById('new_role_select').value;
    const title = document.getElementById('new_title_input').value;

    const res = await fetch(API + '/members', {
        method: 'PUT',
        headers: getHeaders(),
        body: JSON.stringify({ user_id: parseInt(userId), role: role, custom_title: title })
    });

    if (res.ok) {
        closeDrawer();
        loadTeamData();
    } else {
        alert("Ошибка прав доступа");
    }
}
async function removeMember() {
    const userId = document.getElementById('target_user_id').value;
    if(!confirm("Удалить участника из заведения?")) return;
    
    await fetch(API + `/members/${userId}`, { method: 'DELETE', headers: getHeaders() });
    closeDrawer();
    loadTeamData();
}
// Новая функция для подготовки шторки
window.openAddPosWithGhost = (name) => {
    openDrawer('add_pos'); 
    document.getElementById('p_name').value = name; // Подставляем имя автоматически
};
function translateRole(role) {
    const roles = { 'owner': 'Владелец', 'admin': 'Админ', 'manager': 'Менеджер', 'user': 'Сотрудник' };
    return roles[role] || role;
}
// Привязка к window
window.createNewBusiness = createNewBusiness;
window.createNewBusinessFromOnboarding = createNewBusinessFromOnboarding;
window.joinByCode = joinByCode;
window.joinByCodeFromOnboarding = joinByCodeFromOnboarding;
window.addReqItemToCart = addReqItemToCart;
window.removeReqItem = removeReqItem;
window.submitProcurementRequest = submitProcurementRequest;
window.updateReqStatus = updateReqStatus;
window.selectCompany = selectCompany;
window.openDrawer = openDrawer;
window.closeDrawer = closeDrawer;
window.switchTab = switchTab;
window.submitOperation = submitOperation;
window.submitNewPosition = submitNewPosition;
window.submitNewLocation = submitNewLocation;
window.filterSearch = filterSearch;
window.selectPos = selectPos;
window.selectGhostPos = selectGhostPos;
window.submitTransfer = submitTransfer;
window.copyInviteCode = () => {
    const code = document.getElementById('displayInviteCode').textContent;
    navigator.clipboard.writeText(code);
    tg.showAlert("Код скопирован");
};
