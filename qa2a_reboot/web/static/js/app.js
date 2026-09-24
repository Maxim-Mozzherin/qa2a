
// ============================================================================
// ONBOARDING WIZARD LOGIC

async function registerNewSupplierFromOnboarding() {
    const nameInput = document.getElementById("onboarding_supplier_name");
    const name = nameInput.value.trim();
    if (!name) {
        Telegram.WebApp.showAlert("Пожалуйста, введите название компании.");
        return;
    }
    const token = userToken;
    let res = await fetch("/api/supplier/register", { 
        method: "POST", 
        headers: { "X-Telegram-ID": token, "Content-Type": "application/json" },
        body: JSON.stringify({ company_name: name })
    });
    if (res.ok) {
        // Just reload the entire page to let initApp fetch the fresh is_supplier flag from DB
        localStorage.removeItem("skip_supplier");
        window.location.reload();
    } else {
        Telegram.WebApp.showAlert("Ошибка при создании компании.");
    }
}


// ============================================================================
function selectRole(role) {
    document.getElementById("role-card-restaurant").style.borderColor = role === "restaurant" ? "var(--primary)" : "transparent";
    document.getElementById("role-card-supplier").style.borderColor = role === "supplier" ? "var(--accent)" : "transparent";
    
    // Give a slight delay for animation before switching view
    setTimeout(() => {
        document.getElementById("onboarding-step-1").style.display = "none";
        if (role === "restaurant") {
            document.getElementById("onboarding-step-2-restaurant").style.display = "block";
            document.getElementById("onboarding-step-2-supplier").style.display = "none";
        } else {
            document.getElementById("onboarding-step-2-restaurant").style.display = "none";
            document.getElementById("onboarding-step-2-supplier").style.display = "block";
        }
    }, 150);
}

function resetRoleSelection() {
    document.getElementById("role-card-restaurant").style.borderColor = "transparent";
    document.getElementById("role-card-supplier").style.borderColor = "transparent";
    document.getElementById("onboarding-step-2-restaurant").style.display = "none";
    document.getElementById("onboarding-step-2-supplier").style.display = "none";
    document.getElementById("onboarding-step-1").style.display = "block";
}

async function joinSupplierByCode() {
    const codeInput = document.getElementById("onboarding_supplier_invite");
    const code = codeInput.value.trim();
    if (!code) {
        Telegram.WebApp.showAlert("Введите инвайт-код.");
        return;
    }
    const token = userToken;
    let res = await fetch("/api/supplier/join", { 
        method: "POST", 
        headers: { "X-Telegram-ID": token, "Content-Type": "application/json" },
        body: JSON.stringify({ code: code })
    });
    if (res.ok) {
        localStorage.removeItem("skip_supplier");
        window.location.reload();
    } else {
        const errData = await res.json();
        Telegram.WebApp.showAlert("Ошибка: " + (errData.error || "Неверный код"));
    }
}

// ============================================================================
async function openSupplierPortal() {
    localStorage.removeItem("skip_supplier");
    document.getElementById("onboarding").style.display = "none";
    document.getElementById("main-app").style.display = "none";
    document.getElementById("supplier-portal").style.display = "block";
    
    const token = userToken;
    document.getElementById("orgName").innerText = "Кабинет поставщика";
    
    let meRes = await fetch("/api/supplier/me", { headers: { "X-Telegram-ID": token } });
    if (meRes.ok) {
        const meData = await meRes.json();
        document.getElementById("orgName").innerText = meData.company_name;
        // Inject invite code into the analytics tab DOM dynamically if it exists
        window.supplierInviteCode = meData.invite_code;
    }

    let res = await fetch("/api/supplier/offers", { headers: { "X-Telegram-ID": token } });
    if (res.status === 401) {
        await fetch("/api/supplier/register", { method: "POST", headers: { "X-Telegram-ID": token } });
        res = await fetch("/api/supplier/offers", { headers: { "X-Telegram-ID": token } });
    }
    if (res.ok) {
        const offers = await res.json();
        renderSupplierOffers(offers || []);
    }
    switchSupplierTab("offers");
}

function closeSupplierPortal() {
    localStorage.setItem("skip_supplier", "true");
    document.getElementById("supplier-portal").style.display = "none";
    if (allMemberships.length > 0) {
        document.getElementById("main-app").style.display = "block";
        switchTab('request');
    } else {
        document.getElementById("onboarding").style.display = "block";
    }
}
// QA2A TELEGRAM WEBAPP - ОСНОВНОЙ КЛИЕНТСКИЙ СКРИПТ
// ============================================================================

const tg = window.Telegram?.WebApp || {};
const API = window.location.origin + '/api';

// Состояние приложения
let currentUser = null;
let userToken = null;
let currentCompanyId = localStorage.getItem('selected_company_id');
let allMemberships = [];
let catalog = [];
let locations = [];
let historyCache = [];
let requestCart = [];
let supplierContactsCache = [];
let currentProductSuppliers = [];
let currentInvAct = null;

// Состояние модуля графиков смен
let scheduleStaff = [];
let staffPreferences = {};
let generatedScheduleResult = null;

const DAYS_NAMES = ['Понедельник', 'Вторник', 'Среда', 'Четверг', 'Пятница', 'Суббота', 'Воскресенье'];

// Потребность смен по умолчанию (1 смена: утро, 2 смена: вечер)
let dailyDemand = [
    { shift1: 1, shift2: 2 },
    { shift1: 1, shift2: 2 },
    { shift1: 1, shift2: 2 },
    { shift1: 1, shift2: 2 },
    { shift1: 2, shift2: 3 },
    { shift1: 2, shift2: 3 },
    { shift1: 1, shift2: 2 }
];

// ============================================================================
// 1. УПРАВЛЕНИЕ ВИДИМОСТЬЮ НАВИГАЦИОННОЙ ПАНЕЛИ И КЛАВИАТУРОЙ
// ============================================================================

function hideNavBar() {
    const navBar = document.querySelector('.nav-bar');
    if (navBar) {
        navBar.style.setProperty('display', 'none', 'important');
    }
    document.body.classList.add('keyboard-open');
}

function showNavBar() {
    // Не показываем нижнюю панель, если открыта шторка или активен инпут
    const anyDrawerOpen = document.querySelector('.drawer.open');
    const active = document.activeElement;
    const isInputActive = active && ['INPUT', 'SELECT', 'TEXTAREA'].includes(active.tagName);

    if (anyDrawerOpen || isInputActive) {
        return;
    }

    const navBar = document.querySelector('.nav-bar');
    if (navBar) {
        navBar.style.removeProperty('display');
    }
    document.body.classList.remove('keyboard-open');
}

// Скрытие при фокусе в поля ввода
document.addEventListener('focusin', (e) => {
    if (['INPUT', 'SELECT', 'TEXTAREA'].includes(e.target.tagName)) {
        hideNavBar();
    }
});

// Плавное восстановление при потере фокуса
document.addEventListener('focusout', (e) => {
    setTimeout(() => {
        showNavBar();
        window.scrollTo(0, 0);
    }, 50);
});

// Закрытие выпадающих списков и клавиатуры при тапе вне элементов
document.addEventListener('touchstart', (e) => {
    if (!e.target.closest('#req_supplier_group')) {
        const divSup = document.getElementById('req_supplier_results');
        if (divSup) divSup.style.display = 'none';
    }

    // Закрытие общих подсказок поиска при клике вне поля
    if (!e.target.closest('.input-group')) {
        document.querySelectorAll('.search-results').forEach(el => el.style.display = 'none');
    }

    const active = document.activeElement;
    if (active && ['INPUT', 'SELECT', 'TEXTAREA'].includes(active.tagName)) {
        if (e.target.closest('.search-item') || e.target.closest('.btn-tiny') || e.target.closest('.btn-main')) return;
        const tag = e.target.tagName;
        if (!['INPUT', 'SELECT', 'TEXTAREA', 'BUTTON'].includes(tag)) {
            active.blur();
        }
    }
}, { passive: true });

// Двухфакторная автокоррекция дробных чисел (05 -> 0.5)
document.addEventListener('input', (e) => {
    const input = e.target;
    if (input.tagName === 'INPUT' && input.type === 'number') {
        const current = input.value;
        const prev = input._prevVal || "";
        if (/^0[1-9]/.test(current)) {
            input.value = '0.' + current.slice(1);
        } else if (prev === '0' && current.length === 1 && current >= '1' && current <= '9') {
            input.value = '0.' + current;
        }
        input._prevVal = input.value;
    }
});

document.addEventListener('focusin', (e) => {
    if (e.target.tagName === 'INPUT' && e.target.type === 'number') {
        e.target._prevVal = e.target.value;
    }
});

// ============================================================================
// 2. ПАСТЕЛЬНАЯ ЦВЕТОВАЯ ГАММА И ТЕМАТИЗАЦИЯ
// ============================================================================

function applyTelegramTheme() {
    const isDark = tg.colorScheme === 'dark';
    const root = document.documentElement;

    if (isDark) {
        // Элегантная темная пастельная палитра (Slate & Soft Periwinkle)
        root.style.setProperty('--bg', '#0F141C');
        root.style.setProperty('--surface', '#161D2A');
        root.style.setProperty('--text', '#E2E8F0');
        root.style.setProperty('--text-muted', '#8899A6');
        root.style.setProperty('--border', '#242F42');
        root.style.setProperty('--primary', '#E2E8F0');
        root.style.setProperty('--accent', '#6B8AFD');
    } else {
        // Мягкая теплая светлая пастельная палитра (Warm Stone & Pastel Indigo)
        root.style.setProperty('--bg', '#F5F6F8');
        root.style.setProperty('--surface', '#FFFFFF');
        root.style.setProperty('--text', '#2D3748');
        root.style.setProperty('--text-muted', '#718096');
        root.style.setProperty('--border', '#E2E8F0');
        root.style.setProperty('--primary', '#2D3748');
        root.style.setProperty('--accent', '#4F6EF7');
    }
}

if (tg.ready) {
    tg.ready();
    tg.expand();
    if (tg.requestFullscreen) tg.requestFullscreen();
}

document.addEventListener('DOMContentLoaded', initApp);

// ============================================================================
// 3. ИНИЦИАЛИЗАЦИЯ ПРИЛОЖЕНИЯ
// ============================================================================

async function initApp() {
    try {
        applyTelegramTheme();

        const initData = tg.initData || "";
        const reqData = initData ? { initData } : { demo_id: 999, demo_name: "Boss" };

        const res = await fetch(API + '/auth', {
            method: 'POST', 
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(reqData)
        });

        if (!res.ok) throw new Error("Сбой авторизации");

        const data = await res.json();
        currentUser = data.user;
        // КРИТИЧЕСКОЕ ИЗМЕНЕНИЕ: берем подписанный сервером токен, а не просто tg_id
        userToken = data.token; 
        allMemberships = data.memberships || [];


        if (data.is_supplier && localStorage.getItem("skip_supplier") !== "true") {
            openSupplierPortal();
            return;
        }

        if (allMemberships.length === 0) {
            document.getElementById("onboarding").style.display = "block";
            document.getElementById("main-app").style.display = "none";
            // Hide loading if any
            const l = document.getElementById("loading");
            if (l) l.style.display = "none";
        } else {

            const savedId = localStorage.getItem('selected_company_id');
            const found = allMemberships.find(m => m.company_id == savedId);
            currentCompanyId = found ? parseInt(savedId) : allMemberships[0].company_id;
            localStorage.setItem('selected_company_id', currentCompanyId);
            
            document.getElementById('onboarding').style.display = 'none';
            document.getElementById('main-app').style.display = 'block';
            
            const current = allMemberships.find(m => m.company_id == currentCompanyId);
            document.getElementById('orgName').textContent = current ? current.company_name : "Заведение";
            
            await loadAllData();
            renderCompanyList();
            switchTab('request');
        }
    } catch (e) {
        console.error("[initApp] Ошибка инициализации:", e);
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
             fetch(API + '/positions?t=' + Date.now(), { headers: getHeaders() }),
             fetch(API + '/locations?t=' + Date.now(), { headers: getHeaders() })
        ]);
        if (resP.ok) catalog = await resP.json() || [];
        if (resL.ok) {
            locations = await resL.json() || [];
            const opts = '<option value="">Выбрать склад...</option>' + 
                         locations.map(l => `<option value="${l.id}">${escapeHtml(l.name)}</option>`).join('');
            ['w_location', 'tr_location', 'tr_to_location', 'inv_new_location'].forEach(id => {
                const el = document.getElementById(id);
                if (el) el.innerHTML = opts;
            });
        }
        await loadAccounts();
    } catch (e) {
        console.error("[loadAllData] Ошибка загрузки справочников:", e);
    }
}

// ============================================================================
// 4. ПЕРЕКЛЮЧЕНИЕ ВКЛАДОК И МЕНЮ
// ============================================================================

function switchTab(id) {
    if (id === 'request') { if(typeof loadSpecialOffers === 'function') loadSpecialOffers(); }
    document.querySelectorAll('.tab-content').forEach(t => t.style.display = 'none');
    const target = document.getElementById('tab-' + id);
    if (target) target.style.display = 'block';
    
    document.querySelectorAll('.nav-item').forEach(i => i.classList.remove('active'));
    const activeNav = document.getElementById('nav-' + id);
    if (activeNav) activeNav.classList.add('active');
    
    if (id === 'admin') {
        loadAdminData();
    }

    if (id === 'writeoff' || id === 'transfer') {
        const prefix = id === 'writeoff' ? 'w' : 'tr';
        const dateInput = document.getElementById(prefix + '_date');
        const fakeInput = document.getElementById(prefix + '_date_fake');
        
        if (dateInput) {
            const now = new Date();
            const year = now.getFullYear();
            const month = String(now.getMonth() + 1).padStart(2, '0');
            const day = String(now.getDate()).padStart(2, '0');
            const hours = String(now.getHours()).padStart(2, '0');
            const minutes = String(now.getMinutes()).padStart(2, '0');
            const val = `${year}-${month}-${day}T${hours}:${minutes}`;
            
            dateInput.value = val;
            
            if (fakeInput) {
                fakeInput.textContent = now.toLocaleDateString('ru-RU') + ', ' + now.toLocaleTimeString('ru-RU', {hour: '2-digit', minute: '2-digit'});
                fakeInput.style.color = "var(--text)";
            }
        }
    }
}

function toggleSubMenu(menuId) {
    const menu = document.getElementById(menuId);
    const arrow = document.getElementById('arrow-' + menuId);
    if (!menu) return;

    if (menu.style.display === 'none' || menu.style.display === '') {
        menu.style.display = 'flex';
        if (arrow) arrow.textContent = '▴';
    } else {
        menu.style.display = 'none';
        if (arrow) arrow.textContent = '▾';
    }
}

// ============================================================================
// 5. ПОИСК И АВТОПОДБОР НОМЕНКЛАТУРЫ
// ============================================================================

function filterSearch(prefix) {
    const q = document.getElementById(prefix + '_search').value.toLowerCase();
    const div = document.getElementById(prefix + '_results');
    
    document.getElementById(prefix + '_name').value = '';
    const unitDisplay = document.getElementById(prefix + '_unit_display');
    if (unitDisplay) unitDisplay.value = '';
    
    const warningEl = document.getElementById(prefix + '_ghost_warning');
    if (warningEl) warningEl.style.display = 'none';

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

    // При открытии выпадающего списка скрываем навигационную панель
    hideNavBar();

    const searchWords = q.split(/\s+/).filter(word => word.length > 0);

    let matches = catalog.filter(p => {
        const nameLower = p.name.toLowerCase();
        return searchWords.every(word => nameLower.includes(word));
    });
    
    if (prefix === 'req') {
        matches = matches.filter(p => (p.type || "").toUpperCase().trim() === 'GOODS');
    }

    if (prefix === 'tr') {
        matches = matches.filter(p => {
            const t = (p.type || "").toUpperCase().trim();
            return t === 'GOODS' || t === 'PREPARED';
        });
    }

    if (prefix === 'w') {
        matches.sort((a, b) => {
            const typeA = (a.type || "").toUpperCase().trim();
            const typeB = (b.type || "").toUpperCase().trim();
            const getWeight = (type) => {
                if (type === 'PREPARED') return 1;
                if (type === 'GOODS') return 2;
                if (type === 'DISH') return 3;
                return 4;
            };
            return getWeight(typeA) - getWeight(typeB);
        });
    }
    
    let html = matches.map(p => {
        let badge = '';
        const t = (p.type || "").toUpperCase().trim();
        if (t === 'PREPARED') {
            badge = ` <span style="font-size: 8px; background: rgba(79, 110, 247, 0.15); color: var(--accent); padding: 2px 5px; border-radius: 4px; font-weight: 800; margin-left: 4px;">Заготовка</span>`;
        } else if (t === 'GOODS') {
            badge = ` <span style="font-size: 8px; background: rgba(72, 187, 120, 0.15); color: #48BB78; padding: 2px 5px; border-radius: 4px; font-weight: 800; margin-left: 4px;">Товар</span>`;
        } else if (t === 'DISH') {
            badge = ` <span style="font-size: 8px; background: rgba(245, 101, 101, 0.15); color: #F56565; padding: 2px 5px; border-radius: 4px; font-weight: 800; margin-left: 4px;">Блюдо</span>`;
        }

        let conceptionBadge = '';
        if (p.conception) {
            conceptionBadge = ` <span style="font-size: 8px; background: rgba(113, 128, 150, 0.12); color: var(--text-muted); padding: 2px 5px; border-radius: 4px; font-weight: bold; margin-left: 4px;">📍 ${escapeHtml(p.conception)}</span>`;
        }

        return `
            <div class="search-item" onclick="selectPos('${prefix}','${escapeHtml(p.name)}','${p.unit}')">
                ${escapeHtml(p.name)}${badge}${conceptionBadge} <small style="color:var(--text-muted)">(${p.unit})</small>
            </div>
        `;
    }).join('');

    html += `
        <div class="search-item" style="color: var(--accent); font-weight: 800; cursor: pointer;" onclick="selectGhostPos('${prefix}', '${escapeHtml(document.getElementById(prefix + '_search').value)}')">
            + Ввести вручную: "${escapeHtml(document.getElementById(prefix + '_search').value)}"
        </div>
    `;

    div.innerHTML = html;
    div.style.display = 'block';
}

async function selectPos(prefix, name, unit) {
    document.getElementById(prefix + '_name').value = name;
    document.getElementById(prefix + '_search').value = name;
    document.getElementById(prefix + '_results').style.display = 'none';
    
    const unitDisplay = document.getElementById(prefix + '_unit_display');
    if (unitDisplay) unitDisplay.value = unit; 

    let ghostFlag = document.getElementById(prefix + '_is_unlisted');
    if (ghostFlag) ghostFlag.value = 'false';

    const warningEl = document.getElementById(prefix + '_ghost_warning');
    if (warningEl) warningEl.style.display = 'none';

    showNavBar();

    if (prefix === 'req') {
        const product = catalog.find(p => p.name === name);
        const supplierSelectGroup = document.getElementById('req_supplier_group');
        const supplierSearchInput = document.getElementById('req_supplier_search');

        if (product && product.external_id) {
            if (supplierSelectGroup) supplierSelectGroup.style.display = 'block';
            if (supplierSearchInput) {
                try {
                    const currentSelectionUUID = document.getElementById('req_supplier_uuid').value;
                    supplierSearchInput.placeholder = '⏳ Поиск поставщиков...';

                    const res = await fetch(API + `/positions/${product.external_id}/suppliers`, { headers: getHeaders() });
                    if (res.ok) {
                        const data = await res.json() || { is_fallback: true, suppliers: [] };
                        const isFallback = data.is_fallback;
                        currentProductSuppliers = data.suppliers || [];

                        if (currentProductSuppliers.length === 0) {
                            selectSupplier('DIRECT', '📦 Разовая закупка (Поставщик не привязан)', '');
                        } else {
                            let targetSupplier = currentProductSuppliers[0];
                            if (isFallback && currentSelectionUUID) {
                                const matched = currentProductSuppliers.find(s => s.uuid === currentSelectionUUID);
                                if (matched) targetSupplier = matched;
                            }
                            selectSupplier(targetSupplier.uuid, targetSupplier.name, targetSupplier.tg_username || '');
                        }
                        supplierSearchInput.placeholder = 'Начните вводить имя поставщика...';
                    }
                } catch (e) {
                    selectSupplier('DIRECT', '📦 Обычная закупка', '');
                }
            }
        } else {
            if (supplierSelectGroup) supplierSelectGroup.style.display = 'none';
        }
    }
}

function selectGhostPos(prefix, name) {
    const unit = prompt(`Укажите единицу измерения для "${name}"\n(например: шт, кг, л, упак):`);
    if (!unit) return; 

    document.getElementById(prefix + '_name').value = name;
    document.getElementById(prefix + '_search').value = name + ' (Введено вручную)';
    document.getElementById(prefix + '_results').style.display = 'none';
    
    const unitDisplay = document.getElementById(prefix + '_unit_display');
    if (unitDisplay) unitDisplay.value = unit;

    let ghostFlag = document.getElementById(prefix + '_is_unlisted');
    if (ghostFlag) ghostFlag.value = 'true';

    const warningEl = document.getElementById(prefix + '_ghost_warning');
    if (warningEl) warningEl.style.display = 'block';

    showNavBar();
}

function filterSupplierSearch() {
    const q = document.getElementById('req_supplier_search').value.toLowerCase();
    const div = document.getElementById('req_supplier_results');
    
    if (q.length < 1) {
        div.style.display = 'none';
        return;
    }

    hideNavBar();

    const searchWords = q.split(/\s+/).filter(word => word.length > 0);
    const matches = currentProductSuppliers.filter(s => {
        const nameLower = s.name.toLowerCase();
        return searchWords.every(word => nameLower.includes(word));
    });

    div.innerHTML = matches.map(s => `
        <div class="search-item" onclick="selectSupplier('${s.uuid}', '${escapeHtml(s.name)}', '${s.tg_username || ''}')">
            🌟 ${escapeHtml(s.name)} ${s.tg_username ? '(@' + s.tg_username + ')' : '(Без TG)'}
        </div>
    `).join('');
    div.style.display = 'block';
}

function selectSupplier(uuid, name, tgUser) {
    const searchEl = document.getElementById('req_supplier_search');
    const uuidEl = document.getElementById('req_supplier_uuid');
    const nameEl = document.getElementById('req_supplier_name');
    const tgEl = document.getElementById('req_supplier_tg');
    const resultsEl = document.getElementById('req_supplier_results');

    if (searchEl) searchEl.value = name;
    if (uuidEl) uuidEl.value = uuid;
    if (nameEl) nameEl.value = name;
    if (tgEl) tgEl.value = tgUser;
    if (resultsEl) resultsEl.style.display = 'none';

    showNavBar();
}

// ============================================================================
// 6. ОПЕРАЦИИ (СПИСАНИЕ / ПЕРЕМЕЩЕНИЕ)
// ============================================================================

async function submitOperation(type, prefix) {
    const name = document.getElementById(prefix + '_name').value;
    const qty = document.getElementById(prefix + '_qty').value;
    const locId = document.getElementById(prefix + '_location')?.value;
    const toLocId = document.getElementById(prefix + '_to_location')?.value;
    const unit = document.getElementById(prefix + '_unit_display')?.value || '';
    const isUnlisted = document.getElementById(prefix + '_is_unlisted')?.value === 'true';
    const accountId = document.getElementById(prefix + '_account_id')?.value || "";
    let opDate = document.getElementById(prefix + '_date')?.value || "";
    let comment = document.getElementById(prefix + '_comment')?.value || "";

    if (!name || !qty || parseFloat(qty) <= 0) { 
        alert("Выберите товар и укажите корректное количество!"); 
        return; 
    }
    if (!locId) { 
        alert("Пожалуйста, выберите склад!"); 
        return; 
    }

    if (type === 'transfer') {
        if (!toLocId) { alert("Выберите склад-получатель!"); return; }
        if (locId === toLocId) { alert("Склады отправления и назначения должны различаться!"); return; }
    }

    if (isUnlisted && type === 'writeoff' && !comment.trim()) {
        comment = prompt(`Вы списываете товар вне каталога ("${name}").\nУкажите причину (обязательно):`);
        if (!comment) {
            alert("Для неучтенного товара причина обязательна!");
            return;
        }
        const commentInput = document.getElementById('w_comment');
        if (commentInput) commentInput.value = comment;
    }
    if (opDate.length === 10) {
        opDate += "T12:00";
    }

    const btn = document.querySelector(prefix === 'w' ? '#tab-writeoff .btn-main' : '#tab-transfer .btn-main');
    if (btn) {
        btn.disabled = true;
        btn.dataset.oldText = btn.textContent;
        btn.textContent = "⏳ Отправка...";
    }

    const body = {
        position_name: name,
        quantity: parseFloat(qty),
        unit: unit,
        type: type,
        location_id: parseInt(locId),
        is_unlisted: isUnlisted,
        comment: comment,
        account_id: accountId,
        date: opDate
    };

    if (type === 'transfer') body.to_location_id = parseInt(toLocId);

    try {
        const res = await fetch(API + '/operations', { 
            method: 'POST', 
            headers: getHeaders(), 
            body: JSON.stringify(body) 
        });

        if (res.ok) {
            if (tg.HapticFeedback) tg.HapticFeedback.notificationOccurred('success');
            if (tg.showAlert) tg.showAlert("Успешно проведено!"); else alert("Успешно!");
            
            document.getElementById(prefix + '_search').value = '';
            document.getElementById(prefix + '_name').value = '';
            document.getElementById(prefix + '_qty').value = '';
            document.getElementById(prefix + '_unit_display').value = '';
            
            const commentInput = document.getElementById(prefix + '_comment');
            if (commentInput) commentInput.value = '';

            const warningEl = document.getElementById(prefix + '_ghost_warning');
            if (warningEl) warningEl.style.display = 'none';
        } else {
            const err = await res.json().catch(() => ({ error: "Ошибка сервера" }));
            alert("Ошибка: " + (err.error || err.message || "Сбой"));
        }
    } catch (e) { 
        alert("Сбой сети при отправке операции"); 
    } finally {
        if (btn) {
            btn.disabled = false;
            btn.textContent = btn.dataset.oldText;
        }
    }
}

// ============================================================================
// 7. ЗАЯВКИ НА ЗАКУПКУ (PROCUREMENT)
// ============================================================================

function addReqItemToCart() {
    const name = document.getElementById('req_name').value;
    const qty = parseFloat(document.getElementById('req_qty').value);
    const unit = document.getElementById('req_unit_display').value;
    const isUnlisted = document.getElementById('req_is_unlisted')?.value === 'true'; 

    if (!name || isNaN(qty) || qty <= 0) {
        return alert("Выберите товар и укажите положительное количество!");
    }

    let supplierUUID = document.getElementById('req_supplier_uuid').value || "DIRECT";
    let supplierName = document.getElementById('req_supplier_name').value || "Без поставщика (Разовый)";
    let supplierTg = document.getElementById('req_supplier_tg').value || "";

    if (isUnlisted) {
        supplierName = "⚠️ Не учтен в каталоге";
    }

    const existing = requestCart.find(i => i.position_name === name && i.supplier_uuid === supplierUUID);
    if (existing) {
        existing.quantity += qty;
    } else {
        requestCart.push({ 
            position_name: name, 
            quantity: qty, 
            unit: unit, 
            is_unlisted: isUnlisted, 
            supplier_uuid: supplierUUID,
            supplier_name: supplierName,
            supplier_tg: supplierTg
        });
    }

    document.getElementById('req_search').value = '';
    document.getElementById('req_name').value = '';
    document.getElementById('req_qty').value = '';
    document.getElementById('req_unit_display').value = '';
    
    renderRequestCart();
}

function renderRequestCart() {
    const list = document.getElementById('requestCartList');
    const btn = document.getElementById('btnSubmitRequest');
    
    if (requestCart.length === 0) {
        list.innerHTML = '<div style="text-align:center; color:var(--text-muted); font-size:12px; margin: 10px 0;">Список закупки пуст</div>';
        btn.style.display = 'none';
        return;
    }
    btn.style.display = 'block';

    const grouped = requestCart.reduce((acc, item) => {
        if (!acc[item.supplier_name]) acc[item.supplier_name] = [];
        acc[item.supplier_name].push(item);
        return acc;
    }, {});

    list.innerHTML = Object.keys(grouped).map(suppName => {
        const tgUser = grouped[suppName][0].supplier_tg;
        const tgBadge = tgUser ? ` <span style="font-size:10px; color:#48BB78; font-weight:bold;">@${tgUser}</span>` : ' <span style="font-size:10px; color:var(--text-muted);">[Без TG]</span>';
        return `
            <div style="margin-bottom: 15px; border-radius: 14px; border: 1px solid var(--border); overflow: hidden;">
                <div style="background: var(--surface); padding: 8px 12px; border-bottom: 1px solid var(--border); font-size: 10px; font-weight: 800; color: var(--accent); text-transform: uppercase; display:flex; justify-content:space-between; align-items:center;">
                    <span>📦 ${escapeHtml(suppName)}</span>
                    ${tgBadge}
                </div>
                <div style="background: var(--bg); padding: 5px;">
                    ${grouped[suppName].map(item => {
                        const originalIdx = requestCart.findIndex(x => x.position_name === item.position_name && x.supplier_uuid === item.supplier_uuid);
                        return `
                            <div class="pulse-item" style="padding: 8px 12px; margin-bottom: 4px; border: none;">
                                <div class="pulse-info"><div>${escapeHtml(item.position_name)}</div></div>
                                <div style="display:flex; align-items:center; gap:10px;">
                                    <div class="pulse-val">${item.quantity} ${item.unit}</div>
                                    <button type="button" class="btn-tiny" onclick="removeReqItem(${originalIdx})" style="color:#F56565; border-color:#F56565;">✕</button>
                                </div>
                            </div>
                        `;
                    }).join('')}
                </div>
            </div>
        `;
    }).join('');
}

function removeReqItem(index) { 
    requestCart.splice(index, 1); 
    renderRequestCart(); 
}

async function submitProcurementRequest() {
    if (requestCart.length === 0) return;

    const btn = document.getElementById('btnSubmitRequest');
    if (btn) {
        btn.disabled = true;
        btn.dataset.oldText = btn.textContent;
        btn.textContent = "⏳ Отправка...";
    }

    const payload = { 
        items: requestCart.map(i => ({ 
            position_name: i.position_name, 
            quantity: parseFloat(i.quantity), 
            unit: i.unit, 
            is_unlisted: !!i.is_unlisted,
            supplier: i.supplier_uuid + "|" + i.supplier_name 
        })) 
    };

    try {
        const res = await fetch(API + '/procurements', { 
            method: 'POST', 
            headers: getHeaders(), 
            body: JSON.stringify(payload) 
        });
        
        if (res.ok) {
            if (tg.showAlert) tg.showAlert("Заявка успешно отправлена на согласование!"); else alert("Заявка отправлена!");
            requestCart = []; 
            renderRequestCart();
        } else { 
            alert("Ошибка при отправке: " + await res.text()); 
        }
    } catch (e) { 
        alert("Ошибка сети при отправке заявки"); 
    } finally {
        if (btn) {
            btn.disabled = false;
            btn.textContent = btn.dataset.oldText;
        }
    }
}

async function openActiveRequests() {
    openDrawer('active_requests');
    const listEl = document.getElementById('activePendingRequestsList');
    listEl.innerHTML = "Загрузка...";

    const myMembership = allMemberships.find(m => m.company_id == currentCompanyId);
    const canApprove = myMembership && ['owner', 'admin', 'manager'].includes(myMembership.role);

    try {
        const res = await fetch(API + '/procurements?status=pending', { headers: getHeaders() });
        const requests = await res.json() || [];
        if (requests.length === 0) { 
            listEl.innerHTML = '<div style="text-align:center; color:var(--text-muted); padding: 20px 0; font-size:12px;">Нет активных заявок на согласовании</div>'; 
            return; 
        }
        listEl.innerHTML = requests.map(req => {
            const itemsHtml = req.items.map(i => `• ${escapeHtml(i.position_name)}: <b>${i.quantity} ${i.unit}</b>`).join('<br>');
            let actionHtml = canApprove 
                ? `<div style="display:flex; gap:10px; margin-top:10px;"><button type="button" class="btn-main" style="padding:10px; font-size:12px; background:#48BB78; margin:0;" onclick="updateReqStatus(${req.id}, 'approved')">Одобрить</button><button type="button" class="btn-main" style="padding:10px; font-size:12px; background:#F56565; margin:0;" onclick="updateReqStatus(${req.id}, 'rejected')">Отклонить</button></div>` 
                : `<div style="font-size:11px; color:#E6983A; font-style:italic; margin-top:5px;">⏳ Ожидает подтверждения руководством</div>`;
            return `
                <div class="pulse-item" style="display:block; margin-bottom:12px; padding:15px;">
                    <div style="display:flex; justify-content:space-between; margin-bottom:8px;">
                        <span style="font-size:10px; color:var(--text-muted);">${new Date(req.created_at).toLocaleDateString('ru-RU')}</span>
                        <span style="font-size:12px; font-weight:800; color:var(--accent);">Создал: ${escapeHtml(req.user_name)}</span>
                    </div>
                    <div style="font-size:13px; line-height:1.4;">${itemsHtml}</div>
                    ${actionHtml}
                </div>
            `;
        }).join('');
    } catch (e) {
        listEl.innerHTML = '<div style="color:#F56565; font-size:12px; text-align:center; padding: 20px 0;">Ошибка загрузки</div>';
    }
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
            if (tg.HapticFeedback) tg.HapticFeedback.notificationOccurred('success');
            closeDrawer(); 
            if (status === 'approved') {
                orderApprovedRequest(reqId);
            } else {
                loadAdminData();
            }
        } else {
            alert("Ошибка изменения статуса");
        }
    } catch (e) { alert("Сетевой сбой"); }
}

async function loadApprovedRequests() {
    const container = document.getElementById('adminApprovedRequests');
    if (!container) return;
    
    const myMembership = allMemberships.find(m => m.company_id == currentCompanyId);
    const canSend = myMembership && ['owner', 'admin', 'manager'].includes(myMembership.role);

    try {
        const res = await fetch(API + '/procurements?status=approved', { headers: getHeaders() });
        const requests = await res.json() || [];
        if (requests.length === 0) { 
            container.innerHTML = '<div style="font-size:12px; color:var(--text-muted); padding: 15px 0;">Архив пуст</div>'; 
            return; 
        }
        
        container.innerHTML = requests.map(req => `
            <div class="pulse-item" style="display:flex; justify-content:space-between; align-items:center;">
                <div class="pulse-info">
                    <div style="font-weight:700;">Заявка #${req.id}</div>
                    <span style="font-size:11px;">${new Date(req.created_at).toLocaleDateString('ru-RU')} • Автор: ${escapeHtml(req.user_name)}</span>
                </div>
                <div style="display:flex; gap:6px;">
                    <button type="button" class="btn-tiny" onclick="downloadPDF(${req.id})" style="border-color:var(--text-muted); color:var(--text-muted);">PDF</button>
                    ${canSend ? `
                        <button type="button" class="btn-tiny" onclick="orderApprovedRequest(${req.id})" style="background:#48BB78; color:white; border-color:#48BB78;">📝 Заказать</button>
                    ` : ''}
                </div>
            </div>
        `).join('');
    } catch (e) {
        console.error("Approved Requests Error:", e);
    }
}

async function orderApprovedRequest(reqID) {
    try {
        const res = await fetch(API + `/procurements/${reqID}/suppliers`, { headers: getHeaders() });
        if (!res.ok) throw new Error("Ошибка получения поставщиков");
        
        const items = await res.json() || [];
        if (items.length === 0) {
            alert("В заявке нет позиций для заказа!");
            return;
        }

        const container = document.getElementById('procurement_results_content');
        const orgName = document.getElementById('orgName').textContent;
        const dateStr = new Date().toLocaleDateString('ru-RU');

        const grouped = items.reduce((acc, item) => {
            const sName = item.supplier_name || "Без поставщика (Разовый)";
            if (!acc[sName]) acc[sName] = { tg: item.tg_username, items: [] };
            acc[sName].items.push(item);
            return acc;
        }, {});

        let html = "";

        Object.keys(grouped).forEach((suppName, idx) => {
            const supp = grouped[suppName];
            let textBlock = `Заявка на закупку\n`;
            textBlock += `Кому: ${suppName}\n`;
            textBlock += `Отправитель: ${orgName}\n`;
            textBlock += `Дата: ${dateStr}\n`;
            textBlock += `-----------------------\n`;
            
            supp.items.forEach((it, i) => {
                textBlock += `${i+1}. ${it.position_name} — ${it.quantity} ${it.unit}\n`;
            });
            
            textBlock += `-----------------------\n`;
            textBlock += `Пожалуйста, подтвердите принятие заказа.`;

            const textareaId = `order_text_${idx}`;

            html += `
                <div style="background:var(--surface); border:1px solid var(--border); border-radius:16px; padding:15px; margin-bottom:15px;">
                    <div style="font-weight:800; font-size:14px; margin-bottom:10px; color:var(--accent);">📋 Заказ для: ${escapeHtml(suppName)}</div>
                    <textarea id="${textareaId}" readonly style="width:100%; height:130px; font-family:monospace; font-size:12px; background:var(--bg); color:var(--text); border:1px solid var(--border); padding:10px; border-radius:10px; resize:none;">${textBlock}</textarea>
                    
                    <div style="display:flex; gap:10px; margin-top:10px;">
                        <button type="button" class="btn-main" onclick="copyOrderText('${textareaId}')" style="flex:1; font-size:12px; padding:12px; background:var(--bg); color:var(--text); border:1px solid var(--border);">📋 Копировать</button>
                        ${supp.tg ? `
                            <button type="button" class="btn-main" onclick="sendOrderToTg('${textareaId}', '${supp.tg}')" style="flex:1; font-size:12px; padding:12px; background:#48BB78;">🚀 В чат</button>
                        ` : `
                            <button type="button" class="btn-main" disabled style="flex:1; font-size:12px; padding:12px; background:var(--border); color:var(--text-muted); cursor:not-allowed;">[Нет TG]</button>
                        `}
                    </div>
                </div>
            `;
        });

        container.innerHTML = html;
        openDrawer('procurement_results');
    } catch (e) {
        alert("Ошибка при обработке заявки: " + e.message);
    }
}

function copyOrderText(id) {
    const el = document.getElementById(id);
    if (!el) return;
    el.select();
    navigator.clipboard.writeText(el.value);
    if (tg.HapticFeedback) tg.HapticFeedback.notificationOccurred('success');
    if (tg.showAlert) tg.showAlert("Текст скопирован в буфер обмена!"); else alert("Скопировано!");
}

function sendOrderToTg(id, tgUsername) {
    const el = document.getElementById(id);
    if (!el) return;
    navigator.clipboard.writeText(el.value);
    if (tg.HapticFeedback) tg.HapticFeedback.notificationOccurred('success');
    
    let username = tgUsername.trim();
    if (username.startsWith('@')) username = username.substring(1);
    if (!username) return alert("У поставщика не указан TG!");
    
    const tgUrl = `https://t.me/${username}`;
    if (tg.openTelegramLink) tg.openTelegramLink(tgUrl); else window.open(tgUrl, '_blank');
}

function downloadPDF(id) {
    const url = `${API}/procurements/download/${id}?tg_id=${userToken}&c_id=${currentCompanyId}`;
    if (tg.openLink) tg.openLink(url); else window.open(url, '_blank'); 
}

// ============================================================================
// 8. ПАНЕЛЬ УПРАВЛЕНИЯ (АДМИНКА)
// ============================================================================

async function loadAdminData() {
    loadApprovedRequests();
    loadIikoSettings();
    loadHistory();

    // Загрузка инвайт-кода
    try {
        const resC = await fetch(API + '/invite-code', { headers: getHeaders() });
        if (resC.ok) {
            const dataC = await resC.json();
            document.getElementById('displayInviteCode').textContent = dataC.code || "---";
        }
    } catch (e) {
        console.error("Invite Code Load Error:", e);
    }

    // Загрузка участников команды
    try {
        const resM = await fetch(API + '/members', { headers: getHeaders() });
        const listEl = document.getElementById('memberList');
        if (resM.ok && listEl) {
            const members = await resM.json() || [];
            const myMembership = allMemberships.find(m => m.company_id == currentCompanyId);
            const myRole = myMembership?.role; 
            listEl.innerHTML = members.map(m => {
                const title = m.custom_title || '';
                const titleBadge = title ? `<span class="role-badge">${escapeHtml(title)}</span>` : '';
                const canEdit = (myRole === 'owner' || myRole === 'manager' || myRole === 'admin') && m.user_id != currentUser.id;
                return `
                    <div class="pulse-item">
                        <div class="pulse-info">
                            <div style="display:flex; align-items:center; gap:8px;">${escapeHtml(m.user_name)} ${titleBadge}</div>
                            <span>Роль: ${translateRole(m.role)}</span>
                        </div>
                        ${canEdit ? `<button type="button" onclick="openRoleDrawer(${m.user_id}, '${m.role}', '${escapeHtml(title)}')" class="btn-tiny">Настроить</button>` : ''}
                    </div>
                `;
            }).join('');
        }
    } catch (e) {
        console.error("Members Load Error:", e);
    }
}

// ============================================================================
// 9. ИСТОРИЯ ДВИЖЕНИЙ И РЕДАКТИРОВАНИЕ СПИСАНИЙ
// ============================================================================

async function loadHistory() {
    const listEl = document.getElementById('historyFeed');
    if (!listEl) return;
    try {
        const res = await fetch(API + '/operations', { headers: getHeaders() });
        const rawData = await res.json() || [];
        
        if (rawData.length === 0) { 
            listEl.innerHTML = '<div style="text-align:center; color:var(--text-muted); font-size:12px; padding: 20px 0;">История пуста</div>'; 
            return; 
        }

        let processed = [];
        let usedIDs = new Set();

        for (let i = 0; i < rawData.length; i++) {
            let op = rawData[i];
            if (!op || usedIDs.has(op.id)) continue;

            if (['transfer_in', 'transfer_out', 'assembly_in', 'assembly_out'].includes(op.type)) {
                let pairType = '';
                if (op.type === 'transfer_in') pairType = 'transfer_out';
                else if (op.type === 'transfer_out') pairType = 'transfer_in';
                else if (op.type === 'assembly_in') pairType = 'assembly_out';
                else if (op.type === 'assembly_out') pairType = 'assembly_in';

                let pair = rawData.find(p => 
                    p.id !== op.id && 
                    !usedIDs.has(p.id) && 
                    p.type === pairType && 
                    p.position_name === op.position_name &&
                    Math.abs(p.quantity) === Math.abs(op.quantity) &&
                    Math.abs(parseLocalDate(p.created_at) - parseLocalDate(op.created_at)) < 60000
                );

                if (pair) {
                    const isAssembly = op.type.startsWith('assembly');
                    const opOut = op.type.endsWith('_out') ? op : pair;
                    const opIn = op.type.endsWith('_in') ? op : pair;

                    processed.push({
                        ...opIn, 
                        display_type: isAssembly ? 'Приготовление' : 'Перемещение',
                        real_qty: Math.abs(opIn.quantity),
                        from_loc: opOut.location_id,
                        to_loc: opIn.location_id,
                        comment: opIn.comment || opOut.comment || ""
                    });

                    usedIDs.add(op.id);
                    usedIDs.add(pair.id);
                    continue;
                }
            }

            const typeLabels = {
                'writeoff': 'Списание',
                'incoming_invoice': 'Приход',
                'transfer_in': 'Перемещение (Приход)',
                'transfer_out': 'Перемещение (Расход)',
                'assembly_in': 'Приготовление (Приход)',
                'assembly_out': 'Приготовление (Расход)'
            };

            op.display_type = typeLabels[op.type] || op.type;
            op.real_qty = op.type === 'writeoff' || op.type.endsWith('_out') ? -Math.abs(op.quantity) : op.quantity;
            processed.push(op);
            usedIDs.add(op.id);
        }

        historyCache = processed;

        listEl.innerHTML = processed.map((op, index) => {
            const isNegative = op.real_qty < 0;
            const isTransfer = op.display_type.startsWith('Перемещение') || op.display_type.startsWith('Приготовление');
            let color = isTransfer ? 'var(--accent)' : (isNegative ? '#F56565' : '#48BB78');
            let sign = (!isNegative && !isTransfer && op.real_qty > 0) ? '+' : '';

            let iikoBadge = '';
            if (['writeoff', 'transfer_in', 'transfer_out', 'assembly_in', 'assembly_out'].includes(op.type)) {
                if (op.exported_to_iiko) {
                    iikoBadge = `<span style="font-size: 8px; background: rgba(72, 187, 120, 0.12); color: #48BB78; padding: 2px 5px; border-radius: 4px; margin-left: 5px; font-weight: 800;">iiko</span>`;
                } else {
                    iikoBadge = `<span style="font-size: 8px; background: rgba(230, 152, 58, 0.15); color: #E6983A; padding: 2px 5px; border-radius: 4px; margin-left: 5px; font-weight: 800;">🕒 Ожидает</span>`;
                }
            }

            return `
                <div class="pulse-item" style="cursor:pointer;" onclick="openOperationDetails(${index})">
                    <div class="pulse-info">
                        <div style="display:flex; align-items:center; flex-wrap:wrap; gap:4px;">
                            ${escapeHtml(op.position_name)} 
                            <small style="color:var(--text-muted); font-weight:normal;">(${op.display_type})</small>
                            ${iikoBadge}
                        </div>
                        <span>${escapeHtml(op.user_name)} • ${parseLocalDate(op.created_at).toLocaleTimeString([], {hour:'2-digit', minute:'2-digit'})}</span>
                    </div>
                    <div class="pulse-val" style="color:${color}">
                        ${sign}${op.real_qty} ${op.unit}
                    </div>
                </div>
            `;
        }).join('');
    } catch (e) { 
        listEl.innerHTML = '<div style="text-align:center; color:#F56565; font-size:12px;">Ошибка загрузки истории</div>';
    }
}

function openOperationDetails(idx) {
    const op = historyCache[idx];
    if (!op) return;

    const content = document.getElementById('op_details_content');
    const getLocName = id => { const l = locations.find(x => x.id === id); return l ? l.name : 'Неизвестный склад'; };
    
    const dateObj = parseLocalDate(op.created_at);
    const dateStr = dateObj.toLocaleDateString('ru-RU') + ' ' + dateObj.toLocaleTimeString([], {hour:'2-digit', minute:'2-digit'});

    let html = `
        <div style="background:var(--surface); padding:15px; border-radius:14px; border:1px solid var(--border);">
            <h4 style="margin-top:0; margin-bottom:15px; color:var(--accent); font-size:16px;">${escapeHtml(op.position_name)}</h4>
            
            <div style="margin-bottom:10px; font-size:13px; display:flex; justify-content:space-between;">
                <span style="color:var(--text-muted)">Операция:</span>
                <b>${op.display_type}</b>
            </div>
            <div style="margin-bottom:10px; font-size:13px; display:flex; justify-content:space-between;">
                <span style="color:var(--text-muted)">Количество:</span>
                <b>${Math.abs(op.real_qty)} ${op.unit}</b>
            </div>
            <div style="margin-bottom:10px; font-size:13px; display:flex; justify-content:space-between;">
                <span style="color:var(--text-muted)">Сотрудник:</span>
                <b>${escapeHtml(op.user_name)}</b>
            </div>
            <div style="margin-bottom:10px; font-size:13px; display:flex; justify-content:space-between;">
                <span style="color:var(--text-muted)">Дата и время:</span>
                <b>${dateStr}</b>
            </div>
    `;

    if (op.display_type.startsWith('Перемещение') || op.display_type.startsWith('Приготовление')) {
        const fromLabel = op.display_type.startsWith('Приготовление') ? 'Компоненты:' : 'Откуда:';
        const toLabel = op.display_type.startsWith('Приготовление') ? 'Готовый ПФ:' : 'Куда:';
        
        html += `
            <div style="margin-bottom:10px; font-size:13px; display:flex; justify-content:space-between;">
                <span style="color:var(--text-muted)">${fromLabel}</span>
                <b>${escapeHtml(getLocName(op.from_loc))}</b>
            </div>
            <div style="margin-bottom:10px; font-size:13px; display:flex; justify-content:space-between;">
                <span style="color:var(--text-muted)">${toLabel}</span>
                <b>${escapeHtml(getLocName(op.to_loc))}</b>
            </div>
        `;
    } else {
        html += `
            <div style="margin-bottom:10px; font-size:13px; display:flex; justify-content:space-between;">
                <span style="color:var(--text-muted)">Склад:</span>
                <b>${escapeHtml(getLocName(op.location_id))}</b>
            </div>
        `;
    }

    if (op.comment) {
        html += `
            <div style="margin-top:15px; padding-top:12px; border-top:1px solid var(--border); font-size:13px;">
                <span style="color:var(--text-muted); display:block; margin-bottom:5px;">Комментарий:</span>
                <i>${escapeHtml(op.comment)}</i>
            </div>
        `;
    }

    if (op.type === 'writeoff' && !op.exported_to_iiko) {
        html += `
            <div style="margin-top:20px;">
                <button type="button" class="btn-main" onclick="openEditWriteoffForm(${idx})" style="background:var(--accent);">✏️ Редактировать списание</button>
            </div>
        `;
    }

    html += `</div>`;
    content.innerHTML = html;
    openDrawer('op_details');
}

function openEditWriteoffForm(idx) {
    const op = historyCache[idx];
    if (!op) return;

    document.getElementById('edit_op_id').value = op.id;
    document.getElementById('edit_w_search').value = op.position_name;
    document.getElementById('edit_w_qty').value = Math.abs(op.quantity);
    document.getElementById('edit_w_unit_display').value = op.unit;
    document.getElementById('edit_w_comment').value = op.comment || '';

    const srcLoc = document.getElementById('w_location');
    const dbLoc = document.getElementById('edit_w_location');
    if (srcLoc && dbLoc) {
        dbLoc.innerHTML = srcLoc.innerHTML;
        dbLoc.value = op.location_id;
    }

    const srcAcc = document.getElementById('w_account_id');
    const dbAcc = document.getElementById('edit_w_account_id');
    if (srcAcc && dbAcc) {
        dbAcc.innerHTML = srcAcc.innerHTML;
        dbAcc.value = op.account_id;
    }

    const dateInput = document.getElementById('edit_w_date');
    if (dateInput) {
        const d = parseLocalDate(op.created_at);
        d.setMinutes(d.getMinutes() - d.getTimezoneOffset());
        const val = d.toISOString().slice(0, 16);
        dateInput.value = val;
        updateEditFakeDate(val);
    }

    openDrawer('edit_writeoff');
}

async function submitEditWriteoff() {
    const opID = document.getElementById('edit_op_id').value;
    const qty = document.getElementById('edit_w_qty').value;
    const locID = document.getElementById('edit_w_location').value;
    const accountID = document.getElementById('edit_w_account_id').value;
    const comment = document.getElementById('edit_w_comment').value || ""; 
    let opDate = document.getElementById('edit_w_date').value;

    if (!qty || parseFloat(qty) <= 0) return alert("Укажите корректное количество!");
    if (opDate.length === 10) opDate += "T12:00";

    const payload = {
        quantity: parseFloat(qty),
        location_id: parseInt(locID),
        account_id: accountID,
        comment: comment.trim(),
        date: opDate
    };

    try {
        const res = await fetch(API + '/operations/' + opID, {
            method: 'PUT',
            headers: getHeaders(),
            body: JSON.stringify(payload)
        });

        if (res.ok) {
            if (tg.HapticFeedback) tg.HapticFeedback.notificationOccurred('success');
            closeDrawer();
            loadHistory(); 
        } else {
            alert("Ошибка сохранения списания");
        }
    } catch (e) {
        alert("Сбой сети при сохранении");
    }
}

// ============================================================================
// 10. КОНТАКТЫ ПОСТАВЩИКОВ
// ============================================================================

async function openSupplierContacts() {
    openDrawer('supplier_contacts');
    const listEl = document.getElementById('supplierContactsList');
    const searchInput = document.getElementById('supplier_contact_search');
    if (searchInput) searchInput.value = ''; 
    listEl.innerHTML = "Загрузка...";

    try {
        const res = await fetch(API + '/suppliers', { headers: getHeaders() });
        if (res.ok) {
            supplierContactsCache = await res.json() || [];
            renderSupplierContactsList(supplierContactsCache);
        } else {
            listEl.innerHTML = "Ошибка загрузки";
        }
    } catch (e) {
        listEl.innerHTML = "Ошибка сети";
    }
}

function renderSupplierContactsList(list) {
    const listEl = document.getElementById('supplierContactsList');
    if (!listEl) return;
    if (list.length === 0) {
        listEl.innerHTML = '<div style="text-align:center; color:var(--text-muted); font-size:12px; padding: 20px 0;">Поставщики не найдены</div>';
        return;
    }

    listEl.innerHTML = list.map((s, idx) => `
        <div style="background:var(--surface); border:1px solid var(--border); border-radius:12px; padding:12px; margin-bottom:8px;">
            <div style="font-weight:700; font-size:13px; margin-bottom:8px; color:var(--primary);">${escapeHtml(s.name)}</div>
            <div style="display:flex; gap:10px; align-items:center;">
                <input type="text" id="sup_tg_${idx}" value="${s.tg_username || ''}" placeholder="username без @" style="margin-bottom:0; padding:8px 12px; font-size:13px; flex:2;">
                <button type="button" class="btn-main" onclick="saveSupplierContact('${s.uuid}', 'sup_tg_${idx}')" style="flex:1; padding:10px 15px; font-size:12px; background:#48BB78; margin:0;">Сохранить</button>
            </div>
        </div>
    `).join('');
}

function filterSupplierContactsList() {
    const q = document.getElementById('supplier_contact_search').value.toLowerCase();
    if (!q) return renderSupplierContactsList(supplierContactsCache);
    const searchWords = q.split(/\s+/).filter(w => w.length > 0);
    const filtered = supplierContactsCache.filter(s => searchWords.every(w => s.name.toLowerCase().includes(w)));
    renderSupplierContactsList(filtered);
}

async function saveSupplierContact(uuid, inputId) {
    const input = document.getElementById(inputId);
    if (!input) return;
    try {
        const res = await fetch(API + '/suppliers/contacts', {
            method: 'POST',
            headers: getHeaders(),
            body: JSON.stringify({ supplier_uuid: uuid, tg_username: input.value.trim() })
        });
        if (res.ok) {
            if (tg.HapticFeedback) tg.HapticFeedback.notificationOccurred('success');
            if (tg.showAlert) tg.showAlert("Контакт сохранен!"); else alert("Успешно!");
        } else {
            alert("Ошибка сохранения контакта");
        }
    } catch (e) {
        alert("Ошибка сети");
    }
}

// ============================================================================
// 11. СЧЕТА И СТАТЬИ СПИСАНИЙ
// ============================================================================

async function loadAccounts() {
    try {
        const res = await fetch(API + '/accounts', { headers: getHeaders() });
        const accs = await res.json() || [];
        const select = document.getElementById('w_account_id');
        if (select) {
            select.innerHTML = accs.map(a => `<option value="${a.external_id}">${escapeHtml(a.name)}</option>`).join('');
        }
    } catch(e) {}
}

async function openAccountsList() {
    openDrawer('accounts');
    const listEl = document.getElementById('accountsList');
    const select = document.getElementById('acc_iiko_select');
    
    try {
        const res = await fetch(API + '/accounts', { headers: getHeaders() });
        const accs = await res.json() || [];
        listEl.innerHTML = accs.map(a => `
            <div class="pulse-item">
                <div class="pulse-info"><div>${escapeHtml(a.name)}</div><span style="font-size:9px">${a.external_id}</span></div>
                <button type="button" class="btn-tiny" style="border-color:#F56565; color:#F56565;" onclick="deleteAccount(${a.id})">Удалить</button>
            </div>
        `).join('') || '<div style="font-size:12px; color:var(--text-muted);">Нет статей</div>';
    } catch(e) { listEl.innerHTML = "Ошибка загрузки"; }

    try {
        select.innerHTML = '<option value="">⏳ Загружаем из iiko...</option>';
        const resIiko = await fetch(API + '/iiko/accounts', { headers: getHeaders() });
        if (resIiko.ok) {
            const iikoAccs = await resIiko.json() || [];
            select.innerHTML = '<option value="">-- Выберите статью iiko --</option>' + 
                iikoAccs.map(a => `<option value="${a.external_id}">${escapeHtml(a.name)}</option>`).join('');
        } else {
            select.innerHTML = '<option value="">❌ Ошибка загрузки из iiko</option>';
        }
    } catch(e) {
        select.innerHTML = '<option value="">❌ Ошибка сети</option>';
    }
}

async function addAccountFromIiko() {
    const select = document.getElementById('acc_iiko_select');
    const uuid = select.value;
    if (!uuid) return alert("Выберите счет!");
    const name = select.options[select.selectedIndex].text.split(" (")[0];
    
    await fetch(API + '/accounts', { 
        method: 'POST', 
        headers: getHeaders(), 
        body: JSON.stringify({ name: name, externalID: uuid }) 
    });
    openAccountsList();
    loadAccounts();
}

async function deleteAccount(id) {
    if (!confirm("Удалить статью?")) return;
    await fetch(API + '/accounts/' + id, { method: 'DELETE', headers: getHeaders() });
    openAccountsList();
    loadAccounts();
}

// ============================================================================
// 12. ИНВЕНТАРИЗАЦИЯ ОСТАТКОВ
// ============================================================================

async function openInventoryList() {
    openDrawer('inventory_list');
    const listEl = document.getElementById('inventoryActList');
    listEl.innerHTML = "Загрузка...";
    try {
        const res = await fetch(API + '/inventories', { headers: getHeaders() });
        const acts = await res.json() || [];
        if (acts.length === 0) {
            listEl.innerHTML = '<div style="text-align:center; color:var(--text-muted); font-size:12px; padding: 20px;">Нет актов</div>';
            return;
        }
        listEl.innerHTML = acts.map(a => `
            <div class="pulse-item" style="cursor:pointer;" onclick="openInventoryAct(${a.id})">
                <div class="pulse-info">
                    <div>Акт #${a.id} <small>(${escapeHtml(a.location_name)})</small></div>
                    <span>${new Date(a.created_at).toLocaleDateString('ru-RU')} • ${escapeHtml(a.user_name)}</span>
                </div>
                <div class="pulse-val" style="color:${a.status === 'completed' ? '#48BB78' : '#E6983A'}">
                    ${a.status === 'completed' ? 'Завершен' : 'В работе'}
                </div>
            </div>
        `).join('');
    } catch(e) { listEl.innerHTML = "Ошибка загрузки"; }
}

async function startInventory() {
    const locId = document.getElementById('inv_new_location').value;
    if (!locId) return alert("Пожалуйста, выберите склад!");

    const btn = event.target;
    btn.innerText = "⏳ Синхронизация...";
    btn.disabled = true;

    try {
        const res = await fetch(API + '/inventories/start', {
            method: 'POST', headers: getHeaders(),
            body: JSON.stringify({ location_id: parseInt(locId) })
        });
        if (!res.ok) throw new Error(await res.text());
        const act = await res.json();
        openInventoryActUI(act);
    } catch(e) {
        alert("Ошибка: " + e.message);
    } finally {
        btn.innerText = "+ Весь склад";
        btn.disabled = false;
    }
}

async function openInventoryAct(id) {
    try {
        const res = await fetch(API + `/inventories/${id}`, { headers: getHeaders() });
        if (!res.ok) throw new Error("Акт не найден");
        const act = await res.json();
        openInventoryActUI(act);
    } catch(e) { alert(e.message); }
}

function openInventoryActUI(act) {
    currentInvAct = act;
    document.getElementById('inv_process_title').innerHTML = `Акт #${act.id}<br><small style="font-size:12px; color:var(--text-muted)">${escapeHtml(act.location_name)}</small>`;
    openDrawer('inventory_process');
    document.getElementById('inv_search').value = '';
    renderInventoryItems();
}

function renderInventoryItems(query = "") {
    const container = document.getElementById('inv_items_container');
    if (!currentInvAct || !currentInvAct.items) return;

    let filtered = currentInvAct.items;
    if (query) {
        const q = query.toLowerCase();
        const searchWords = q.split(/\s+/).filter(w => w.length > 0);
        filtered = filtered.filter(item => searchWords.every(w => item.position_name.toLowerCase().includes(w)));
    }

    const isCompleted = currentInvAct.status === 'completed';

    container.innerHTML = filtered.map(item => {
        const realIdx = currentInvAct.items.findIndex(x => x.position_name === item.position_name);
        const catItem = catalog.find(c => c.name === item.position_name);
        const unit = catItem ? catItem.unit : 'ед.';

        return `
            <div style="background:var(--surface); border:1px solid var(--border); border-radius:12px; padding:10px 15px; margin-bottom:8px; display:flex; align-items:center; justify-content:space-between; gap:15px;">
                <div style="font-size:13px; font-weight:600; flex:1; line-height:1.3;">
                    ${escapeHtml(item.position_name)}
                </div>
                <div style="display:flex; align-items:center; gap:8px;">
                    <input type="number" step="0.001" value="${item.actual_amount}" 
                        onchange="updateInvAmount(${realIdx}, this.value)"
                        ${isCompleted ? 'disabled style="background:var(--bg); border:none;"' : ''}
                        placeholder="0"
                        style="width: 80px; padding: 10px; margin:0; text-align:center; font-weight:bold;">
                    <div style="font-size:13px; font-weight:700; color:var(--text-muted); width: 35px; text-align:left;">
                        ${unit}
                    </div>
                </div>
            </div>
        `;
    }).join('') || '<div style="text-align:center; padding:20px; font-size:12px; color:var(--text-muted)">Пусто</div>';

    const actions = document.getElementById('inv_actions');
    if (actions) actions.style.display = isCompleted ? 'none' : 'flex';
}

function filterInventory() {
    renderInventoryItems(document.getElementById('inv_search').value);
}

function updateInvAmount(idx, val) {
    if (!currentInvAct) return;
    currentInvAct.items[idx].actual_amount = parseFloat(val) || 0;
}

async function saveInventoryProgress(finalize) {
    if (!currentInvAct) return;
    if (finalize && !confirm("Завершить инвентаризацию? Данные будут выгружены в iiko RMS.")) return;

    const btnDraft = document.getElementById('btn_inv_draft');
    const btnFinal = document.getElementById('btn_inv_final');
    if (btnDraft) btnDraft.disabled = true; 
    if (btnFinal) btnFinal.disabled = true;

    try {
        let res = await fetch(API + `/inventories/${currentInvAct.id}`, {
            method: 'PUT', headers: getHeaders(), body: JSON.stringify(currentInvAct.items)
        });
        if (!res.ok) throw new Error(await res.text());

        if (finalize) {
            res = await fetch(API + `/inventories/${currentInvAct.id}/finalize`, { method: 'POST', headers: getHeaders() });
            if (!res.ok) throw new Error(await res.text());
            if (tg.showAlert) tg.showAlert("Успешно выгружено в iiko!"); else alert("Успешно!");
            closeDrawer(); 
            openInventoryList();
        } else {
            if (tg.HapticFeedback) tg.HapticFeedback.notificationOccurred('success');
        }
    } catch(e) {
        alert("Ошибка: " + e.message);
    } finally {
        if (btnDraft) btnDraft.disabled = false; 
        if (btnFinal) btnFinal.disabled = false;
    }
}

async function deleteInventoryAct() {
    if (!currentInvAct) return;
    if (!confirm("Удалить этот черновик инвентаризации?")) return;

    try {
        const res = await fetch(API + `/inventories/${currentInvAct.id}`, { method: 'DELETE', headers: getHeaders() });
        if (res.ok) {
            closeDrawer();
            openInventoryList(); 
        } else {
            alert("Ошибка при удалении черновика");
        }
    } catch (e) {
        alert("Сбой сети");
    }
}

async function loadQA2ATemplates(locID) {
    const wrapper = document.getElementById('qa2a_templates_wrapper');
    const select = document.getElementById('qa2a_templates_select');
    const btnTemplate = document.getElementById('btn_start_from_template');
    
    if (!locID) {
        if (wrapper) wrapper.style.display = 'none';
        if (btnTemplate) btnTemplate.style.display = 'none';
        return;
    }

    try {
        const res = await fetch(API + `/inventories/templates?location_id=${locID}`, { headers: getHeaders() });
        if (res.ok) {
            const templates = await res.json() || [];
            if (templates.length === 0) {
                if (wrapper) wrapper.style.display = 'none';
                if (btnTemplate) btnTemplate.style.display = 'none';
            } else {
                if (select) {
                    select.innerHTML = '<option value="">-- Выберите шаблон --</option>' +
                        templates.map(t => `<option value="${t.id}">${escapeHtml(t.name)}</option>`).join('');
                }
                if (wrapper) wrapper.style.display = 'block';
                if (btnTemplate) btnTemplate.style.display = 'block';
            }
        }
    } catch(e) {}
}

async function startInventoryFromTemplate() {
    const locId = document.getElementById('inv_new_location').value;
    const select = document.getElementById('qa2a_templates_select');
    const templateId = select ? select.value : '';
    if (!locId || !templateId) return alert("Пожалуйста, выберите склад и бланк!");

    try {
        const res = await fetch(API + '/inventories/start-from-template', {
            method: 'POST',
            headers: getHeaders(),
            body: JSON.stringify({ location_id: parseInt(locId), template_id: parseInt(templateId) })
        });
        if (!res.ok) throw new Error(await res.text());
        const act = await res.json();
        openInventoryActUI(act);
    } catch(e) {
        alert("Ошибка: " + e.message);
    }
}

// ============================================================================
// 13. КОМАНДА И РОЛИ
// ============================================================================

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
        loadAdminData();
    } else {
        alert("Ошибка прав доступа при изменении роли");
    }
}

async function removeMember() {
    const userId = document.getElementById('target_user_id').value;
    if (!confirm("Удалить участника из заведения?")) return;
    await fetch(API + `/members/${userId}`, { method: 'DELETE', headers: getHeaders() });
    closeDrawer();
    loadAdminData();
}

// ============================================================================
// 14. НАСТРОЙКИ IIKO RMS
// ============================================================================

async function loadIikoSettings() {
    const myMembership = allMemberships.find(m => m.company_id == currentCompanyId);
    const btnIiko = document.getElementById('btn_iiko_settings_sub'); 
    if (!myMembership || myMembership.role !== 'owner') { 
        if (btnIiko) btnIiko.style.display = 'none'; 
        return; 
    }
    if (btnIiko) btnIiko.style.display = 'block';

    try {
        const res = await fetch(API + '/iiko/settings', { headers: getHeaders() });
        if (res.ok) {
            const data = await res.json();
            document.getElementById('iiko_host').value = data.iiko_host || '';
            document.getElementById('iiko_login').value = data.iiko_api_login || '';
            document.getElementById('iiko_pass').value = data.iiko_api_password ? '********' : '';
        }
    } catch(e) {}
}

async function saveAndSyncIiko() {
    const host = document.getElementById('iiko_host').value.trim();
    const login = document.getElementById('iiko_login').value.trim();
    const pass = document.getElementById('iiko_pass').value.trim();

    if (!host || !login) return alert("Заполните адрес Host и Login API!");

    const btn = document.getElementById('btn_save_sync');
    const oldText = btn.innerText;
    btn.innerText = "⏳ Сохранение...";
    btn.disabled = true;

    try {
        const payload = { host, login, password: pass === '********' ? '' : pass };
        const resSave = await fetch(API + '/iiko/settings', { method: 'POST', headers: getHeaders(), body: JSON.stringify(payload) });
        if (!resSave.ok) throw new Error("Ошибка сохранения параметров iiko");

        btn.innerText = "⏳ Синхронизация номенклатуры...";
        const resSync = await fetch(API + '/iiko/sync', { method: 'POST', headers: getHeaders() });
        if (!resSync.ok) throw new Error("Ошибка синхронизации: " + await resSync.text());

        if (tg.HapticFeedback) tg.HapticFeedback.notificationOccurred('success');
        if (tg.showAlert) tg.showAlert("✅ Синхронизировано!"); else alert("✅ Успешно!");
        await loadAllData(); 
        closeDrawer();       
    } catch(e) { 
        alert("❌ " + e.message); 
    } finally {
        btn.innerText = oldText;
        btn.disabled = false;
    }
}

async function forceExportDay() {
    if (!confirm("Выгрузить накопившиеся операции за сегодня в iiko прямо сейчас?")) return;
    try {
        const res = await fetch(API + '/iiko/force-export', { method: 'POST', headers: getHeaders() });
        if (res.ok) {
            if (tg.HapticFeedback) tg.HapticFeedback.notificationOccurred('success');
            if (tg.showAlert) tg.showAlert("✅ Проводки успешно переданы в iiko!"); else alert("Успешно!");
        } else {
            alert("❌ Ошибка выгрузки: " + await res.text());
        }
    } catch(e) { alert("Ошибка соединения"); }
}

// ============================================================================
// 15. МОДУЛЬ СОСТАВЛЕНИЯ ГРАФИКА РАБОТЫ
// ============================================================================

function openScheduleMenu() {
    openDrawer('schedule');
    switchSchedTab(1);
    loadStaffForSchedule();
}

function switchSchedTab(tabNum) {
    [1, 2, 3, 4].forEach(i => {
        const tabEl = document.getElementById('sched_tab_' + i);
        if (tabEl) tabEl.style.display = i === tabNum ? 'block' : 'none';

        const btn = document.getElementById('sched_tab_btn_' + i);
        if (btn) {
            btn.style.background = i === tabNum ? 'var(--surface)' : 'transparent';
            btn.style.color = i === tabNum ? 'var(--text)' : 'var(--text-muted)';
            btn.style.boxShadow = i === tabNum ? '0 2px 4px rgba(0,0,0,0.05)' : 'none';
        }
    });

    if (tabNum === 2) renderDemandGrid();
    if (tabNum === 3) populateUserSelectForSlots();
    if (tabNum === 4) generateSchedule();
}

async function loadStaffForSchedule() {
    const listEl = document.getElementById('sched_staff_list');
    listEl.innerHTML = "Загрузка сотрудников...";
    try {
        const res = await fetch(API + '/members', { headers: getHeaders() });
        if (!res.ok) throw new Error("Ошибка загрузки");
        const members = await res.json() || [];
        scheduleStaff = members;

        scheduleStaff.forEach(u => {
            if (!staffPreferences[u.user_id]) {
                staffPreferences[u.user_id] = { 0:'ANY', 1:'ANY', 2:'ANY', 3:'ANY', 4:'ANY', 5:'ANY', 6:'ANY' };
            }
        });

        listEl.innerHTML = members.map(m => `
            <div class="pulse-item" style="padding: 10px 14px; margin-bottom: 6px;">
                <div>
                    <b>${escapeHtml(m.user_name)}</b>
                    <small style="color:var(--text-muted); display:block;">${escapeHtml(m.custom_title || translateRole(m.role))}</small>
                </div>
                <input type="checkbox" id="sched_inc_${m.user_id}" checked style="width:20px; height:20px; margin:0; cursor:pointer;">
            </div>
        `).join('') || 'Нет сотрудников';
    } catch(e) {
        listEl.innerHTML = "Ошибка загрузки списка команды";
    }
}

function renderDemandGrid() {
    const grid = document.getElementById('sched_demand_grid');
    grid.innerHTML = dailyDemand.map((d, dayIdx) => `
        <div style="background:var(--surface); border:1px solid var(--border); border-radius:12px; padding:10px 14px; display:flex; justify-content:space-between; align-items:center;">
            <div style="font-size:13px; font-weight:700; width:110px;">${DAYS_NAMES[dayIdx]}</div>
            <div style="display:flex; gap:16px; align-items:center;">
                <div style="display:flex; align-items:center; gap:6px;">
                    <span style="font-size:12px; color:var(--text-muted);">1 смена:</span>
                    <input type="number" min="0" max="20" value="${d.shift1}" 
                        onchange="updateDemand(${dayIdx}, 'shift1', this.value)"
                        style="width:48px; padding:6px; margin:0; text-align:center; font-weight:bold;">
                </div>
                <div style="display:flex; align-items:center; gap:6px;">
                    <span style="font-size:12px; color:var(--text-muted);">2 смена:</span>
                    <input type="number" min="0" max="20" value="${d.shift2}" 
                        onchange="updateDemand(${dayIdx}, 'shift2', this.value)"
                        style="width:48px; padding:6px; margin:0; text-align:center; font-weight:bold;">
                </div>
            </div>
        </div>
    `).join('');
}

function updateDemand(dayIdx, shift, val) {
    dailyDemand[dayIdx][shift] = parseInt(val) || 0;
}

function populateUserSelectForSlots() {
    const select = document.getElementById('sched_user_select');
    const includedUsers = scheduleStaff.filter(m => {
        const cb = document.getElementById(`sched_inc_${m.user_id}`);
        return cb && cb.checked;
    });

    select.innerHTML = includedUsers.map(m => `<option value="${m.user_id}">${escapeHtml(m.user_name)}</option>`).join('');
    renderUserSlots();
}

function renderUserSlots() {
    const userId = document.getElementById('sched_user_select').value;
    const container = document.getElementById('sched_slots_grid');
    if (!userId) return;

    if (!staffPreferences[userId]) {
        staffPreferences[userId] = { 0:'ANY', 1:'ANY', 2:'ANY', 3:'ANY', 4:'ANY', 5:'ANY', 6:'ANY' };
    }

    const userPref = staffPreferences[userId];

    const states = {
        'ANY':    { label: 'Любая смена', badge: 'bg-any' },
        'SHIFT1': { label: 'Только 1 смена', badge: 'bg-morning' },
        'SHIFT2': { label: 'Только 2 смена', badge: 'bg-evening' },
        'OFF':    { label: 'Выходной', badge: 'bg-off' }
    };

    container.innerHTML = DAYS_NAMES.map((dayName, dayIdx) => {
        const curState = userPref[dayIdx] || 'ANY';
        return `
            <div style="background:var(--surface); border:1px solid var(--border); border-radius:12px; padding:10px 14px; display:flex; justify-content:space-between; align-items:center; cursor:pointer;" 
                 onclick="cycleSlotState(${userId}, ${dayIdx})">
                <div style="font-size:13px; font-weight:700;">${dayName}</div>
                <div class="slot-badge ${states[curState].badge}">
                    ${states[curState].label}
                </div>
            </div>
        `;
    }).join('');
}

function cycleSlotState(userId, dayIdx) {
    const sequence = ['ANY', 'SHIFT1', 'SHIFT2', 'OFF'];
    const cur = staffPreferences[userId][dayIdx] || 'ANY';
    const nextIdx = (sequence.indexOf(cur) + 1) % sequence.length;
    staffPreferences[userId][dayIdx] = sequence[nextIdx];
    renderUserSlots();
}

// Алгоритм распределения смен с балансировкой нагрузки
function generateSchedule() {
    const includedUsers = scheduleStaff.filter(m => {
        const cb = document.getElementById(`sched_inc_${m.user_id}`);
        return cb && cb.checked;
    });

    if (includedUsers.length === 0) {
        document.getElementById('sched_result_list').innerHTML = '<div style="text-align:center; color:#F56565; padding:20px;">Не выбраны сотрудники на шаге 1</div>';
        return;
    }

    const shiftStats = {};
    includedUsers.forEach(u => shiftStats[u.user_id] = 0);

    const schedule = [];

    for (let dayIdx = 0; dayIdx < 7; dayIdx++) {
        const req1 = dailyDemand[dayIdx].shift1;
        const req2 = dailyDemand[dayIdx].shift2;

        const assignedToday = new Set();
        const shift1Assigned = [];
        const shift2Assigned = [];

        // 1. Первая смена: приоритет тем, кто может ТОЛЬКО в 1 смену
        let shift1Candidates = includedUsers.filter(u => staffPreferences[u.user_id][dayIdx] === 'SHIFT1');
        shift1Candidates.sort((a, b) => shiftStats[a.user_id] - shiftStats[b.user_id]);

        shift1Candidates.forEach(u => {
            if (shift1Assigned.length < req1 && !assignedToday.has(u.user_id)) {
                shift1Assigned.push(u);
                assignedToday.add(u.user_id);
                shiftStats[u.user_id]++;
            }
        });

        // Добор на 1 смену из тех, кто может в любую
        if (shift1Assigned.length < req1) {
            let anyCandidates = includedUsers.filter(u => staffPreferences[u.user_id][dayIdx] === 'ANY' && !assignedToday.has(u.user_id));
            anyCandidates.sort((a, b) => shiftStats[a.user_id] - shiftStats[b.user_id]);

            anyCandidates.forEach(u => {
                if (shift1Assigned.length < req1) {
                    shift1Assigned.push(u);
                    assignedToday.add(u.user_id);
                    shiftStats[u.user_id]++;
                }
            });
        }

        // 2. Вторая смена: приоритет тем, кто может ТОЛЬКО во 2 смену
        let shift2Candidates = includedUsers.filter(u => staffPreferences[u.user_id][dayIdx] === 'SHIFT2' && !assignedToday.has(u.user_id));
        shift2Candidates.sort((a, b) => shiftStats[a.user_id] - shiftStats[b.user_id]);

        shift2Candidates.forEach(u => {
            if (shift2Assigned.length < req2) {
                shift2Assigned.push(u);
                assignedToday.add(u.user_id);
                shiftStats[u.user_id]++;
            }
        });

        // Добор на 2 смену из тех, кто может в любую
        if (shift2Assigned.length < req2) {
            let anyCandidates = includedUsers.filter(u => staffPreferences[u.user_id][dayIdx] === 'ANY' && !assignedToday.has(u.user_id));
            anyCandidates.sort((a, b) => shiftStats[a.user_id] - shiftStats[b.user_id]);

            anyCandidates.forEach(u => {
                if (shift2Assigned.length < req2) {
                    shift2Assigned.push(u);
                    assignedToday.add(u.user_id);
                    shiftStats[u.user_id]++;
                }
            });
        }

        schedule.push({
            dayName: DAYS_NAMES[dayIdx],
            shift1: shift1Assigned,
            shift2: shift2Assigned,
            shift1Deficit: req1 - shift1Assigned.length,
            shift2Deficit: req2 - shift2Assigned.length
        });
    }

    generatedScheduleResult = { schedule, shiftStats, includedUsers };
    renderScheduleResult();
}

function renderScheduleResult() {
    const container = document.getElementById('sched_result_list');
    if (!generatedScheduleResult) return;

    const { schedule, shiftStats, includedUsers } = generatedScheduleResult;

    let html = '';

    // Сводка смен
    html += `
        <div style="background:var(--surface); border:1px solid var(--border); border-radius:14px; padding:12px; margin-bottom:10px;">
            <div style="font-size:11px; font-weight:800; text-transform:uppercase; color:var(--text-muted); margin-bottom:8px;">Смен за неделю:</div>
            <div style="display:flex; flex-wrap:wrap; gap:6px;">
                ${includedUsers.map(u => `
                    <span style="font-size:12px; background:var(--bg); border:1px solid var(--border); padding:4px 8px; border-radius:8px;">
                        ${escapeHtml(u.user_name)}: <b>${shiftStats[u.user_id]}</b>
                    </span>
                `).join('')}
            </div>
        </div>
    `;

    // Расписание по дням недели
    html += schedule.map(day => {
        const s1Names = day.shift1.map(u => `<b>${escapeHtml(u.user_name)}</b>`).join(', ');
        const s2Names = day.shift2.map(u => `<b>${escapeHtml(u.user_name)}</b>`).join(', ');

        const s1Warn = day.shift1Deficit > 0 ? `<span style="color:#F56565; font-size:11px; font-weight:bold;"> (Нехватка: ${day.shift1Deficit})</span>` : '';
        const s2Warn = day.shift2Deficit > 0 ? `<span style="color:#F56565; font-size:11px; font-weight:bold;"> (Нехватка: ${day.shift2Deficit})</span>` : '';

        return `
            <div class="pulse-item" style="display:block; padding:12px; margin-bottom:8px;">
                <div style="font-size:13px; font-weight:800; margin-bottom:8px; border-bottom:1px solid var(--border); padding-bottom:4px;">
                    ${day.dayName}
                </div>
                
                <div style="font-size:12px; margin-bottom:6px;">
                    <span style="color:var(--text-muted);">1 смена:</span> 
                    ${s1Names || '<span style="color:var(--text-muted)">Нет назначений</span>'}${s1Warn}
                </div>

                <div style="font-size:12px;">
                    <span style="color:var(--text-muted);">2 смена:</span> 
                    ${s2Names || '<span style="color:var(--text-muted)">Нет назначений</span>'}${s2Warn}
                </div>
            </div>
        `;
    }).join('');

    container.innerHTML = html;
}

function copyScheduleText() {
    if (!generatedScheduleResult) return;
    const { schedule } = generatedScheduleResult;
    const orgName = document.getElementById('orgName').textContent;

    let text = `График смен — ${orgName}\n\n`;

    schedule.forEach(d => {
        const s1 = d.shift1.map(u => u.user_name).join(', ') || '—';
        const s2 = d.shift2.map(u => u.user_name).join(', ') || '—';
        
        text += `${d.dayName}:\n`;
        text += `  1 смена: ${s1}\n`;
        text += `  2 смена: ${s2}\n\n`;
    });

    navigator.clipboard.writeText(text);
    if (tg.HapticFeedback) tg.HapticFeedback.notificationOccurred('success');
    if (tg.showAlert) tg.showAlert("График скопирован!"); else alert("Скопировано!");
}

// ============================================================================
// 16. УТИЛИТЫ И УПРАВЛЕНИЕ ШТОРКАМИ
// ============================================================================

function openDrawer(id) {
    hideNavBar();
    document.getElementById('overlay').classList.add('visible');
    document.querySelectorAll('.drawer').forEach(d => d.classList.remove('open'));
    const target = document.getElementById('drawer_' + id);
    if (target) target.classList.add('open');
}

function closeDrawer() {
    document.getElementById('overlay').classList.remove('visible');
    document.querySelectorAll('.drawer').forEach(d => d.classList.remove('open'));
    showNavBar();
}

function renderCompanyList() {
    document.getElementById('companyList').innerHTML = allMemberships.map(m => `
        <div class="pulse-item" onclick="selectCompany(${m.company_id})" style="border:${m.company_id == currentCompanyId ? '1px solid var(--accent)' : '1px solid var(--border)'}; cursor:pointer;">
            <div>${escapeHtml(m.company_name)}</div>
        </div>
    `).join('');
}

function selectCompany(id) { 
    localStorage.setItem('selected_company_id', id); 
    location.reload(); 
}

async function createNewBusiness() {
    const nameInput = document.getElementById('new_biz_name');
    const name = nameInput ? nameInput.value.trim() : prompt("Название заведения:");
    if (!name) return;
    const res = await fetch(API + '/companies', { 
        method: 'POST', 
        headers: getHeaders(), // Теперь getHeaders использует Signed Token
        body: JSON.stringify({ name }) 
    });
    if (res.ok) location.reload(); else alert("Ошибка создания заведения");
}

async function createNewBusinessFromOnboarding() {
    const nameInput = document.getElementById('onboarding_biz_name');
    const name = nameInput ? nameInput.value.trim() : '';
    if (!name) return alert("Введите название заведения!");
    const res = await fetch(API + '/companies', { 
        method: 'POST', 
        headers: { 'Content-Type': 'application/json', 'X-Telegram-ID': String(userToken) }, 
        body: JSON.stringify({ name }) 
    });
    if (res.ok) location.reload(); else alert("Ошибка создания заведения");
}

async function joinByCode() {
    const code = prompt("Введите инвайт-код:");
    if (!code) return;
    const res = await fetch(API + '/join', { method: 'POST', headers: getHeaders(), body: JSON.stringify({ code }) });
    if (res.ok) location.reload(); else alert("Ошибка вступления");
}

async function joinByCodeFromOnboarding() {
    const code = document.getElementById('onboarding_invite_code').value.trim();
    if (!code) return alert("Введите код доступа!");
    const res = await fetch(API + '/join', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Telegram-ID': String(userToken) }, body: JSON.stringify({ code }) });
    if (res.ok) location.reload(); else alert("Ошибка вступления");
}

window.copyInviteCode = () => {
    const code = document.getElementById('displayInviteCode').textContent;
    if (code === "---" || code.includes("Ошибка")) return;
    navigator.clipboard.writeText(code);
    if (tg.showAlert) tg.showAlert("Код скопирован"); else alert("Код скопирован");
};

function updateFakeDate(val) {
    const fake = document.getElementById('w_date_fake');
    if (fake && val) {
        const d = new Date(val);
        fake.textContent = d.toLocaleDateString('ru-RU') + ', ' + d.toLocaleTimeString('ru-RU', {hour: '2-digit', minute: '2-digit'});
        fake.style.color = "var(--text)";
    }
}

function updateTransferFakeDate(val) {
    const fake = document.getElementById('tr_date_fake');
    if (fake && val) {
        const d = new Date(val);
        fake.textContent = d.toLocaleDateString('ru-RU') + ', ' + d.toLocaleTimeString('ru-RU', {hour: '2-digit', minute: '2-digit'});
        fake.style.color = "var(--text)";
    }
}

function updateEditFakeDate(val) {
    const fake = document.getElementById('edit_w_date_fake');
    if (fake && val) {
        const d = new Date(val);
        fake.textContent = d.toLocaleDateString('ru-RU') + ', ' + d.toLocaleTimeString('ru-RU', {hour: '2-digit', minute: '2-digit'});
    }
}

function translateRole(role) {
    const roles = { 'owner': 'Владелец', 'admin': 'Админ', 'manager': 'Менеджер', 'user': 'Сотрудник' };
    return roles[role] || role;
}

function parseLocalDate(dateStr) {
    if (!dateStr) return new Date();
    const isoStr = dateStr.replace(' ', 'T').replace(/Z|[-+]\d{2}:\d{2}$/i, '');
    return new Date(isoStr);
}

function escapeHtml(unsafe) {
    if (!unsafe) return "";
    return String(unsafe)
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;")
        .replace(/'/g, "&#039;");
}

// ============================================================================
// ЭКСПОРТ В WINDOW ДЛЯ INLINE-ОБРАБОТЧИКОВ HTML
// ============================================================================

window.toggleSubMenu = toggleSubMenu;
window.openScheduleMenu = openScheduleMenu;
window.switchSchedTab = switchSchedTab;
window.updateDemand = updateDemand;
window.renderUserSlots = renderUserSlots;
window.cycleSlotState = cycleSlotState;
window.generateSchedule = generateSchedule;
window.copyScheduleText = copyScheduleText;
window.switchTab = switchTab;
window.openDrawer = openDrawer;
window.closeDrawer = closeDrawer;
window.openActiveRequests = openActiveRequests;
window.openInventoryList = openInventoryList;
window.openAccountsList = openAccountsList;
window.openSupplierContacts = openSupplierContacts;
window.filterSupplierContactsList = filterSupplierContactsList;
window.saveSupplierContact = saveSupplierContact;
window.addAccountFromIiko = addAccountFromIiko;
window.deleteAccount = deleteAccount;
window.saveAndSyncIiko = saveAndSyncIiko;
window.forceExportDay = forceExportDay;
window.submitOperation = submitOperation;
window.filterSearch = filterSearch;
window.selectPos = selectPos;
window.selectGhostPos = selectGhostPos;
window.filterSupplierSearch = filterSupplierSearch;
window.selectSupplier = selectSupplier;
window.addReqItemToCart = addReqItemToCart;
window.removeReqItem = removeReqItem;
window.submitProcurementRequest = submitProcurementRequest;
window.updateReqStatus = updateReqStatus;
window.orderApprovedRequest = orderApprovedRequest;
window.copyOrderText = copyOrderText;
window.sendOrderToTg = sendOrderToTg;
window.downloadPDF = downloadPDF;
window.startInventory = startInventory;
window.openInventoryAct = openInventoryAct;
window.filterInventory = filterInventory;
window.updateInvAmount = updateInvAmount;
window.saveInventoryProgress = saveInventoryProgress;
window.deleteInventoryAct = deleteInventoryAct;
window.loadQA2ATemplates = loadQA2ATemplates;
window.startInventoryFromTemplate = startInventoryFromTemplate;
window.openOperationDetails = openOperationDetails;
window.openEditWriteoffForm = openEditWriteoffForm;
window.submitEditWriteoff = submitEditWriteoff;
window.submitRoleChange = submitRoleChange;
window.removeMember = removeMember;
window.selectCompany = selectCompany;
window.createNewBusiness = createNewBusiness;
window.createNewBusinessFromOnboarding = createNewBusinessFromOnboarding;
window.joinByCode = joinByCode;
window.joinByCodeFromOnboarding = joinByCodeFromOnboarding;
window.updateFakeDate = updateFakeDate;
window.updateTransferFakeDate = updateTransferFakeDate;
window.updateEditFakeDate = updateEditFakeDate;
window.escapeHtml = escapeHtml;

// ============================================================================
// SUPPLIER PORTAL


let currentEditingOfferId = 0;
window.supplierOffersCache = [];

function openSupplierOfferModal(offerId = 0) {
    currentEditingOfferId = offerId;
    
    if (offerId > 0) {
        const offer = window.supplierOffersCache.find(o => o.id === offerId);
        if (offer) {
            document.getElementById("so_title").value = offer.title;
            document.getElementById("so_desc").value = offer.description;
            document.getElementById("so_price_type").value = offer.price_type;
            document.getElementById("so_price").value = offer.price_value;
            let kwds = [];
            try { kwds = JSON.parse(offer.keywords || "[]"); } catch(e){}
            document.getElementById("so_keywords").value = kwds.join(", ");
        }
    } else {
        document.getElementById("so_title").value = "";
        document.getElementById("so_desc").value = "";
        document.getElementById("so_price").value = "";
        document.getElementById("so_keywords").value = "";
    }
    
    openDrawer("supplier_offer");
}
// & MARKETPLACE STUBS
// ============================================================================



function formatPrice(type, val) {
    if (type === "exact") return val.toFixed(2) + " ₽";
    if (type === "from") return "от " + val.toFixed(2) + " ₽";
    return "По запросу";
}

function renderSupplierOffers(offers) {
    window.supplierOffersCache = offers;
    const list = document.getElementById("supplier_offers_list");
    list.innerHTML = "";
    if (offers.length === 0) {
        list.innerHTML = "<div class=\"empty-state\">У вас пока нет активных предложений.</div>";
    } else {
        offers.forEach(o => {
            list.innerHTML += `<div class="card" style="margin-bottom:10px;">
                <div style="display:flex; justify-content:space-between;">
                    <div>
                        <h4 style="margin:0;">${o.title}</h4>
                        <div style="font-size:14px; margin:5px 0; color:var(--text-muted);">${o.description}</div>
                        <div style="font-weight:600; color:var(--primary);">${formatPrice(o.price_type, o.price_value)}</div>
                    </div>
                    <div style="display:flex; flex-direction:column; align-items:flex-end; gap:8px;">
                        <div style="background:var(--bg); padding:4px 8px; border-radius:8px; font-size:12px;"><span title="Просмотры">👁️ ${o.views_count}</span></div>
                        <div style="background:var(--bg); padding:4px 8px; border-radius:8px; font-size:12px;"><span title="Клики">🖱️ ${o.clicks_count}</span></div>
                    </div>
                </div>
                <div style="display:flex; gap: 10px; margin-top: 10px; border-top: 1px solid var(--border); padding-top: 10px;">
                    <button class="btn-main" style="flex:1; background:var(--bg); color:var(--text); border:1px solid var(--border);" onclick="openSupplierOfferModal(${o.id})">Редактировать</button>
                    <button class="btn-main" style="flex:1; background:#ffebee; color:#d32f2f; border:none;" onclick="deleteSupplierOffer(${o.id})">Удалить</button>
                </div>
            </div>`;
        });
    }

    let totalViews = offers.reduce((v, o) => v + o.views_count, 0);
    let totalClicks = offers.reduce((c, o) => c + o.clicks_count, 0);
    let ctr = totalViews > 0 ? ((totalClicks / totalViews) * 100).toFixed(1) : 0;

    const analyticsContainer = document.getElementById("supplier-tab-settings-inner");
    if (analyticsContainer) {
        analyticsContainer.innerHTML = `
            
            <div style="display:grid; grid-template-columns:1fr 1fr 1fr; gap:10px; margin-bottom:20px;">
                <div class="card" style="text-align:center; padding:15px 10px;">
                    <div style="font-size:32px; font-weight:800; color:var(--primary); margin-bottom:5px;">${totalViews}</div>
                    <div style="font-size:12px; color:var(--text-muted);">Просмотров</div>
                </div>
                <div class="card" style="text-align:center; padding:15px 10px;">
                    <div style="font-size:32px; font-weight:800; color:var(--accent); margin-bottom:5px;">${totalClicks}</div>
                    <div style="font-size:12px; color:var(--text-muted);">Кликов</div>
                </div>
                <div class="card" style="text-align:center; padding:15px 10px;">
                    <div style="font-size:32px; font-weight:800; color:#EA18EE; margin-bottom:5px;">${ctr}%</div>
                    <div style="font-size:12px; color:var(--text-muted);">Conversion</div>
                </div>
            </div>
            <div class="card">
                <h3 style="margin:0 0 10px 0; font-size:14px;">Воронка продаж</h3>
                <div style="font-size:13px; color:var(--text); line-height:1.5;">
                    Всего просмотров: <b>${totalViews}</b><br>Заказов: <b>${totalClicks}</b>
                </div>
            </div>

            <div class="card" style="margin-top:20px; text-align:center;">
                <h3 style="margin:0 0 10px 0; font-size:14px;">Код для приглашения сотрудников:</h3>
                <div style="font-size:20px; font-weight:800; letter-spacing:2px; color:var(--primary); background:var(--bg); padding:10px; border-radius:8px;">
                    ${window.supplierInviteCode || "НЕТ КОДА"}
                </div>
            </div>
        `;
    }
}


async function saveSupplierOffer() {
    const token = userToken;
    let kwStr = document.getElementById("so_keywords").value.trim();
    let keywords = "[]";
    if (kwStr) {
        const parts = kwStr.split(",").map(s => s.trim()).filter(s => s);
        keywords = JSON.stringify(parts);
    }
    const payload = {
        id: currentEditingOfferId,
        title: document.getElementById("so_title").value,
        desc: document.getElementById("so_desc").value,
        price_type: document.getElementById("so_price_type").value,
        price_value: parseFloat(document.getElementById("so_price_val") ? document.getElementById("so_price_val").value : document.getElementById("so_price").value || "0"),
        keywords: keywords
    };
    await fetch("/api/supplier/offers", {
        method: "POST",
        headers: { "Content-Type": "application/json", "X-Telegram-ID": token },
        body: JSON.stringify(payload)
    });
    closeDrawer();
    openSupplierPortal();
}

async function loadSpecialOffers() {
    try {
        const token = userToken;
        if (!token) return;
        
        let cId = currentCompanyId || localStorage.getItem("selected_company_id");
        
        const res = await fetch("/api/marketplace/offers", {
            headers: { "Authorization": "Bearer " + token, "X-Telegram-ID": token, "X-Company-ID": String(cId || "") }
        });
        if (!res.ok) {
            console.error("Failed to load offers", res.status);
            return;
        }
        const offers = await res.json();
        if (typeof renderSpecialOffers === "function") {
            renderSpecialOffers(offers || []);
        }
    } catch(e) {
        console.error("Error loadSpecialOffers:", e);
    }
}


function switchSupplierTab(id) {
    document.querySelectorAll(".supplier-tab-content").forEach(t => t.style.display = "none");
    const target = document.getElementById("supplier-tab-" + id);
    if (target) target.style.display = "block";
    
    document.querySelectorAll(".supplier-nav-item").forEach(i => i.classList.remove("active"));
    const activeNav = document.getElementById("sup-nav-" + id);
    if (activeNav) activeNav.classList.add("active");
}

async function deleteSupplierOffer(id) {
    Telegram.WebApp.showConfirm("Вы уверены, что хотите удалить эту карточку товара?", async function(ok) {
        if (!ok) return;
        const token = userToken;
    let res = await fetch("/api/supplier/offers?id=" + id, {
        method: "DELETE",
        headers: { "X-Telegram-ID": token }
    });
    if (res.ok) {
        Telegram.WebApp.showAlert("Товар удален");
        document.getElementById("supplier_offers_list").innerHTML = "<div class=\"empty-state\">Загрузка...</div>";
        let reloadRes = await fetch("/api/supplier/offers", { headers: { "X-Telegram-ID": token } });
        if (reloadRes.ok) renderSupplierOffers(await reloadRes.json());
    } else {
        Telegram.WebApp.showAlert("Ошибка при удалении");
    }
    });
}
