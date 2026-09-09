// ============================================================================
// ГЛОБАЛЬНЫЕ СЕЛЕКТОРЫ И СОСТОЯНИЕ ПРИЛОЖЕНИЯ
// ============================================================================

const els = {
    loginModal: document.getElementById('login-modal'),
    dashboardContent: document.getElementById('dashboard-content'),
    loginUser: document.getElementById('login-user'),
    loginPass: document.getElementById('login-pass'),
    btnSubmitLogin: document.getElementById('btn-submit-login'),
    btnLogout: document.getElementById('btn-logout'),

    company: document.getElementById('set-company'),
    store: document.getElementById('set-store'),
    supplierSearch: document.getElementById('set-supplier-search'),
    supplierDatalist: document.getElementById('iiko-suppliers-list'),
    btnSave: document.getElementById('btn-save-settings'),
    btnCatalog: document.getElementById('btn-load-catalog'),
    badge: document.getElementById('status-badge'),
    datalist: document.getElementById('iiko-catalog-list'),

    file: document.getElementById('pdf-file'),
    btnParse: document.getElementById('btn-parse'),
    loaderParse: document.getElementById('parse-loader'),
    dropZone: document.getElementById('drop-zone'),
    addFile: document.getElementById('pdf-add-file'),
    btnAddPage: document.getElementById('btn-add-page'),
    loaderAddPage: document.getElementById('add-page-loader'),

    resSection: document.getElementById('results-section'),
    resVendor: document.getElementById('res-vendor'),
    resDocnum: document.getElementById('res-docnum'),
    resDocdate: document.getElementById('res-docdate'),
    resConsignee: document.getElementById('res-consignee'),
    resShipper: document.getElementById('res-shipper'),
    tbody: document.getElementById('items-tbody'),
    btnImport: document.getElementById('btn-import'),
    loaderImport: document.getElementById('import-loader'),

    tbodyUnlisted: document.getElementById('unlisted-tbody'),
    btnRefreshUnlisted: document.getElementById('btn-refresh-unlisted'),

    tbodyAnalytics: document.getElementById('analytics-tbody'),
    analyticsSearch: document.getElementById('analytics-search'),
    analyticsDays: document.getElementById('analytics-days'),
};

let iikoCatalog = [];
let iikoSuppliers = [];
let currentToken = "";
let currentDocData = null;
let templateItems = [];
let analyticsCache = []; // Кэш для аналитики цен

// ============================================================================
// 1. АВТОРИЗАЦИЯ И УПРАВЛЕНИЕ СЕССИЕЙ БУХГАЛТЕРА
// ============================================================================

function getAuthToken() {
    return localStorage.getItem('bugh_token') || "";
}

function checkAuthentication() {
    const token = getAuthToken();
    if (token) {
        if (els.loginModal) els.loginModal.classList.add('hidden');
        if (els.dashboardContent) els.dashboardContent.classList.remove('blur-sm', 'pointer-events-none');
        initDashboard();
    } else {
        if (els.loginModal) els.loginModal.classList.remove('hidden');
        if (els.dashboardContent) els.dashboardContent.classList.add('blur-sm', 'pointer-events-none');
    }
}

if (els.btnSubmitLogin) {
    els.btnSubmitLogin.addEventListener('click', async () => {
        const login = els.loginUser.value.trim();
        const password = els.loginPass.value.trim();

        if (!login || !password) {
            alert("Пожалуйста, заполните логин и пароль!");
            return;
        }

        try {
            const res = await fetch('api/login', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ login, password })
            });

            if (!res.ok) {
                const errText = await res.text();
                throw new Error(errText || "Неверный логин или пароль (HTTP " + res.status + ")");
            }

            const data = await res.json();
            localStorage.setItem('bugh_token', data.token);
            
            els.loginUser.value = "";
            els.loginPass.value = "";

            checkAuthentication();
        } catch (err) {
            alert("❌ Ошибка входа: " + err.message);
        }
    });
}

if (els.btnLogout) {
    els.btnLogout.addEventListener('click', () => {
        if (confirm("Вы уверены, что хотите выйти из кабинета бухгалтера?")) {
            localStorage.removeItem('bugh_token');
            currentToken = "";
            currentDocData = null;
            iikoCatalog = [];
            iikoSuppliers = [];
            analyticsCache = [];
            location.reload();
        }
    });
}

document.addEventListener('DOMContentLoaded', checkAuthentication);

// ============================================================================
// 2. ИНИЦИАЛИЗАЦИЯ И ВЫБОР ЗАВЕДЕНИЯ
// ============================================================================

async function initDashboard() {
    try {
        const res = await fetch('api/companies', {
            headers: { 'Authorization': getAuthToken() }
        });

        if (res.status === 401) {
            localStorage.removeItem('bugh_token');
            checkAuthentication();
            return;
        }

        const companies = await res.json() || [];
        loadPromptPresets();
        els.company.innerHTML = '<option value="">-- Выберите заведение --</option>' +
            companies.map(c => `<option value="${c.id}">${escapeHtml(c.name)}</option>`).join('');

        const savedCompany = localStorage.getItem('active_company_id');
        if (savedCompany) {
            els.company.value = savedCompany;
            handleCompanyChange();
        }
    } catch (err) {
        console.error("Ошибка загрузки заведений:", err);
    }
}

if (els.company) {
    els.company.addEventListener('change', handleCompanyChange);
}

function handleCompanyChange() {
    const companyId = els.company.value;
    if (!companyId) {
        resetDashboardState();
        return;
    }

    localStorage.setItem('active_company_id', companyId);
    loadPromptPresets(companyId);

    els.store.innerHTML = '<option value="">-- Сначала обновите справочник --</option>';
    els.store.disabled = true;
    els.supplierSearch.value = "";
    els.supplierSearch.disabled = true;
    els.btnParse.disabled = true;
    els.btnParse.classList.add('opacity-50', 'cursor-not-allowed');

    try {
        const cachedCatalog = localStorage.getItem(`cached_catalog_${companyId}`);
        const cachedSuppliers = localStorage.getItem(`cached_suppliers_${companyId}`);
        const savedStore = localStorage.getItem(`saved_store_${companyId}`);

        if (cachedCatalog && cachedSuppliers) {
            iikoCatalog = JSON.parse(cachedCatalog) || [];
            iikoSuppliers = JSON.parse(cachedSuppliers) || [];

            populateDatalists();

            if (savedStore) {
                els.store.innerHTML = `<option value="${savedStore}">[Сохраненный склад из кэша]</option>`;
                els.store.dataset.saved = savedStore;
            }

            els.store.disabled = false;
            els.supplierSearch.disabled = false;
            els.btnParse.disabled = false;
            els.btnParse.classList.remove('opacity-50', 'cursor-not-allowed');

            els.badge.innerText = `✅ Справочник: ${iikoCatalog.length} товаров (Кэш)`;
            els.badge.className = "px-3 py-1 rounded-full text-[10px] font-semibold bg-emerald-500/10 text-emerald-400 border border-emerald-500/20";

            // При выборе компании обновляем данные на активных вкладках
            loadUnlistedOperations();
            if (document.getElementById('bugh-section-analytics').classList.contains('hidden') === false) {
                loadAnalytics();
            }
        } else {
            els.badge.innerText = "⏳ Требуется обновление справочника";
            els.badge.className = "px-3 py-1 rounded-full text-[10px] font-semibold bg-amber-500/10 text-amber-400 border border-amber-500/20";
        }
    } catch (e) {
        console.error("Ошибка чтения кэша заведения:", e);
    }
}

function resetDashboardState() {
    localStorage.removeItem('active_company_id');
    els.store.innerHTML = '<option value="">-- Сначала выберите заведение --</option>';
    els.store.disabled = true;
    els.supplierSearch.value = "";
    els.supplierSearch.disabled = true;
    els.badge.innerText = "Заведение не выбрано";
    els.badge.className = "px-3 py-1 rounded-full text-[10px] font-semibold bg-rose-500/10 text-rose-400 border border-rose-500/20";
    els.btnParse.disabled = true;
    els.btnParse.classList.add('opacity-50', 'cursor-not-allowed');
    els.tbodyUnlisted.innerHTML = '<tr><td colspan="7" class="p-6 text-center text-slate-500 italic">Заведение не выбрано</td></tr>';
    els.tbodyAnalytics.innerHTML = '<tr><td colspan="4" class="p-6 text-center text-slate-500 italic">Заведение не выбрано</td></tr>';
}

function populateDatalists() {
    if (els.datalist) {
        els.datalist.innerHTML = '';
        iikoCatalog.forEach(item => {
            const opt = document.createElement('option');
            opt.value = item.name;
            els.datalist.appendChild(opt);
        });
    }

    if (els.supplierDatalist) {
        els.supplierDatalist.innerHTML = '';
        iikoSuppliers.forEach(sup => {
            const opt = document.createElement('option');
            opt.value = sup.name;
            els.supplierDatalist.appendChild(opt);
        });
    }
}

if (els.btnSave) {
    els.btnSave.addEventListener('click', () => {
        const companyId = els.company.value;
        const storeUuid = els.store.value;
        const supplierName = els.supplierSearch.value.trim();

        if (!companyId) return alert("Пожалуйста, выберите заведение!");

        localStorage.setItem(`saved_store_${companyId}`, storeUuid);
        localStorage.setItem(`saved_supplier_name_${companyId}`, supplierName);
        
        const foundSupplier = iikoSuppliers.find(s => s.name === supplierName);
        if (foundSupplier) {
            localStorage.setItem(`saved_supplier_uuid_${companyId}`, foundSupplier.uuid);
        }

        alert('💾 Настройки сопоставления сохранены локально!');
    });
}

// ============================================================================
// 3. ОБНОВЛЕНИЕ СПРАВОЧНИКОВ IIKO RMS
// ============================================================================

if (els.btnCatalog) {
    els.btnCatalog.addEventListener('click', async () => {
        const companyId = els.company.value;
        if (!companyId) {
            alert("Пожалуйста, сначала выберите активное заведение!");
            return;
        }

        els.btnCatalog.disabled = true;
        els.btnCatalog.innerText = "⏳ Синхронизация...";

        try {
            const res = await fetch('api/catalog', {
                method: 'POST',
                headers: { 
                    'Content-Type': 'application/json',
                    'Authorization': getAuthToken()
                },
                body: JSON.stringify({ company_id: parseInt(companyId) })
            });

            if (!res.ok) throw new Error(await res.text());

            const data = await res.json();
            currentToken = data.token;
            iikoCatalog = data.catalog || [];
            iikoSuppliers = data.suppliers || [];

            localStorage.setItem(`cached_catalog_${companyId}`, JSON.stringify(iikoCatalog));
            localStorage.setItem(`cached_suppliers_${companyId}`, JSON.stringify(iikoSuppliers));

            populateDatalists();

            if (data.stores && data.stores.length > 0) {
                els.store.innerHTML = '';
                data.stores.forEach(s => {
                    const opt = document.createElement('option');
                    opt.value = s.uuid;
                    opt.textContent = s.name;
                    els.store.appendChild(opt);
                });
                els.store.disabled = false;

                const savedStore = localStorage.getItem(`saved_store_${companyId}`);
                if (savedStore) els.store.value = savedStore;
            }

            els.supplierSearch.disabled = false;
            const savedSupplierName = localStorage.getItem(`saved_supplier_name_${companyId}`);
            if (savedSupplierName) els.supplierSearch.value = savedSupplierName;

            els.badge.innerText = `✅ Справочник: ${iikoCatalog.length} товаров`;
            els.badge.className = "px-3 py-1 rounded-full text-[10px] font-bold bg-emerald-500/10 text-emerald-400 border border-emerald-500/20";
            
            els.btnParse.disabled = false;
            els.btnParse.classList.remove('opacity-50', 'cursor-not-allowed');

            loadUnlistedOperations();

        } catch (err) {
            alert("❌ Ошибка соединения: " + err.message);
        } finally {
            els.btnCatalog.disabled = false;
            els.btnCatalog.innerHTML = "🔄 Обновить справочник iiko";
        }
    });
}

// ============================================================================
// 4. НЕЙРОСЕТЕВОЙ ПАРСИНГ УПД (PDF)
// ============================================================================

if (els.btnParse) {
    els.btnParse.addEventListener('click', (e) => {
        e.preventDefault();
        executeParseWithFiles(els.file.files);
    });
}

async function executeParseWithFiles(fileObjs) {
    if (!fileObjs || fileObjs.length === 0) {
        alert("����������, �������� ���� ��������� (PDF ��� ����)");
        return;
    }

    const companyId = els.company.value;
    if (!companyId) {
        alert("Пожалуйста, сначала выберите активное заведение!");
        return;
    }

    const formData = new FormData();
    for(let i=0; i<fileObjs.length; i++) { formData.append('pdf', fileObjs[i], fileObjs[i].name); }
    formData.append('company_id', companyId);

    els.btnParse.disabled = true;
    els.loaderParse.classList.remove('hidden');
    els.resSection.classList.add('hidden');

    try {
        const res = await fetch('api/parse?token=' + getAuthToken(), {
            method: 'POST',
            body: formData
        });

        if (!res.ok) throw new Error(await res.text());

        currentDocData = await res.json();
        renderTable(currentDocData);
        els.resSection.classList.remove('hidden');
    } catch (err) {
        alert("❌ Ошибка парсинга AI: " + err.message);
    } finally {
        els.btnParse.disabled = false;
        els.loaderParse.classList.add('hidden');
    }
}

// ============================================================================
// DRAG AND DROP ОБРАБОТКА
// ============================================================================

['dragenter', 'dragover', 'dragleave', 'drop'].forEach(eventName => {
    window.addEventListener(eventName, (e) => {
        e.preventDefault();
        e.stopPropagation();
    }, false);
});

if (els.dropZone) {
    ['dragenter', 'dragover'].forEach(eventName => {
        els.dropZone.addEventListener(eventName, () => {
            els.dropZone.classList.add('border-brand-500', 'bg-brand-500/5');
        }, false);
    });

    ['dragleave', 'drop'].forEach(eventName => {
        els.dropZone.addEventListener(eventName, () => {
            els.dropZone.classList.remove('border-brand-500', 'bg-brand-500/5');
        }, false);
    });

    els.dropZone.addEventListener('drop', (e) => {
        const dt = e.dataTransfer;
        const files = dt.files;

        if (files.length > 0) {
            const file = files[0];
            const validExts = [".pdf", ".png", ".jpg", ".jpeg", ".webp"];
            const isValid = validExts.some(ext => file.name.toLowerCase().endsWith(ext)) || file.type.startsWith("image/") || file.type === "application/pdf";
            
            if (!isValid) {
                alert("����������, �������� ���� ��������� (PDF ��� ����)");
                return;
            }

            els.file.files = files;
            executeParseWithFiles(files);
        }
    }, false);

    els.dropZone.addEventListener('click', (e) => {
        if (e.target.closest('button, select, input, a, label, #prompt-preset-select, #btn-open-prompt-modal')) {
            return;
        }
        els.file.click();
    });
}

// ============================================================================
// 5. ОТРИСОВКА СПЕЦИФИКАЦИИ И РАСЧЕТОВ НАКЛАДНОЙ
// ============================================================================

async function executeAppendParseWithFiles(fileObjs) {
    if (!fileObjs || fileObjs.length === 0) return;
    if (!currentDocData) {
        alert("Сначала загрузите основную накладную!");
        return;
    }

    const companyId = els.company.value;
    const formData = new FormData();
    for (let i = 0; i < fileObjs.length; i++) {
        formData.append('pdf', fileObjs[i], fileObjs[i].name);
    }
    formData.append('company_id', companyId);
    if (currentCustomPrompt) {
        formData.append('prompt', currentCustomPrompt);
    }

    els.btnAddPage.disabled = true;
    els.loaderAddPage.classList.remove('hidden');

    try {
        const res = await fetch('api/parse?token=' + getAuthToken(), {
            method: 'POST',
            body: formData
        });

        if (!res.ok) throw new Error(await res.text());

        const appendData = await res.json();
        if (appendData && appendData.items && appendData.items.length > 0) {
            currentDocData.items = currentDocData.items.concat(appendData.items);
            renderTable(currentDocData);
        } else {
            alert("На добавленной странице товаров не найдено.");
        }
    } catch (err) {
        alert("Ошибка распознавания новой страницы: " + err.message);
    } finally {
        els.btnAddPage.disabled = false;
        els.loaderAddPage.classList.add('hidden');
        els.addFile.value = "";
    }
}

if (els.btnAddPage) {
    els.btnAddPage.addEventListener('click', (e) => {
        e.preventDefault();
        els.addFile.click();
    });
}
if (els.addFile) {
    els.addFile.addEventListener('change', (e) => {
        if (e.target.files.length > 0) {
            executeAppendParseWithFiles(e.target.files);
        }
    });
}


function updateFooterTotals() {
    if (!currentDocData || !currentDocData.items) return;
    let totalWithNds = 0;
    let totalWithoutNds = 0;
    currentDocData.items.forEach(item => {
        let q = parseFloat(item.quantity) || 0;
        let p = parseFloat(item.price) || 0;
        let nds = parseFloat(item.nds_percent) || 0;
        let sWithNds = q * p;
        let sumWithoutNds = sWithNds / (1 + nds/100);
        totalWithNds += sWithNds;
        totalWithoutNds += sumWithoutNds;
    });
    const elWithNds = document.getElementById('footer-total-with-nds');
    const elWithoutNds = document.getElementById('footer-total-without-nds');
    if (elWithNds) elWithNds.innerText = totalWithNds.toFixed(2) + " ₽";
    if (elWithoutNds) elWithoutNds.innerText = totalWithoutNds.toFixed(2) + " ₽";
}
window.updateFooterTotals = updateFooterTotals;

function executeAddEmptyRow() {
    if (!currentDocData) {
        currentDocData = {
            vendor_name: "Ручной ввод",
            doc_number: "Б/Н",
            doc_date: new Date().toISOString().split('T')[0],
            consignee: "",
            shipper: "",
            items: []
        };
        document.getElementById('results-section').classList.remove('hidden');
    }
    
    currentDocData.items.push({
        name: "Новый товар",
        clean_category: "Без категории",
        brand: "",
        quantity: 1.0,
        price: 0.0,
        sum: 0.0,
        sum_without_nds: 0.0,
        nds_percent: 0.0,
        mapped_uuid: "",
        mapped_name: "",
        multiplier: 1.0,
        is_ai_guessed: false,
        is_weight_changed: false,
        ai_multiplier: 1.0,
        ai_tip: "Ручной ввод"
    });
    renderTable(currentDocData);
}

document.addEventListener('DOMContentLoaded', () => {
    const btnEmpty = document.getElementById('btn-create-empty');
    if (btnEmpty) btnEmpty.addEventListener('click', () => {
        executeAddEmptyRow();
        document.getElementById('results-section').classList.remove('hidden');
    });
    
    const btnAddRow = document.getElementById('btn-add-row');
    if (btnAddRow) btnAddRow.addEventListener('click', executeAddEmptyRow);
});


function updateFooterTotals() {
    if (!currentDocData || !currentDocData.items) return;
    let totalWithNds = 0;
    let totalWithoutNds = 0;
    currentDocData.items.forEach(item => {
        let q = parseFloat(item.quantity) || 0;
        let p = parseFloat(item.price) || 0;
        let nds = parseFloat(item.nds_percent) || 0;
        let sWithNds = q * p;
        let sumWithoutNds = sWithNds / (1 + nds/100);
        totalWithNds += sWithNds;
        totalWithoutNds += sumWithoutNds;
    });
    const elWithNds = document.getElementById('footer-total-with-nds');
    const elWithoutNds = document.getElementById('footer-total-without-nds');
    if (elWithNds) elWithNds.innerText = totalWithNds.toFixed(2) + " ₽";
    if (elWithoutNds) elWithoutNds.innerText = totalWithoutNds.toFixed(2) + " ₽";
}
window.updateFooterTotals = updateFooterTotals;

function executeAddEmptyRow() {
    if (!currentDocData) {
        currentDocData = {
            vendor_name: "Ручной ввод",
            doc_number: "Б/Н",
            doc_date: new Date().toISOString().split('T')[0],
            consignee: "",
            shipper: "",
            items: []
        };
        document.getElementById('results-section').classList.remove('hidden');
    }
    
    currentDocData.items.push({
        name: "Новый товар",
        clean_category: "Без категории",
        brand: "",
        quantity: 1.0,
        price: 0.0,
        sum: 0.0,
        sum_without_nds: 0.0,
        nds_percent: 0.0,
        mapped_uuid: "",
        mapped_name: "",
        multiplier: 1.0,
        is_ai_guessed: false,
        is_weight_changed: false,
        ai_multiplier: 1.0,
        ai_tip: "Ручной ввод"
    });
    renderTable(currentDocData);
}

document.addEventListener('DOMContentLoaded', () => {
    const btnEmpty = document.getElementById('btn-create-empty');
    if (btnEmpty) btnEmpty.addEventListener('click', () => {
        executeAddEmptyRow();
        document.getElementById('results-section').classList.remove('hidden');
    });
    
    const btnAddRow = document.getElementById('btn-add-row');
    if (btnAddRow) btnAddRow.addEventListener('click', executeAddEmptyRow);
});

function renderTable(data) {
    els.resVendor.innerText = data.vendor_name || "Не определен";
    els.resDocnum.innerText = data.doc_number || "Б/Н";
    
    if (els.resDocdate) {
        els.resDocdate.value = data.doc_date || "";
    }

    if (els.resConsignee) {
        els.resConsignee.innerText = data.consignee || "Грузополучатель не распознан";
        els.resConsignee.title = data.consignee || "";
    }
    if (els.resShipper) {
        els.resShipper.innerText = data.shipper || "Грузоотправитель не распознан";
        els.resShipper.title = data.shipper || "";
    }

    const companyId = els.company.value;

    if (data.mapped_store_uuid && els.store) {
        els.store.value = data.mapped_store_uuid;
    } else {
        const savedStore = localStorage.getItem(`saved_store_${companyId}`);
        if (savedStore) els.store.value = savedStore;
    }

    if (data.mapped_supplier_uuid) {
        const cleanSupUuid = (data.mapped_supplier_uuid || '').toLowerCase().trim();
        const foundSup = iikoSuppliers.find(s => (s.uuid || '').toLowerCase().trim() === cleanSupUuid);
        if (foundSup) {
            els.supplierSearch.value = foundSup.name;
        }
    } else {
        const savedSupplierName = localStorage.getItem(`saved_supplier_name_${companyId}`);
        if (savedSupplierName) els.supplierSearch.value = savedSupplierName;
    }

    els.tbody.innerHTML = '';

    let totalWithNds = 0;
    let totalWithoutNds = 0;

    data.items.forEach((item, idx) => {
        let prefillName = '';
        if (item.mapped_uuid) {
            const cleanUuid = (item.mapped_uuid || '').toLowerCase().trim();
            const found = iikoCatalog.find(c => (c.uuid || '').toLowerCase().trim() === cleanUuid);
            if (found) {
                prefillName = found.name;
            } else if (item.mapped_name) {
                prefillName = item.mapped_name;
            } else {
                prefillName = item.mapped_uuid;
            }
        } else if (item.mapped_name) {
            prefillName = item.mapped_name;
        }

        const tr = document.createElement('tr');
        tr.className = "hover:bg-[#111827]/40 transition-colors border-b border-slate-800/40 align-middle";

        const aiQty = parseFloat(item.quantity) || 0;
        const initMult = parseFloat(item.multiplier) || 1.0;
        const initFinalQty = aiQty * initMult;

        const isAi = item.is_ai_guessed;
        const isWarning = item.is_weight_changed;

        let multClass = "border-slate-800 bg-[#090d16] text-white focus:border-brand-500 focus:bg-[#111827]";
        let multBadge = "";

        if (isWarning) {
            multClass = "border-amber-500/80 bg-amber-500/10 text-amber-300 font-semibold focus:border-amber-500 focus:ring-2 focus:ring-amber-500/20";
            multBadge = `
                <div class="text-[9px] text-amber-300 font-semibold mt-1.5 text-center bg-amber-500/10 border border-amber-500/20 rounded px-1.5 py-0.5 inline-block">
                    ⚠️ Вес изменился! (ИИ: ${item.ai_multiplier} | Было: ${item.history_multiplier})
                </div>`;
        } else if (isAi) {
            multClass = "border-teal-500/80 bg-teal-500/10 text-teal-300";
            multBadge = `<div class="text-[9px] text-teal-400 font-semibold mt-1 text-center">✨ ИИ веса</div>`;
        }

        const aiTipHtml = (item.ai_tip && item.ai_tip !== "Обычный товар")
            ? `<div class="text-[10px] text-brand-400 font-medium mt-1 bg-brand-500/5 border border-brand-500/10 rounded px-2 py-0.5 inline-block">💡 ${escapeHtml(item.ai_tip)}</div>`
            : "";

        const finalSumWithNds = parseFloat(item.sum) || 0;
        const ndsPercent = parseFloat(item.nds_percent) || 0;

        let sumWithoutNds = parseFloat(item.sum_without_nds);
        if (isNaN(sumWithoutNds) || sumWithoutNds <= 0) {
            sumWithoutNds = finalSumWithNds;
            if (ndsPercent > 0) {
                sumWithoutNds = finalSumWithNds / (1.0 + (ndsPercent / 100.0));
            }
        }

        totalWithNds += finalSumWithNds;
        totalWithoutNds += sumWithoutNds;

        const aiCat = item.clean_category || "";
        const catOptions = ["Мясо и птица", "Рыба и морепродукты", "Овощи и фрукты", "Молочные продукты", "Бакалея", "Консервы", "Напитки", "Хозяйственные товары", "Прочее", "Без категории"];
        if (aiCat && !catOptions.includes(aiCat)) catOptions.push(aiCat);
        
        let catOptionsHtml = catOptions.map(c => `<option value="${escapeHtml(c)}" ${c === aiCat ? 'selected' : ''}>${escapeHtml(c)}</option>`).join('');

        tr.innerHTML = `
            <td class="px-2 py-3 text-center text-slate-500 font-semibold border-r border-slate-800/40 align-middle">${idx + 1}</td>
            <td class="px-2 py-3 align-middle">
                <input type="text" class="w-full bg-transparent border-b border-slate-700/50 outline-none focus:border-brand-500 font-bold text-slate-100 tracking-tight text-xs pb-1 transition-all" value="${escapeHtml(item.name)}" oninput="currentDocData.items[${idx}].name = this.value;">
                ${aiTipHtml}
            </td>
            <td class="px-2 py-3 align-middle">
                <select class="clean-category-select w-full bg-[#090d16] border border-slate-800 text-white rounded-lg p-2 text-[10px] outline-none focus:bg-[#111827] focus:border-brand-500 transition-all-300">
                    <option value="">Выберите...</option>
                    ${catOptionsHtml}
                </select>
            </td>
            <td class="px-2 py-3 text-center align-middle">
                <input type="text" inputmode="decimal" class="w-12 bg-transparent border-b border-slate-700/50 outline-none focus:border-brand-500 text-center font-extrabold text-slate-300 text-xs pb-1 transition-all ai-qty-input" value="${aiQty}">
            </td>
            <td class="px-2 py-3 text-center border-r border-slate-800/40 align-middle">
                <div class="flex items-center justify-center gap-1"><input type="text" inputmode="decimal" class="w-14 bg-transparent border-b border-slate-700/50 outline-none focus:border-brand-500 text-center font-semibold text-white text-xs pb-1 transition-all ai-price-input" value="${item.price.toFixed(2)}"> <span class="text-[10px]">₽</span></div>
                <div class="text-[10px] text-slate-500 mt-1"><span class="ai-sum-display">${finalSumWithNds.toFixed(2)}</span> ₽ (всего)</div>
                <div class="text-[9px] text-brand-400 font-semibold mt-1 bg-brand-500/10 rounded px-1.5 py-0.5 inline-block">НДС ${item.nds_percent}%</div>
            </td>
            <td class="px-2 py-3 align-middle">
                <input type="text" list="iiko-catalog-list" class="iiko-search w-full bg-[#090d16] border border-slate-800 text-white placeholder-slate-500 rounded-lg p-2 text-xs outline-none focus:bg-[#111827] focus:border-brand-500 transition-all-300 font-normal" 
                    value="${escapeHtml(prefillName)}" placeholder="Начните вводить или вставьте UUID...">
            </td>
            <td class="px-2 py-3 text-center align-middle">
                <input type="text" inputmode="decimal" oninput="this.value = this.value.replace(/[^0-9.,]/g, '');" class="iiko-mult w-12 bg-[#090d16] border border-slate-800 rounded-lg p-1.5 text-xs outline-none focus:bg-[#111827] focus:border-brand-500 text-center ${multClass} font-normal" 
                    value="${initMult}">
                ${multBadge}
            </td>
            <td class="px-2 py-3 text-center font-semibold text-emerald-400 text-xs final-qty align-middle">
                ${initFinalQty.toFixed(3)}
            </td>
            <td class="px-2 py-3 text-center align-middle">
                <button class="bg-rose-500/10 text-rose-400 border border-rose-500/20 hover:bg-rose-500/20 transition-all-300 px-2.5 py-1.5 rounded-lg text-[10px] font-bold" onclick="deleteInvoiceItem(${idx})">
                    ✕
                </button>
            </td>
        `;

        const multInput = tr.querySelector('.iiko-mult');
        const finalCell = tr.querySelector('.final-qty');

        const aiQtyInput = tr.querySelector('.ai-qty-input');
        const aiPriceInput = tr.querySelector('.ai-price-input');
        const sumDisplay = tr.querySelector('.ai-sum-display');
        
        function updateRowMath() {
            let m = parseFloat(multInput.value.replace(',', '.')) || 0;
            let q = parseFloat(aiQtyInput.value.replace(',', '.')) || 0;
            let p = parseFloat(aiPriceInput.value.replace(',', '.')) || 0;
            
            currentDocData.items[idx].quantity = q;
            currentDocData.items[idx].price = p;
            currentDocData.items[idx].multiplier = m;
            
            let sWithNds = q * p;
            currentDocData.items[idx].sum = sWithNds;
            
            let nds = parseFloat(currentDocData.items[idx].nds_percent) || 0;
            currentDocData.items[idx].sum_without_nds = sWithNds / (1 + nds/100);
            
            finalCell.innerText = (q * m).toFixed(3);
            sumDisplay.innerText = sWithNds.toFixed(2);
            updateFooterTotals();
        }

        multInput.addEventListener('input', updateRowMath);
        if(aiQtyInput) aiQtyInput.addEventListener('input', updateRowMath);
        if(aiPriceInput) aiPriceInput.addEventListener('input', updateRowMath);

        els.tbody.appendChild(tr);
    });

    if (data.items.length > 0) {
        const totalTr = document.createElement('tr');
        totalTr.className = "bg-[#090d16] font-semibold border-t border-slate-800 text-slate-300 text-xs align-middle";
        totalTr.innerHTML = `
            <td class="p-4 text-center text-slate-500">∑</td>
            <td class="p-4 text-left uppercase text-[10px] font-semibold tracking-wider text-slate-500" colspan="2">Итого накладная:</td>
            <td class="p-4 text-left align-middle border-r border-slate-800/40" colspan="5">
                <span class="text-slate-500 font-normal">Без НДС:</span> 
                <span id="footer-total-without-nds" class="text-slate-200 font-semibold mr-6">${totalWithoutNds.toFixed(2)} ₽</span>
                <span class="text-slate-500 font-normal">С НДС:</span> 
                <span id="footer-total-with-nds" class="text-brand-400 font-semibold">${totalWithNds.toFixed(2)} ₽</span>
            </td>
        `;
        els.tbody.appendChild(totalTr);
    }
}

// ============================================================================
// 6. ИМПОРТ НАКЛАДНОЙ В IIKO RMS И ОБНОВЛЕНИЕ АНАЛИТИКИ
// ============================================================================

if (els.btnImport) {
    els.btnImport.addEventListener('click', async () => {
        if (!currentDocData) return;

        const companyId = els.company.value;
        const storeUuid = els.store.value;
        
        if (!companyId) return alert("Заведение не выбрано!");
        if (!storeUuid) return alert("⚠️ Сначала укажите Склад прихода в iiko!");

        const supplierName = els.supplierSearch.value.trim();
        const foundSupplier = iikoSuppliers.find(s => s.name === supplierName);
        const supplierUuid = foundSupplier ? foundSupplier.uuid : '';

        if (!supplierUuid) {
            alert("⚠️ Указанный Поставщик не найден! Пожалуйста, выберите корректного поставщика из выпадающего списка.");
            return;
        }

        const rows = els.tbody.querySelectorAll('tr');
        const itemsToImport = [];
        let hasErrors = false;

        rows.forEach((tr, idx) => {
            const searchInput = tr.querySelector('.iiko-search')?.value.trim();
            const rawMult = (tr.querySelector('.iiko-mult')?.value || "").replace(',', '.');
            const multInput = parseFloat(rawMult) || 1.0;
            const originalItem = currentDocData.items[idx];

            if (searchInput && searchInput !== "") {
                let mappedUuid = "";
                let mappedName = searchInput;

                const uuidRegex = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i;
                const match = searchInput.match(uuidRegex);

                if (match) {
                    mappedUuid = match[0];
                    mappedName = `[UUID] ${searchInput}`;
                } else {
                    const cleanSearch = searchInput.toLowerCase().trim();
                    const foundProduct = iikoCatalog.find(c =>
                        c.name.toLowerCase().trim() === cleanSearch ||
                        c.uuid.toLowerCase().trim() === cleanSearch
                    );

                    if (foundProduct) {
                        mappedUuid = foundProduct.uuid;
                        mappedName = foundProduct.name;
                    }
                }

                if (!mappedUuid) {
                    hasErrors = true;
                    tr.querySelector('.iiko-search').classList.add('border-red-500/80', 'bg-red-500/5');
                } else {
                    tr.querySelector('.iiko-search').classList.remove('border-red-500/80', 'bg-red-500/5');
                    
                    const userCategory = tr.querySelector('.clean-category-select')?.value || originalItem.clean_category || "";
                    itemsToImport.push({
                        name: originalItem.name,
                        clean_category: userCategory,
                        brand: originalItem.brand || "",
                        quantity: originalItem.quantity,
                        price: originalItem.price,
                        sum: originalItem.sum,
                        sum_without_nds: parseFloat(originalItem.sum_without_nds) || 0.0,
                        nds_percent: parseFloat(originalItem.nds_percent) || 0.0,
                        mapped_uuid: mappedUuid,
                        mapped_name: mappedName,
                        multiplier: multInput
                    });
                }
            }
        });

        if (hasErrors) {
            alert("⚠️ Некоторые товары не сопоставлены со справочником! Проверьте поля, подсвеченные красным.");
            return;
        }

        if (itemsToImport.length === 0) {
            alert("⚠️ Нет товаров для отправки!");
            return;
        }

        const payload = {
            company_id: parseInt(companyId),
            token: currentToken,
            store_uuid: storeUuid,
            supplier_uuid: supplierUuid,
            vendor_name: currentDocData.vendor_name,
            consignee: currentDocData.consignee,
            shipper: currentDocData.shipper,
            invoice_number: currentDocData.doc_number,
            invoice_date: els.resDocdate ? els.resDocdate.value.trim() : "",
            items: itemsToImport
        };

        els.btnImport.disabled = true;
        els.loaderImport.classList.remove('hidden');

        try {
            const res = await fetch('api/import?token=' + getAuthToken(), {
                method: 'POST',
                headers: { 
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify(payload)
            });

            const text = await res.text();
            if (!res.ok) throw new Error(text);

            alert("✅ Успешно! Накладная создана в iiko RMS и добавлена в историю аналитики.");
            els.resSection.classList.add('hidden');
            els.file.value = "";
            
            // Если вкладка аналитики загружалась, обновляем её данными новой накладной
            if (analyticsCache.length > 0) {
                loadAnalytics();
            }

        } catch (err) {
            alert("❌ Ошибка отправки:\n" + err.message);
        } finally {
            els.btnImport.disabled = false;
            els.loaderImport.classList.add('hidden');
        }
    });
}

function deleteInvoiceItem(idx) {
    if (!currentDocData || !currentDocData.items) return;

    const itemName = currentDocData.items[idx].name;
    if (confirm(`Вычеркнуть позицию из накладной?\n\n"${itemName}"`)) {
        currentDocData.items.splice(idx, 1);
        renderTable(currentDocData);
    }
}
window.deleteInvoiceItem = deleteInvoiceItem;

// ============================================================================
// 7. АНАЛИТИКА ЦЕН И ЗАКУПОК (DASHBOARD)
// ============================================================================

async function loadAnalytics() {
    const companyId = els.company.value;
    if (!companyId) return;

    const days = els.analyticsDays ? els.analyticsDays.value : 30;
    els.tbodyAnalytics.innerHTML = '<tr><td colspan="4" class="p-6 text-center text-slate-400 font-semibold animate-pulse">⏳ Вычисляем аналитику...</td></tr>';

    try {
        const res = await fetch(`api/analytics?company_id=${companyId}&days=${days}`, {
            headers: { 'Authorization': getAuthToken() }
        });

        if (!res.ok) throw new Error(await res.text());

        const records = await res.json() || [];

        if (records.length === 0) {
            els.tbodyAnalytics.innerHTML = `<tr><td colspan="4" class="p-6 text-center text-slate-500 italic">За последние ${days} дней накладные не проводились</td></tr>`;
            analyticsCache = [];
            return;
        }

        // Группировка и вычисления на лету (Client-side OLAP)
        const grouped = {};
        
        records.forEach(r => {
            const key = (r.iiko_product_uuid && r.iiko_product_uuid.trim() !== "") ? r.iiko_product_uuid : (r.iiko_product_name || r.product_name);
            const displayName = r.iiko_product_name || r.product_name;
            if (!grouped[key]) {
                grouped[key] = {
                    name: displayName,
                    invoiceName: r.product_name,
                    uuid: r.iiko_product_uuid || "",
                    prices: [],
                    latestDate: '1970-01-01',
                    latestPrice: 0,
                    latestSupplier: '',
                    minPrice: Infinity,
                    bestSupplier: ''
                };
            }

            const price = parseFloat(r.price_per_base_unit);
            grouped[key].prices.push(price);

            // Ищем последнюю цену по дате накладной
            if (r.invoice_date >= grouped[key].latestDate) {
                grouped[key].latestDate = r.invoice_date;
                grouped[key].latestPrice = price;
                grouped[key].latestSupplier = r.supplier_name;
            }

            // Ищем исторический минимум за период
            if (price < grouped[key].minPrice) {
                grouped[key].minPrice = price;
                grouped[key].bestSupplier = r.supplier_name;
            }
        });

        analyticsCache = Object.values(grouped).map(g => {
            // Расчет медианы (сортируем цены и берем среднюю точку)
            g.prices.sort((a, b) => a - b);
            const mid = Math.floor(g.prices.length / 2);
            if (g.prices.length % 2 !== 0) {
                g.medianPrice = g.prices[mid];
            } else {
                g.medianPrice = (g.prices[mid - 1] + g.prices[mid]) / 2.0;
            }
            return g;
        });

        // Сортировка по алфавиту для удобства
        analyticsCache.sort((a, b) => a.name.localeCompare(b.name));

        renderAnalytics(analyticsCache);

    } catch (err) {
        els.tbodyAnalytics.innerHTML = `<tr><td colspan="4" class="p-6 text-center text-rose-500 font-semibold">❌ Ошибка загрузки: ${err.message}</td></tr>`;
    }
}

function renderAnalytics(data) {
    if (data.length === 0) {
        els.tbodyAnalytics.innerHTML = '<tr><td colspan="4" class="p-6 text-center text-slate-500 italic">По вашему запросу ничего не найдено</td></tr>';
        return;
    }

    els.tbodyAnalytics.innerHTML = data.map(item => {
        let latestColorClass = "text-slate-300";
        let icon = "";
        let tooltip = "Цена в норме";

        if (item.latestPrice > item.medianPrice * 1.05) {
            latestColorClass = "text-rose-400";
            icon = "↑";
            tooltip = "Товар подорожал относительно медианы";
        } else if (item.latestPrice < item.medianPrice * 0.95) {
            latestColorClass = "text-emerald-400";
            icon = "↓";
            tooltip = "Товар подешевел относительно медианы";
        }

        return `
            <tr class="hover:bg-[#111827]/40 transition-colors border-b border-slate-800/40 align-middle">
                <td class="p-4 align-middle">
                    <div class="font-bold text-slate-200 text-xs">${escapeHtml(item.name)}</div>
                    ${item.invoiceName && item.invoiceName !== item.name ? `<div class="text-[10px] text-slate-400 mt-0.5 truncate max-w-xs" title="${escapeHtml(item.invoiceName)}">В накладной: ${escapeHtml(item.invoiceName)}</div>` : ''}
                    ${item.uuid ? `<div class="text-[9px] text-slate-500 mt-0.5 font-mono">${escapeHtml(item.uuid)}</div>` : ''}
                </td>
                <td class="p-4 text-center border-r border-slate-800/40 align-middle" title="${tooltip}">
                    <div class="font-bold ${latestColorClass} text-xs">${item.latestPrice.toFixed(2)} ₽ ${icon}</div>
                    <div class="text-[9px] text-slate-500 mt-1">${item.latestDate}</div>
                    ${item.latestSupplier ? `<div class="text-[9px] text-slate-400 mt-0.5">📦 ${escapeHtml(item.latestSupplier)}</div>` : ''}
                </td>
                <td class="p-4 text-center font-bold text-slate-300 text-xs border-r border-slate-800/40 align-middle">
                    ${item.medianPrice.toFixed(2)} ₽
                </td>
                <td class="p-4 align-middle">
                    <div class="font-bold text-emerald-400 text-xs">${item.minPrice.toFixed(2)} ₽</div>
                    <div class="text-[10px] text-slate-400 mt-1 whitespace-normal break-words">🏆 ${escapeHtml(item.bestSupplier)}</div>
                </td>
            </tr>
        `;
    }).join('');
}

function filterAnalytics() {
    const query = els.analyticsSearch ? els.analyticsSearch.value.toLowerCase().trim() : '';
    if (!query) {
        renderAnalytics(analyticsCache);
        return;
    }
    const searchWords = query.split(/\s+/);
    const filtered = analyticsCache.filter(item => {
        const nameLower = (item.name || '').toLowerCase();
        const invLower = (item.invoiceName || '').toLowerCase();
        const bestSup = (item.bestSupplier || '').toLowerCase();
        const latestSup = (item.latestSupplier || '').toLowerCase();
        return searchWords.every(word => 
            nameLower.includes(word) || invLower.includes(word) || bestSup.includes(word) || latestSup.includes(word)
        );
    });
    renderAnalytics(filtered);
}
window.loadAnalytics = loadAnalytics;
window.filterAnalytics = filterAnalytics;


// ============================================================================
// 8. ДИСПЕТЧЕР НЕУЧТЕННЫХ СПИСАНИЙ
// ============================================================================

async function loadUnlistedOperations() {
    const companyId = els.company.value;
    if (!companyId) return;

    els.tbodyUnlisted.innerHTML = '<tr><td colspan="7" class="p-6 text-center text-slate-400">⏳ Загрузка списка операций...</td></tr>';

    try {
        const res = await fetch(`api/unlisted-operations?company_id=${companyId}`, {
            headers: { 'Authorization': getAuthToken() }
        });

        if (!res.ok) throw new Error(await res.text());

        const ops = await res.json() || [];

        if (ops.length === 0) {
            els.tbodyUnlisted.innerHTML = '<tr><td colspan="7" class="p-6 text-center text-slate-500 italic">Нет необработанных неучтенных списаний</td></tr>';
            return;
        }

        els.tbodyUnlisted.innerHTML = ops.map((op, idx) => {
            const dateObj = new Date(op.created_at);
            const dateStr = dateObj.toLocaleDateString('ru-RU') + ' ' + dateObj.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
            const inputId = `unlisted_match_${op.id}`;

            return `
                <tr class="hover:bg-[#111827]/40 transition-colors border-b border-slate-800/40 align-middle">
                    <td class="p-4 text-center text-slate-500 font-semibold border-r border-slate-800/40 align-middle">${idx + 1}</td>
                    <td class="p-4 align-middle">
                        <div class="font-bold text-slate-200">${escapeHtml(op.user_name)}</div>
                        <div class="text-[10px] text-slate-500 mt-1">${dateStr}</div>
                    </td>
                    <td class="p-4 font-bold text-rose-400 align-middle text-xs">
                        ⚠️ ${escapeHtml(op.position_name)}
                    </td>
                    <td class="p-4 text-center font-bold text-slate-300 text-xs align-middle">
                        ${op.quantity} ${op.unit}
                    </td>
                    <td class="p-4 text-slate-400 italic align-middle text-xs truncate max-w-[200px]" title="${escapeHtml(op.comment)}">
                        ${escapeHtml(op.comment) || '<span class="text-slate-600">Нет причины</span>'}
                    </td>
                    <td class="p-4 align-middle">
                        <input type="text" id="${inputId}" list="iiko-catalog-list" class="w-full bg-[#090d16] border border-slate-800 text-white placeholder-slate-500 rounded-lg p-2 text-xs outline-none focus:bg-[#111827] focus:border-brand-500 transition-all-300 font-normal" placeholder="Введите имя товара в iiko...">
                    </td>
                    <td class="p-4 text-center align-middle">
                        <button class="bg-emerald-500/10 hover:bg-emerald-500/20 text-emerald-400 border border-emerald-500/20 px-4 py-2 rounded-lg text-[10px] font-bold transition-all-300 shadow-sm" onclick="resolveUnlistedOperation(${op.id}, '${inputId}')">
                            Учесть в iiko
                        </button>
                    </td>
                </tr>
            `;
        }).join('');

    } catch (err) {
        els.tbodyUnlisted.innerHTML = `<tr><td colspan="7" class="p-6 text-center text-rose-500 font-semibold">❌ Ошибка загрузки: ${err.message}</td></tr>`;
    }
}

if (els.btnRefreshUnlisted) {
    els.btnRefreshUnlisted.addEventListener('click', loadUnlistedOperations);
}

async function resolveUnlistedOperation(opID, inputId) {
    const companyId = els.company.value;
    const input = document.getElementById(inputId);
    if (!input) return;

    const matchedName = input.value.trim();
    if (!matchedName) {
        alert("⚠️ Укажите правильное имя товара из iiko RMS!");
        return;
    }

    const cleanSearch = matchedName.toLowerCase().trim();
    const foundProduct = iikoCatalog.find(c => c.name.toLowerCase().trim() === cleanSearch);

    if (!foundProduct) {
        alert("❌ Товар с таким именем не найден в вашем справочнике iiko RMS! Пожалуйста, выберите корректное значение из выпадающего списка.");
        return;
    }

    if (!confirm(`Привязать операцию к товару из iiko?\n\nБыло: "Неучтенный товар"\nСтанет: "${foundProduct.name}"`)) {
        return;
    }

    try {
        const res = await fetch('api/unlisted-operations/resolve?token=' + getAuthToken(), {
            method: 'POST',
            headers: { 
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({
                company_id: parseInt(companyId),
                operation_id: opID,
                iiko_product_name: foundProduct.name
            })
        });

        if (!res.ok) throw new Error(await res.text());

        alert("✅ Успешно сопоставлено! Списание приведено к номенклатуре и будет выгружено автоматически.");
        loadUnlistedOperations();
    } catch (err) {
        alert("❌ Ошибка сопоставления: " + err.message);
    }
}

window.resolveUnlistedOperation = resolveUnlistedOperation;
window.loadUnlistedOperations = loadUnlistedOperations;

// ============================================================================
// 9. ВКЛАДКИ (TABS)
// ============================================================================

function switchBughTab(tabId) {
    const tabs = ['parser', 'analytics', 'unlisted', 'template'];
    
    tabs.forEach(t => {
        const btn = document.getElementById(`tab-btn-${t}`);
        const sect = document.getElementById(`bugh-section-${t}`);
        
        if (btn) btn.className = "px-6 py-3 font-semibold text-slate-500 hover:text-slate-300 text-xs focus:outline-none transition-all-300 whitespace-nowrap";
        if (sect) sect.classList.add('hidden');
    });

    const activeBtn = document.getElementById(`tab-btn-${tabId}`);
    const activeSect = document.getElementById(`bugh-section-${tabId}`);

    if (activeBtn) activeBtn.className = "px-6 py-3 font-semibold border-b-2 border-brand-500 text-brand-400 text-xs focus:outline-none transition-all-300 whitespace-nowrap";
    if (activeSect) activeSect.classList.remove('hidden');

    if (tabId === 'unlisted') loadUnlistedOperations();
    if (tabId === 'analytics') loadAnalytics();
}

window.switchBughTab = switchBughTab;

// ============================================================================
// 10. КОНСТРУКТОР БЛАНКОВ ИНВЕНТАРИЗАЦИИ
// ============================================================================

const btnAddTplItem = document.getElementById('btn-add-tpl-item');
if (btnAddTplItem) {
    btnAddTplItem.addEventListener('click', () => {
        const input = document.getElementById('tpl-search');
        const val = input.value.trim();
        if (!val) return;

        const found = iikoCatalog.find(c => c.name === val);
        if (!found) {
            alert("⚠️ Товар не найден в справочнике! Пожалуйста, выберите товар из выпадающего списка.");
            return;
        }

        if (templateItems.includes(val)) {
            alert("Товар уже добавлен в бланк!");
            return;
        }

        templateItems.push(val);
        input.value = '';
        renderTemplateTable();
    });
}

function renderTemplateTable() {
    const tbody = document.getElementById('tpl-tbody');
    if (!tbody) return;
    tbody.innerHTML = '';

    if (templateItems.length === 0) {
        tbody.innerHTML = `<tr><td colspan="3" class="p-6 text-center text-slate-500 italic">Добавьте товары в список через строку поиска выше</td></tr>`;
        return;
    }

    templateItems.forEach((name, idx) => {
        const tr = document.createElement('tr');
        tr.className = "hover:bg-[#111827]/40 transition-colors border-b border-slate-800/40";
        tr.innerHTML = `
            <td class="p-4 text-center text-slate-500 font-bold border-r border-slate-800/40 align-middle">${idx + 1}</td>
            <td class="p-4 font-bold text-slate-100 text-xs align-middle">${escapeHtml(name)}</td>
            <td class="p-4 text-center align-middle">
                <button class="bg-rose-500/10 text-rose-400 border border-rose-500/20 px-3 py-1.5 rounded-lg hover:bg-rose-500/20 transition-all-300 text-xs font-semibold" onclick="removeTemplateItem(${idx})">
                    Удалить
                </button>
            </td>
        `;
        tbody.appendChild(tr);
    });
}

function removeTemplateItem(idx) {
    templateItems.splice(idx, 1);
    renderTemplateTable();
}

const btnSaveTemplate = document.getElementById('btn-save-template');
if (btnSaveTemplate) {
    btnSaveTemplate.addEventListener('click', async () => {
        const name = document.getElementById('tpl-name').value.trim();
        const companyId = els.company.value;
        const storeUuid = els.store.value;

        if (!companyId) return alert("⚠️ Выберите заведение!");
        if (!storeUuid) return alert("⚠️ Выберите склад в настройках выше!");
        if (!name) return alert("⚠️ Укажите название бланка (например: Пятница Бар Срез)!");
        if (templateItems.length === 0) return alert("⚠️ Добавьте хотя бы один товар в бланк!");

        const oldText = btnSaveTemplate.innerText;
        btnSaveTemplate.disabled = true;
        btnSaveTemplate.innerText = "⏳ Опубликование бланка...";

        const payload = {
            store_uuid: storeUuid,
            name: name,
            items: templateItems,
            company_id: parseInt(companyId)
        };

        try {
            const res = await fetch('api/templates/save?token=' + getAuthToken(), {
                method: 'POST',
                headers: { 
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify(payload)
            });

            if (!res.ok) throw new Error(await res.text());

            alert("✅ Успешно! Бланк сохранен и опубликован в Mini App для сотрудников.");

            document.getElementById('tpl-name').value = '';
            templateItems = [];
            renderTemplateTable();
        } catch (err) {
            alert("❌ Ошибка сохранения:\n" + err.message);
        } finally {
            btnSaveTemplate.disabled = false;
            btnSaveTemplate.innerText = oldText;
        }
    });
}

function escapeHtml(unsafe) {
    if (!unsafe) return "";
    return unsafe
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;")
        .replace(/'/g, "&#039;");
}

renderTemplateTable();
window.removeTemplateItem = removeTemplateItem;
// ============================================================================
// 10. Resizable Columns
// ============================================================================
function initResizableColumns(tableId) {
    const table = document.getElementById(tableId)?.closest('table');
    if (!table) return;

    const cols = table.querySelectorAll('thead th.relative');
    [].forEach.call(cols, function(col) {
        const resizer = document.createElement('div');
        resizer.style.width = '6px';
        resizer.style.height = '100%';
        resizer.style.position = 'absolute';
        resizer.style.right = '0';
        resizer.style.top = '0';
        resizer.style.cursor = 'col-resize';
        resizer.style.userSelect = 'none';
        resizer.style.zIndex = '10';
        resizer.style.transition = 'background-color 0.2s';
        
        resizer.addEventListener('mouseenter', () => resizer.style.backgroundColor = 'rgba(16, 185, 129, 0.4)');
        resizer.addEventListener('mouseleave', () => resizer.style.backgroundColor = 'transparent');
        
        col.appendChild(resizer);

        let x = 0;
        let w = 0;

        const mouseDownHandler = function(e) {
            x = e.clientX;
            const styles = window.getComputedStyle(col);
            w = parseInt(styles.width, 10);
            
            document.addEventListener('mousemove', mouseMoveHandler);
            document.addEventListener('mouseup', mouseUpHandler);
            resizer.style.backgroundColor = 'rgba(16, 185, 129, 0.8)';
        };

        const mouseMoveHandler = function(e) {
            const dx = e.clientX - x;
            col.style.width = (w + dx) + 'px';
            col.style.minWidth = (w + dx) + 'px';
        };

        const mouseUpHandler = function() {
            document.removeEventListener('mousemove', mouseMoveHandler);
            document.removeEventListener('mouseup', mouseUpHandler);
            resizer.style.backgroundColor = 'transparent';
        };

        resizer.addEventListener('mousedown', mouseDownHandler);
    });
}

document.addEventListener('DOMContentLoaded', () => {
    initResizableColumns('items-tbody');
});



// ============================================================================
// УПРАВЛЕНИЕ КАСТОМНЫМИ ПРОМПТАМИ И ПРЕСЕТАМИ AI
// ============================================================================

async function loadPromptPresets(companyId) {
    try {
        const cId = companyId || (els.company ? els.company.value : "") || "0";
        const res = await fetch('api/parser/presets?company_id=' + cId + '&token=' + getAuthToken());
        if (!res.ok) return;

        promptPresets = await res.json() || [];
        renderPresetDropdowns();

        const savedPresetId = localStorage.getItem('active_prompt_preset_id');
        let selectedPreset = promptPresets.find(p => String(p.id) === String(savedPresetId));
        if (!selectedPreset && promptPresets.length > 0) {
            selectedPreset = promptPresets[0];
        }

        if (selectedPreset) {
            selectPresetById(selectedPreset.id);
        }
    } catch (err) {
        console.error("Ошибка загрузки пресетов промптов:", err);
    }
}

function renderPresetDropdowns() {
    const pSel = document.getElementById('prompt-preset-select');
    const mSel = document.getElementById('modal-preset-select');

    const presetsToRender = (promptPresets && promptPresets.length > 0) ? promptPresets : [
        { id: 1, name: "Стандартный (УПД / ТОРГ-12)", is_default: true },
        { id: 2, name: "Многостраничная накладная (фото / сканы)", is_default: true },
        { id: 3, name: "Товарный чек / Простая квитанция", is_default: true }
    ];

    const optionsHtml = presetsToRender.map(p => {
        const prefix = p.is_default ? "⭐ " : "📁 ";
        return '<option value="' + p.id + '">' + prefix + escapeHtml(p.name) + '</option>';
    }).join('');

    if (pSel) pSel.innerHTML = optionsHtml;
    if (mSel) mSel.innerHTML = optionsHtml;
}

function selectPresetById(presetId) {
    const preset = (promptPresets || []).find(p => String(p.id) === String(presetId));
    if (!preset) return;

    activePresetId = preset.id;
    currentCustomPrompt = preset.prompt || "";
    localStorage.setItem('active_prompt_preset_id', preset.id);

    const pSel = document.getElementById('prompt-preset-select');
    const mSel = document.getElementById('modal-preset-select');
    const mName = document.getElementById('modal-preset-name');
    const mText = document.getElementById('modal-prompt-textarea');
    const badge = document.getElementById('preset-badge');
    const btnDel = document.getElementById('btn-delete-preset');

    if (pSel) pSel.value = preset.id;
    if (mSel) mSel.value = preset.id;
    if (mName) mName.value = preset.name;
    if (mText) {
        mText.value = preset.prompt || "";
        updatePromptCharCount();
    }

    if (badge) {
        if (preset.is_default) {
            badge.textContent = "Системный шаблон (защищен)";
            badge.className = "px-2.5 py-1 bg-brand-500/10 border border-brand-500/20 text-brand-400 text-[10px] font-semibold rounded-lg";
        } else {
            badge.textContent = "Пользовательский пресет";
            badge.className = "px-2.5 py-1 bg-amber-500/10 border border-amber-500/20 text-amber-400 text-[10px] font-semibold rounded-lg";
        }
    }

    if (btnDel) {
        if (preset.is_default) {
            btnDel.disabled = true;
            btnDel.classList.add('opacity-40', 'cursor-not-allowed');
            btnDel.title = "Системный шаблон нельзя удалить";
        } else {
            btnDel.disabled = false;
            btnDel.classList.remove('opacity-40', 'cursor-not-allowed');
            btnDel.title = "Удалить пользовательский пресет";
        }
    }
}

function updatePromptCharCount() {
    if (!els.promptCharCount || !els.modalPromptTextarea) return;
    const len = els.modalPromptTextarea.value.length;
    els.promptCharCount.textContent = len + ' симв.';
}

function openPromptModal() {
    if (!els.promptModal) return;
    if (activePresetId) {
        selectPresetById(activePresetId);
    }
    els.promptModal.classList.remove('hidden');
}

function closePromptModal() {
    if (!els.promptModal) return;
    els.promptModal.classList.add('hidden');
}

async function savePromptPreset() {
    const name = els.modalPresetName.value.trim();
    const promptText = els.modalPromptTextarea.value.trim();

    if (!name) {
        alert("Пожалуйста, введите название пресета!");
        els.modalPresetName.focus();
        return;
    }
    if (!promptText) {
        alert("Текст промпта не может быть пустым!");
        els.modalPromptTextarea.focus();
        return;
    }

    const companyId = parseInt(els.company ? els.company.value : "0") || 0;
    const payload = {
        id: activePresetId || 0,
        company_id: companyId,
        name: name,
        prompt: promptText
    };

    try {
        const res = await fetch('api/parser/presets?token=' + getAuthToken(), {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });

        if (!res.ok) throw new Error(await res.text());

        const result = await res.json();
        const targetId = result.id || activePresetId;

        await loadPromptPresets(companyId);
        selectPresetById(targetId);

        currentCustomPrompt = promptText;
        closePromptModal();

        if (result.is_copy) {
            alert('✅ Так как системный шаблон защищен, создана его пользовательская копия: "' + name + ' (Копия)" и выбрана в качестве активной.');
        } else {
            alert('✅ Пресет "' + name + '" успешно сохранен и применен!');
        }
    } catch (err) {
        alert("❌ Ошибка сохранения пресета: " + err.message);
    }
}

async function deletePromptPreset() {
    if (!activePresetId) return;
    const preset = promptPresets.find(p => p.id === activePresetId);
    if (!preset || preset.is_default) {
        alert("Нельзя удалить системный шаблон.");
        return;
    }

    if (!confirm('Вы действительно хотите удалить пресет "' + preset.name + '"?')) {
        return;
    }

    try {
        const res = await fetch('api/parser/presets?id=' + activePresetId + '&token=' + getAuthToken(), {
            method: 'DELETE'
        });

        if (!res.ok) throw new Error(await res.text());

        localStorage.removeItem('active_prompt_preset_id');
        const companyId = parseInt(els.company ? els.company.value : "0") || 0;
        await loadPromptPresets(companyId);
        alert("🗑️ Пресет успешно удален.");
    } catch (err) {
        alert("❌ Ошибка удаления пресета: " + err.message);
    }
}

async function resetToDefaultPrompt() {
    try {
        const res = await fetch('api/parser/default-prompt?token=' + getAuthToken());
        if (!res.ok) throw new Error(await res.text());
        const data = await res.json();
        if (data.prompt && els.modalPromptTextarea) {
            els.modalPromptTextarea.value = data.prompt;
            updatePromptCharCount();
        }
    } catch (err) {
        alert("Ошибка сброса промпта: " + err.message);
    }
}

function createNewPresetForm() {
    activePresetId = 0;
    if (els.modalPresetName) {
        els.modalPresetName.value = "Новый пресет";
        els.modalPresetName.focus();
        els.modalPresetName.select();
    }
    if (els.presetBadge) {
        els.presetBadge.textContent = "Новый пресет (не сохранен)";
        els.presetBadge.className = "px-2.5 py-1 bg-emerald-500/10 border border-emerald-500/20 text-emerald-400 text-[10px] font-semibold rounded-lg";
    }
    if (els.btnDeletePreset) {
        els.btnDeletePreset.disabled = true;
        els.btnDeletePreset.classList.add('opacity-40', 'cursor-not-allowed');
    }
}

// Привязка событий пресетов
if (els.promptPresetSelect) {
    els.promptPresetSelect.addEventListener('change', (e) => {
        selectPresetById(e.target.value);
    });
}
if (els.modalPresetSelect) {
    els.modalPresetSelect.addEventListener('change', (e) => {
        selectPresetById(e.target.value);
    });
}
if (els.btnOpenPromptModal) {
    els.btnOpenPromptModal.addEventListener('click', (e) => {
        e.preventDefault();
        openPromptModal();
    });
}
if (els.btnClosePromptModal) {
    els.btnClosePromptModal.addEventListener('click', closePromptModal);
}
if (els.btnCancelPromptModal) {
    els.btnCancelPromptModal.addEventListener('click', closePromptModal);
}
if (els.btnSavePreset) {
    els.btnSavePreset.addEventListener('click', savePromptPreset);
}
if (els.btnDeletePreset) {
    els.btnDeletePreset.addEventListener('click', deletePromptPreset);
}
if (els.btnNewPreset) {
    els.btnNewPreset.addEventListener('click', createNewPresetForm);
}
if (els.btnResetDefaultPrompt) {
    els.btnResetDefaultPrompt.addEventListener('click', resetToDefaultPrompt);
}
if (els.modalPromptTextarea) {
    els.modalPromptTextarea.addEventListener('input', updatePromptCharCount);
}
