async function handleCompanyChange() {
    const companyId = els.company.value;
    if (!companyId) {
        resetDashboardState();
        return;
    }

    localStorage.setItem('active_company_id', companyId);
    loadPromptPresets(companyId);

    // Immediately disable store and supplier-search and set their placeholders
    els.store.innerHTML = '<option value="">⏳ Синхронизация с iiko...</option>';
    els.store.disabled = true;
    els.supplierSearch.value = "";
    els.supplierSearch.disabled = true;
    els.supplierSearch.placeholder = "⏳ Синхронизация с iiko...";

    els.btnParse.disabled = true;
    els.btnParse.classList.add('opacity-50', 'cursor-not-allowed');

    els.badge.innerText = "⏳ Синхронизация с iiko...";
    els.badge.className = "px-3 py-1 rounded-full text-[10px] font-semibold bg-amber-500/10 text-amber-400 border border-amber-500/20";

    try {
        const res = await fetch('api/catalog', {
            method: 'POST',
            headers: { 
                'Content-Type': 'application/json',
                'Authorization': getAuthToken()
            },
            body: JSON.stringify({ company_id: parseInt(companyId) })
        });

        if (!res.ok) {
            let errMsg = "Ошибка загрузки справочника";
            try {
                const text = await res.text();
                if (text) errMsg = text;
            } catch (e) {}

            if (res.status === 400 || res.status === 401 || errMsg.toLowerCase().includes("iiko") || errMsg.toLowerCase().includes("ключ")) {
                els.badge.innerText = "⚠️ iiko не подключена";
                els.badge.className = "px-3 py-1 rounded-full text-[10px] font-semibold bg-amber-500/10 text-amber-400 border border-amber-500/20";
            } else {
                els.badge.innerText = `❌ ${errMsg}`;
                els.badge.className = "px-3 py-1 rounded-full text-[10px] font-semibold bg-rose-500/10 text-rose-400 border border-rose-500/20";
            }

            els.store.innerHTML = '<option value="">-- iiko не подключена --</option>';
            els.store.disabled = true;
            els.supplierSearch.value = "";
            els.supplierSearch.disabled = true;
            els.supplierSearch.placeholder = "iiko не подключена";

            loadUnlistedOperations();
            if (document.getElementById('bugh-section-analytics')?.classList.contains('hidden') === false) {
                loadAnalytics();
            }
            if (document.getElementById('bugh-section-tickets')?.classList.contains('hidden') === false) {
                loadAdminTickets();
            }
            return;
        }

        const data = await res.json();
        currentToken = data.token;
        iikoCatalog = data.catalog || [];
        iikoSuppliers = data.suppliers || [];

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
            if (savedStore && Array.from(els.store.options).some(o => o.value === savedStore)) {
                els.store.value = savedStore;
            }
            const inlineStore = document.getElementById('res-inline-store');
            if (inlineStore) {
                inlineStore.innerHTML = els.store.innerHTML;
                inlineStore.value = els.store.value;
                inlineStore.disabled = false;
            }
        } else {
            els.store.innerHTML = '<option value="">-- Нет доступных складов --</option>';
            els.store.disabled = true;
        }

        els.supplierSearch.disabled = false;
        els.supplierSearch.placeholder = "Введите имя поставщика...";
        const savedSupplierName = localStorage.getItem(`saved_supplier_name_${companyId}`);
        if (savedSupplierName) els.supplierSearch.value = savedSupplierName;

        const inlineSupplierName = document.getElementById('res-inline-supplier-name');
        if (inlineSupplierName) {
            inlineSupplierName.innerText = (els.supplierSearch && els.supplierSearch.value) ? els.supplierSearch.value : '—';
        }

        els.badge.innerText = "✅ Справочник загружен";
        els.badge.className = "px-3 py-1 rounded-full text-[10px] font-bold bg-emerald-500/10 text-emerald-400 border border-emerald-500/20";

        els.btnParse.disabled = false;
        els.btnParse.classList.remove('opacity-50', 'cursor-not-allowed');

        // Refresh data on active tabs
        loadUnlistedOperations();
        if (document.getElementById('bugh-section-analytics')?.classList.contains('hidden') === false) {
            loadAnalytics();
        }
        if (document.getElementById('bugh-section-tickets')?.classList.contains('hidden') === false) {
            loadAdminTickets();
        }

    } catch (err) {
        console.error("Ошибка синхронизации с iiko:", err);
        els.badge.innerText = "❌ Ошибка сети при синхронизации";
        els.badge.className = "px-3 py-1 rounded-full text-[10px] font-semibold bg-rose-500/10 text-rose-400 border border-rose-500/20";
        els.store.innerHTML = '<option value="">-- Ошибка сети --</option>';
        els.store.disabled = true;
        els.supplierSearch.disabled = true;
        els.supplierSearch.placeholder = "Ошибка синхронизации";
    }
}

function resetDashboardState() {
    localStorage.removeItem('active_company_id');
    els.store.innerHTML = '<option value="">-- Сначала выберите заведение --</option>';
    els.store.disabled = true;
    els.supplierSearch.value = "";
    els.supplierSearch.disabled = true;
    els.supplierSearch.placeholder = "Введите имя поставщика...";
    els.badge.innerText = "Заведение не выбрано";
    els.badge.className = "px-3 py-1 rounded-full text-[10px] font-semibold bg-rose-500/10 text-rose-400 border border-rose-500/20";
    els.btnParse.disabled = true;
    els.btnParse.classList.add('opacity-50', 'cursor-not-allowed');
    els.tbodyUnlisted.innerHTML = '<tr><td colspan="7" class="p-6 text-center text-slate-500 italic">Заведение не выбрано</td></tr>';
    els.tbodyAnalytics.innerHTML = '<tr><td colspan="4" class="p-6 text-center text-slate-500 italic">Заведение не выбрано</td></tr>';
    const ticketsList = document.getElementById('tickets-admin-list');
    if (ticketsList) ticketsList.innerHTML = '<div class="p-8 text-center text-slate-500 text-xs">Сначала выберите заведение в верхней панели</div>';
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

function switchBughTab(tabId) {
    if (typeof stopTicketsPolling === 'function') stopTicketsPolling();
    const tabs = ['parser', 'analytics', 'unlisted', 'template', 'tickets', 'reconciliation'];
    
    tabs.forEach(t => {
        const btn = document.getElementById(`tab-btn-${t}`);
        const sect = document.getElementById(`bugh-section-${t}`);
        
        if (btn) {
            btn.classList.remove('border-b-2', 'border-brand-500', 'text-brand-400');
            btn.classList.add('text-slate-500', 'hover:text-slate-300');
        }
        if (sect) sect.classList.add('hidden');
    });

    const activeBtn = document.getElementById(`tab-btn-${tabId}`);
    const activeSect = document.getElementById(`bugh-section-${tabId}`);

    if (activeBtn) {
        activeBtn.classList.remove('text-slate-500', 'hover:text-slate-300');
        activeBtn.classList.add('border-b-2', 'border-brand-500', 'text-brand-400');
    }
    if (activeSect) activeSect.classList.remove('hidden');

    if (tabId === 'unlisted') loadUnlistedOperations();
    if (tabId === 'analytics') loadAnalytics();
    if (tabId === 'tickets') {
        if (typeof startTicketsPolling === 'function') startTicketsPolling();
        else loadAdminTickets();
    }
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

if (els.store) {
    els.store.addEventListener('change', () => {
        const companyId = els.company.value;
        const val = els.store.value;
        
        // Auto-save to localStorage
        if (companyId && val) {
            localStorage.setItem(`saved_store_${companyId}`, val);
        } else if (companyId && !val) {
            localStorage.removeItem(`saved_store_${companyId}`);
        }
        
        // Sync inline selector
        const inlineStore = document.getElementById('res-inline-store');
        if (inlineStore) {
            inlineStore.value = val;
        }
    });
}

if (els.supplierSearch) {
    // Keep the existing input listener for real-time UI sync
    els.supplierSearch.addEventListener('input', () => {
        const inlineSupplier = document.getElementById('res-inline-supplier-name');
        if (inlineSupplier) {
            inlineSupplier.innerText = els.supplierSearch.value.trim() || '—';
        }
    });

    // Add a new change/blur listener for auto-saving
    els.supplierSearch.addEventListener('change', () => {
        const companyId = els.company.value;
        const val = els.supplierSearch.value.trim();
        if (companyId) {
            localStorage.setItem(`saved_supplier_name_${companyId}`, val);
            const foundSup = iikoSuppliers.find(s => s.name === val);
            if (foundSup) {
                localStorage.setItem(`saved_supplier_uuid_${companyId}`, foundSup.uuid);
            }
        }
    });
}