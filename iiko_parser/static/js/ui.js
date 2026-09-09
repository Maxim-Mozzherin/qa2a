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